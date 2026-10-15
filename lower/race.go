package lower

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// The handler race rule (M1 plan, decision D2).
//
// cakebear runs each request on its own goroutine (CLAUDE.md, semantics
// decision 3), so a request listener runs on many goroutines at once.
// JavaScript guarantees handlers never overlap, and correct TypeScript relies
// on it: `hits = hits + 1` in a handler is fine under Node and a data race
// here. Rather than add locks, lowering refuses what could race:
//
//   - a handler may not touch a mutable binding (a `let`, or a parameter)
//     declared outside it. Reading is refused too, because the rest of the
//     program can change the binding while handlers run;
//   - nor may it call, or pass on, a function that touches one, directly or
//     through the functions it calls in turn.
//
// Reading a const is fine when it holds a primitive or a function, because
// neither can change, and those are the only consts allowed: what a const of
// any other type holds counts as shared, so a new kind of value is refused
// until the rule says otherwise (shareableConst). A function value is fine
// only when the rule can see into it, as a function declaration or a const
// bound to a function literal. One it can't, such as a const holding what a
// call returned, may close over state nothing here sees, so a concurrent
// function may not use it. A promise can: it settles. So a const promise counts as
// shared state too (M2 plan, D9), and the runtime, which keeps each promise to
// the domain that made it, never sees one cross. So does a const host value
// (D10): the runtime's server and response types are not goroutine-safe, and
// a handler or a spawned function reaching one made outside it would be using
// it from two goroutines at once.
// Objects and arrays are the same (M4 plan D10): a const object's fields
// can still change, and so can a const array's elements. A concurrent
// function may use only the objects and arrays it creates itself.
//
// The same rule holds a spawned function to what it may share with its caller
// (spawn.go); concurrency says which of the two a diagnostic is about.
//
// It is program-wide (M3 plan step 5): the summary covers every module's
// functions, a name is followed through an import to the declaration it
// names, and a binding counts wherever in the program it is declared. A
// file-by-file rule would pass, silently, a handler calling an imported
// function that changes its own module's let.

// sharedUse is one mutable binding, declared outside some function, that the
// function touches.
type sharedUse struct {
	sym     *ast.Symbol
	name    string
	file    *ast.SourceFile // where the binding is declared
	write   bool
	promise bool // the binding holds a promise
	host    bool // the binding holds a host value, such as a response
	opaque  bool // the binding holds a function value the rule can't see into
	// aggregate is "object" or "array" when the binding holds one: shared
	// state, whether the binding is a const or a let.
	aggregate string
	at        *ast.Node
}

// concurrency is the wording for one kind of function the runtime runs in
// parallel with others: a request handler, or a spawned function.
type concurrency struct {
	kind     string // "request handler"
	runs     string // what "this …" says about it
	named    string // the same, for a function named in place of a literal
	share    string // how a changed binding is shared
	sharedBy string // the same, for a callee's binding
	outside  string // where to copy a binding to
	inside   string // where to create an object instead
	across   string // where a promise or host value may not cross
}

var (
	handlerRuns = concurrency{
		kind: "request handler", runs: "this handler runs on many requests at once",
		named: "the handler `%s` runs on many requests at once", share: "which they all share",
		sharedBy: "shared by them all", outside: "outside the handler", inside: "inside the handler", across: "between requests",
	}
	spawnRuns = concurrency{
		kind: "spawned function", runs: "this spawned function runs in parallel with its caller",
		named: "the spawned function `%s` runs in parallel with its caller", share: "which its caller shares",
		sharedBy: "shared with its caller", outside: "outside the spawned function", inside: "inside the spawned function",
		across: "with a spawned function",
	}
)

// reference is a use of a callable inside another function.
type reference struct {
	sym *ast.Symbol
	at  *ast.Node
}

// callable is a function the rule can see into: a function declaration, or a
// function literal bound to a const. uses holds what it touches, including
// through the callables it references, once raceChecker has propagated them.
type callable struct {
	name string
	node *ast.Node
	uses []sharedUse
	refs []reference
}

