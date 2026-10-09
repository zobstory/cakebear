package lower

import (
	"sort"

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
// Reading a const is fine: in M1 a const holds a primitive, a function or a
// host value, and none of those can change. ⚠ That stops being true once
// objects and arrays exist (a const array is still mutable through
// `xs.push(…)`), and this rule must grow with them.

// sharedUse is one mutable binding, declared outside some function, that the
// function touches.
type sharedUse struct {
	sym   *ast.Symbol
	name  string
	write bool
	at    *ast.Node
}

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
	l         *lowerer
	callables map[*ast.Symbol]*callable
	// order is the callables in source order. Propagation walks it rather
	// than the map, so which binding a diagnostic names never depends on Go's
	// map iteration order.
	order []*callable
}

// raceRule builds the file's call-graph summary the first time a handler
// needs checking.
func (l *lowerer) raceRule() *raceChecker {
	if l.race != nil {
		return l.race
	}
	rc := &raceChecker{l: l, callables: map[*ast.Symbol]*callable{}}
	rc.collect(l.file.AsNode())
	for _, c := range rc.order {
		c.uses, c.refs = rc.scan(c.node)
	}
	rc.propagate()
	l.race = rc
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
			if sym := rc.l.checker.GetSymbolAtLocation(n); sym != nil {
				if decl, ok := rc.mutableBinding(sym); ok && !within(decl, fn) {
					uses = append(uses, sharedUse{sym: sym, name: n.Text(), write: isWrite(n), at: n})
				} else if c, ok := rc.callables[sym]; ok && !within(c.node, fn) {
					refs = append(refs, reference{sym: sym, at: n})
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
	decl := sym.ValueDeclaration
	if sym.Flags&ast.SymbolFlagsVariable == 0 || decl == nil || ast.GetSourceFileOfNode(decl) != rc.l.file {
		return nil, false
	}
	if decl.Kind == ast.KindVariableDeclaration && decl.Parent != nil && decl.Parent.Flags&ast.NodeFlagsConst != 0 {
		return nil, false
	}
	return decl, true
}

func within(decl, fn *ast.Node) bool {
	return decl.Pos() >= fn.Pos() && decl.End() <= fn.End()
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
	rc := l.raceRule()
	switch arg.Kind {
	case ast.KindArrowFunction, ast.KindFunctionExpression:
		if lit, ok := lowered.(*ir.FuncLit); ok {
			lit.Concurrent = true
		}
		rc.checkLiteral(arg)
	case ast.KindIdentifier:
		c, ok := rc.callables[l.checker.GetSymbolAtLocation(arg)]
		if !ok {
			l.fail(arg, "cakebear can only check a request handler written inline or declared as a function; %s is neither", arg.Text())
			return
		}
		if u, ok := strongest(c.uses); ok {
			l.fail(arg, "the handler `%s` runs on many requests at once, so it can't %s", c.name, touchPhrase(u))
		}
	default:
		l.fail(arg, "cakebear can only check a request handler written inline or declared as a function")
	}
}

// checkLiteral reports, in source order, each shared binding a handler
// literal touches (once, at its first write if there is one) and each
// callable it references that touches one.
func (rc *raceChecker) checkLiteral(fn *ast.Node) {
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
		found = append(found, finding{u.at, "this handler runs on many requests at once, so it can't " + touchPhrase(u)})
	}

	reported := map[*ast.Symbol]bool{}
	for _, r := range refs {
		c := rc.callables[r.sym]
		u, ok := strongest(c.uses)
		if !ok || reported[r.sym] {
			continue
		}
		reported[r.sym] = true
		verb, name := "use", c.name
		if p := r.at.Parent; p != nil && p.Kind == ast.KindCallExpression && p.Expression() == r.at {
			verb, name = "call", c.name+"()"
		}
		found = append(found, finding{r.at, "this handler runs on many requests at once, so it can't " +
			verb + " `" + name + "`, which " + calleePhrase(u)})
	}

	sort.SliceStable(found, func(i, j int) bool { return found[i].at.Pos() < found[j].at.Pos() })
	for _, f := range found {
		rc.l.fail(f.at, "%s", f.msg)
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

func touchPhrase(u sharedUse) string {
	if u.write {
		return "change `" + u.name + "`, which they all share"
	}
	return "read `" + u.name + "`, which can change while it runs; copy it into a `const` outside the handler if it doesn't change"
}

func calleePhrase(u sharedUse) string {
	if u.write {
		return "changes `" + u.name + "`, shared by them all"
	}
	return "reads `" + u.name + "`, which can change while it runs"
}