type raceChecker struct {
	// l is the lowerer that built the summary, for its checker and type
	// mapping. Diagnostics go through the lowerer of the module being
	// checked, whose file their spans are in.
	l         *lowerer
	callables map[*ast.Symbol]*callable
	// order is the callables in source order. Propagation walks it rather
	// than the map, so which binding a diagnostic names never depends on Go's
	// map iteration order.
	order []*callable
}

// raceRule builds the program's call-graph summary the first time a
// concurrent function needs checking.
func (l *lowerer) raceRule() *raceChecker {
	if l.p.race != nil {
		return l.p.race
	}
	rc := &raceChecker{l: l, callables: map[*ast.Symbol]*callable{}}
	for _, f := range l.p.files {
		rc.collect(f.AsNode())
	}
	for _, c := range rc.order {
		c.uses, c.refs = rc.scan(c.node)
	}
	rc.propagate()
	l.p.race = rc
	return rc
}

func (rc *raceChecker) collect(n *ast.Node) {
	switch n.Kind {
	case ast.KindFunctionDeclaration:
		if name := n.Name(); name != nil {
			rc.add(name, n)
		}
	case ast.KindVariableDeclaration:
		d := n.AsVariableDeclaration()
		if init := d.Initializer; init != nil && n.Parent != nil && n.Parent.Flags&ast.NodeFlagsConst != 0 &&
			d.Name() != nil && d.Name().Kind == ast.KindIdentifier &&
			(init.Kind == ast.KindArrowFunction || init.Kind == ast.KindFunctionExpression) {
			rc.add(d.Name(), init)
		}
	}
	n.ForEachChild(func(c *ast.Node) bool { rc.collect(c); return false })
}

func (rc *raceChecker) add(name, fn *ast.Node) {
	if sym := rc.l.checker.GetSymbolAtLocation(name); sym != nil {
		c := &callable{name: name.Text(), node: fn}
		rc.callables[sym] = c
		rc.order = append(rc.order, c)
	}
}

// scan finds, in fn's body, the mutable bindings declared outside fn that it
// touches, and the callables declared outside fn that it references. Anything
// declared inside fn, a nested closure included, is part of the walk itself.
func (rc *raceChecker) scan(fn *ast.Node) (uses []sharedUse, refs []reference) {
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if n.Kind == ast.KindIdentifier && !isMemberName(n) {
			if sym := rc.l.p.resolve(rc.l.valueSymbol(n)); sym != nil {
				if decl, ok := rc.mutableBinding(sym); ok && !within(decl, fn) {
					kind := rc.l.typeOf(n).Kind
					uses = append(uses, sharedUse{
						sym: sym, name: n.Text(), file: ast.GetSourceFileOfNode(decl), write: isWrite(n),
						promise: kind == ir.KindPromise, host: kind == ir.KindHost, aggregate: aggregateWord(kind), at: n,
					})
				} else if c, ok := rc.callables[sym]; ok && !within(c.node, fn) {
					refs = append(refs, reference{sym: sym, at: n})
				} else if decl, ok := rc.opaqueFunction(sym); ok && !within(decl, fn) {
					uses = append(uses, sharedUse{sym: sym, name: n.Text(), file: ast.GetSourceFileOfNode(decl), opaque: true, at: n})
				}
			}
		}
		n.ForEachChild(visit)
		return false
	}
	if body := fn.Body(); body != nil {
		visit(body)
	}
	return uses, refs
}

// propagate folds each callable's references into its uses until nothing
// changes, so recursion and mutual recursion settle rather than loop.
func (rc *raceChecker) propagate() {
	for changed := true; changed; {
		changed = false
		for _, c := range rc.order {
			for _, r := range c.refs {
				for _, u := range rc.callables[r.sym].uses {
					if rc.mergeUse(c, u) {
						changed = true
					}
				}
			}
		}
	}
}

// mergeUse records u on c if c does not already account for it, keeping the
// stronger kind: a write outranks a read.
//
// A callee's binding never needs filtering here: c only references callables
// declared outside it, and those cannot see a binding declared inside c, so
// every use they bring is outside c too.
func (rc *raceChecker) mergeUse(c *callable, u sharedUse) bool {
	for i := range c.uses {
		if c.uses[i].sym == u.sym {
			if u.write && !c.uses[i].write {
				c.uses[i].write = true
				return true
			}
			return false
		}
	}
	c.uses = append(c.uses, u)
	return true
}

func (rc *raceChecker) mutableBinding(sym *ast.Symbol) (*ast.Node, bool) {
	decl := sym.ValueDeclaration()
	if sym.Flags()&ast.SymbolFlagsVariable == 0 || !rc.l.p.inProgram(decl) {
		return nil, false
	}
	if isConstDecl(decl) {
		// A const is shared state unless what it holds can't change.
		if name := decl.Name(); name != nil && shareableConst(rc.l.typeOf(name).Kind) {
			return nil, false
		}
	}
	return decl, true
}

// shareableConst reports whether a const of kind k can be shared with a
// concurrent function: a primitive, an extension type, or a function (which
// scan checks as a callable, or refuses as opaque). It is an allowlist, so a
// kind of value it doesn't name, a promise, a host value, and whatever comes
// next, counts as shared.
func shareableConst(k ir.Kind) bool {
	switch k {
	case ir.KindNumber, ir.KindString, ir.KindBoolean, ir.KindFunc:
		return true
	}
	return ir.Type{Kind: k}.IsExtension()
}

// opaqueFunction reports whether sym is a binding of the program's own that
// holds a function value the rule can't see into: of function type, but not a
// callable. A mutable one is a mutableBinding already.
func (rc *raceChecker) opaqueFunction(sym *ast.Symbol) (*ast.Node, bool) {
	decl := sym.ValueDeclaration()
	if sym.Flags()&ast.SymbolFlagsVariable == 0 || !rc.l.p.inProgram(decl) || decl.Name() == nil {
		return nil, false
	}
	if _, known := rc.callables[sym]; known || rc.l.typeOf(decl.Name()).Kind != ir.KindFunc {
		return nil, false
	}
	return decl, true
}

// within reports whether decl is declared inside fn. Positions are offsets
// into a file, so a declaration in another module is never within, whatever
// its offsets.
func within(decl, fn *ast.Node) bool {
	return ast.GetSourceFileOfNode(decl) == ast.GetSourceFileOfNode(fn) &&
		decl.Pos() >= fn.Pos() && decl.End() <= fn.End()
}

// isWrite reports whether id is assigned to, rather than read.
func isWrite(id *ast.Node) bool {
	p := id.Parent
	if p == nil {
		return false
	}
	switch p.Kind {
	case ast.KindBinaryExpression:
		b := p.AsBinaryExpression()
		k := b.OperatorToken.Kind
		return b.Left == id && k >= ast.KindFirstAssignment && k <= ast.KindLastAssignment
	case ast.KindPrefixUnaryExpression:
		op := p.AsPrefixUnaryExpression().Operator
		return op == ast.KindPlusPlusToken || op == ast.KindMinusMinusToken
	case ast.KindPostfixUnaryExpression:
		return true // ++ and -- are its only operators
	}
	return false
}

// checkHandler applies the rule to createServer's listener argument.
func (l *lowerer) checkHandler(arg *ast.Node, lowered ir.Expr) {
	l.checkConcurrent(arg, lowered, handlerRuns)
}

// checkConcurrent applies the rule to a function the runtime runs in parallel
// with others: a request handler, or a spawned function.
func (l *lowerer) checkConcurrent(arg *ast.Node, lowered ir.Expr, who concurrency) {
	rc := l.raceRule()
	switch arg.Kind {
	case ast.KindArrowFunction, ast.KindFunctionExpression:
		if lit, ok := lowered.(*ir.FuncLit); ok {
			lit.Concurrent = true
		}
		rc.checkLiteral(l, arg, who)
	case ast.KindIdentifier:
		c, ok := rc.callables[l.p.resolve(l.checker.GetSymbolAtLocation(arg))]
		if !ok {
			l.failf(arg, "cakebear can only check a %s written inline or declared as a function; %s is neither", who.kind, arg.Text())
			return
		}
		if u, ok := strongest(c.uses); ok {
			l.failf(arg, "%s, so it can't %s", strings.Replace(who.named, "%s", arg.Text(), 1), touchPhrase(u, who, l.file))
		}
	default:
		l.failf(arg, "cakebear can only check a %s written inline or declared as a function", who.kind)
	}
}

// checkLiteral reports, in source order, each shared binding a concurrent
// literal touches (once, at its first write if there is one) and each
// callable it references that touches one.
func (rc *raceChecker) checkLiteral(l *lowerer, fn *ast.Node, who concurrency) {
	uses, refs := rc.scan(fn)
	type finding struct {
		at  *ast.Node
		msg string
	}
	var found []finding

	first := map[*ast.Symbol]int{}
	var direct []sharedUse
	for _, u := range uses {
		if i, seen := first[u.sym]; !seen {
			first[u.sym] = len(direct)
			direct = append(direct, u)
		} else if u.write && !direct[i].write {
			direct[i] = u // report the write, not the read before it
		}
	}
	for _, u := range direct {
		found = append(found, finding{u.at, who.runs + ", so it can't " + touchPhrase(u, who, l.file)})
	}

	reported := map[*ast.Symbol]bool{}
	for _, r := range refs {
		c := rc.callables[r.sym]
		u, ok := strongest(c.uses)
		if !ok || reported[r.sym] {
			continue
		}
		reported[r.sym] = true
		// The name as written: an import may rename what it names.
		verb, name := "use", r.at.Text()
		if p := r.at.Parent; p != nil && p.Kind == ast.KindCallExpression && p.Expression() == r.at {
			verb, name = "call", name+"()"
		}
		found = append(found, finding{r.at, who.runs + ", so it can't " +
			verb + " `" + name + "`, which " + calleePhrase(u, who, l.file)})
	}

	sort.SliceStable(found, func(i, j int) bool { return found[i].at.Pos() < found[j].at.Pos() })
	for _, f := range found {
		l.failf(f.at, "%s", f.msg)
	}
}

// strongest picks the use to name: the first write, else the first read.
func strongest(uses []sharedUse) (sharedUse, bool) {
	for _, u := range uses {
		if u.write {
			return u, true
		}
	}
	if len(uses) > 0 {
		return uses[0], true
	}
	return sharedUse{}, false
}

// label names u's binding in a diagnostic reported in from, with its
// module's file when another module declares it.
func (u sharedUse) label(from *ast.SourceFile) string {
	if u.file != nil && u.file != from {
		return "`" + u.name + "` in " + filepath.Base(string(u.file.FileName()))
	}
	return "`" + u.name + "`"
}

// aggregateWord names the aggregate a binding of kind k holds, or is "".
func aggregateWord(k ir.Kind) string {
	switch k {
	case ir.KindObject:
		return "object"
	case ir.KindArray:
		return "array"
	}
	return ""
}

func touchPhrase(u sharedUse, who concurrency, from *ast.SourceFile) string {
	switch name := u.label(from); {
	case u.write:
		return "change " + name + ", " + who.share
	case u.aggregate != "":
		return "use " + name + ", an " + u.aggregate + " created outside it; an " + u.aggregate + " can't be shared " + who.across +
			" yet: create it " + who.inside + ", or in a function that returns a new one"
	case u.promise:
		return "use " + name + ", a promise created outside it; a promise can't be shared " + who.across + " yet"
	case u.host:
		return "use " + name + ", a node:http value created outside it; one can't be shared " + who.across + " yet"
	case u.opaque:
		return "use " + name + ", a function value cakebear can't see into, which could touch anything; " +
			"declare it with `function`, or as a const bound to an arrow function"
	default:
		return "read " + name + ", which can change while it runs; copy it into a `const` " + who.outside + " if it doesn't change"
	}
}

func calleePhrase(u sharedUse, who concurrency, from *ast.SourceFile) string {
	switch name := u.label(from); {
	case u.write:
		return "changes " + name + ", " + who.sharedBy
	case u.aggregate != "":
		return "uses " + name + ", an " + u.aggregate + " created " + who.outside
	case u.promise:
		return "uses " + name + ", a promise created " + who.outside
	case u.host:
		return "uses " + name + ", a node:http value created " + who.outside
	case u.opaque:
		return "uses " + name + ", a function value cakebear can't see into"
	default:
		return "reads " + name + ", which can change while it runs"
	}
}
