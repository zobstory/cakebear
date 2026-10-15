package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// Frozen consts (M4 plan D10b). A request handler or a spawned function may
// not use an object or array declared outside it, because whatever else holds
// it could change it while the function runs (D10). A frozen const is the
// exception: a const whose type is readonly all the way down, and whose
// initialiser creates everything it holds, so nothing anywhere can change any
// of it. That is what lets a handler read a route table or a config object.
//
// Two things keep it so. TypeScript refuses a write through a readonly
// property or a ReadonlyArray. And lowering refuses a flow from a readonly
// type into a mutable one (readonlyFlow), which TypeScript allows for
// properties, so no mutable alias of any part of a frozen const can be made.

// frozenWhy says why decl, a const's declaration or a name its pattern binds,
// isn't frozen, or returns "" when it is. The answer is kept per declaration,
// since the race rule asks at every use.
func (l *lowerer) frozenWhy(decl *ast.Node) string {
	if why, ok := l.p.frozen[decl]; ok {
		return why
	}
	if l.p.frozen == nil {
		l.p.frozen = map[*ast.Node]string{}
	}
	l.p.frozen[decl] = "it refers to itself" // while it is being decided
	why := l.decideFrozen(decl)
	l.p.frozen[decl] = why
	return why
}

func (l *lowerer) decideFrozen(decl *ast.Node) string {
	if !isConstDecl(decl) {
		switch root := bindingRoot(decl); {
		case root != nil && root.Kind == ast.KindParameter:
			return "it is a parameter"
		case root != nil && root.Kind == ast.KindVariableDeclaration:
			return "it is a let"
		}
		return "it isn't a const"
	}
	if why := l.deepReadonly(l.checker.GetTypeAtLocation(decl.Name()), nil); why != "" {
		return "its type isn't readonly all the way down: " + why
	}
	root := bindingRoot(decl)
	init := root.Initializer()
	if init == nil {
		return "it has no initialiser"
	}
	return l.fresh(init)
}

// deepReadonly says what in a value of type t could be changed through it, or
// returns "": a primitive, or a ReadonlyArray or an object of readonly
// properties holding only such values. A function, a promise or a host value
// is refused, since what it captures or stands for can change.
func (l *lowerer) deepReadonly(t *checker.Type, seen map[*checker.Type]bool) string {
	if seen[t] {
		return ""
	}
	if seen == nil {
		seen = map[*checker.Type]bool{}
	}
	seen[t] = true
	switch it := l.typeFromType(t); it.Kind {
	case ir.KindArray:
		if !l.isReadonlyArray(t) {
			return "a " + it.String() + " is mutable; write `readonly " + it.String() + "`"
		}
		return l.deepReadonly(l.checker.GetElementTypeOfArrayType(t), seen)
	case ir.KindObject:
		for _, prop := range l.checker.GetPropertiesOfType(t) {
			if !l.checker.IsReadonlySymbol(prop) {
				return "property `" + prop.Name() + "` isn't readonly"
			}
			if why := l.deepReadonly(l.checker.GetTypeOfSymbol(prop), seen); why != "" {
				return why
			}
		}
		return ""
	case ir.KindFunc:
		return "it holds a function, whose captures can change"
	case ir.KindPromise:
		return "it holds a promise"
	case ir.KindHost:
		return "it holds a node:http value"
	case ir.KindInvalid:
		return "it holds a type the backend cannot represent yet"
	}
	return "" // a primitive, or one of cakebear's extension types
}

// isReadonlyArray reports whether t is ReadonlyArray<T>, which
// `readonly T[]` also spells.
func (l *lowerer) isReadonlyArray(t *checker.Type) bool {
	if !l.checker.IsArrayType(t) || t.ObjectFlags()&checker.ObjectFlagsReference == 0 {
		return false
	}
	sym := t.Target().Symbol()
	return sym != nil && sym.Name() == "ReadonlyArray" && onlyInBundledLib(sym, "")
}

// fresh says what in the initialiser n of a frozen const might also be held
// elsewhere, or returns "": every object and array in it is a literal written
// there, or another frozen const. A primitive can come from anything.
func (l *lowerer) fresh(n *ast.Node) string {
	n = ast.SkipParentheses(n)
	switch l.typeOf(n).Kind {
	case ir.KindObject, ir.KindArray:
	default:
		return ""
	}
	switch n.Kind {
	case ast.KindAsExpression, ast.KindSatisfiesExpression:
		return l.fresh(n.Expression())
	case ast.KindObjectLiteralExpression:
		for _, p := range n.AsObjectLiteralExpression().Properties.Nodes {
			var why string
			switch p.Kind {
			case ast.KindPropertyAssignment:
				why = l.fresh(p.AsPropertyAssignment().Initializer)
			case ast.KindShorthandPropertyAssignment:
				why = l.freshName(p.Name(), l.checker.GetShorthandAssignmentValueSymbol(p))
			default:
				why = "it holds a " + describeKind(p.Kind)
			}
			if why != "" {
				return why
			}
		}
		return ""
	case ast.KindArrayLiteralExpression:
		for _, e := range n.AsArrayLiteralExpression().Elements.Nodes {
			if why := l.fresh(e); why != "" {
				return why
			}
		}
		return ""
	case ast.KindIdentifier:
		return l.freshName(n, l.checker.GetSymbolAtLocation(n))
	}
	return "it holds a value from a " + describeKind(n.Kind) + ", which something else may hold too; build it from literals"
}

// freshName says why the aggregate name refers to, sym, might be held
// elsewhere, or returns "" when it is a frozen const.
func (l *lowerer) freshName(name *ast.Node, sym *ast.Symbol) string {
	sym = l.p.resolve(sym)
	if sym == nil || sym.ValueDeclaration() == nil {
		return "it holds `" + name.Text() + "`, which cakebear can't see"
	}
	if why := l.frozenWhy(sym.ValueDeclaration()); why != "" {
		return "it holds `" + name.Text() + "`, which isn't frozen: " + why
	}
	return ""
}

// readonlyFlow says where a value of type src flowing into a slot of type dst
// would be writable through dst though readonly in src, or returns "".
// TypeScript allows that for properties; cakebear refuses it, so that no
// mutable alias of a frozen const can be made. The walk follows the IR's own
// kinds, so it enters only shapes, arrays, promises and function types the
// subset holds, never a library's types. A function type is checked both
// ways: its result flows out, and its parameters flow in.
func (l *lowerer) readonlyFlow(src, dst *checker.Type, seen map[[2]*checker.Type]bool) string {
	if src == nil || dst == nil || src == dst || seen[[2]*checker.Type{src, dst}] {
		return ""
	}
	if seen == nil {
		seen = map[[2]*checker.Type]bool{}
	}
	seen[[2]*checker.Type{src, dst}] = true
	c := l.checker
	kind := l.typeFromType(dst).Kind
	if l.typeFromType(src).Kind != kind {
		return ""
	}
	switch kind {
	case ir.KindArray:
		return l.readonlyFlow(c.GetElementTypeOfArrayType(src), c.GetElementTypeOfArrayType(dst), seen)
	case ir.KindPromise:
		return l.readonlyFlow(c.GetPromisedTypeOfPromise(src), c.GetPromisedTypeOfPromise(dst), seen)
	case ir.KindObject:
		for _, d := range c.GetPropertiesOfType(dst) {
			s := c.GetPropertyOfType(src, d.Name())
			if s == nil {
				continue
			}
			if c.IsReadonlySymbol(s) && !c.IsReadonlySymbol(d) {
				return "`" + d.Name() + "` is readonly in " + c.TypeToString(src) + " and not in " + c.TypeToString(dst)
			}
			if why := l.readonlyFlow(c.GetTypeOfSymbol(s), c.GetTypeOfSymbol(d), seen); why != "" {
				return why
			}
		}
	case ir.KindFunc:
		ss, ds := c.GetSignaturesOfType(src, checker.SignatureKindCall), c.GetSignaturesOfType(dst, checker.SignatureKindCall)
		if len(ss) != 1 || len(ds) != 1 {
			return ""
		}
		if why := l.readonlyFlow(c.GetReturnTypeOfSignature(ss[0]), c.GetReturnTypeOfSignature(ds[0]), seen); why != "" {
			return "its result: " + why
		}
		sps, dps := ss[0].Parameters(), ds[0].Parameters()
		for i := 0; i < len(sps) && i < len(dps); i++ {
			if given := c.GetTypeOfSymbol(dps[i]); l.readonlyFlow(given, c.GetTypeOfSymbol(sps[i]), seen) != "" {
				return "its parameter " + sps[i].Name() + " is given a " + c.TypeToString(given) + ", and " +
					l.readonlyFlow(given, c.GetTypeOfSymbol(sps[i]), nil)
			}
		}
	}
	return ""
}

// readonlyOK refuses, at n, a value of a readonly type filling a slot of a
// mutable one (readonlyFlow). Only a value that is or holds an object can be
// changed through its slot. A function literal is held to its slot where it
// is lowered (closureFor), parameters and all. An object literal is held to
// its slot as a whole, which covers a shorthand property in it.
//
// The value an unannotated pattern destructures, `const { a } = n`, needs no
// exception: the checker's contextual type there is one it makes up from the
// pattern, `{ a: any }`, which no shape matches, so readonlyFlow finds nothing.
func (l *lowerer) readonlyOK(n *ast.Node, v ir.Expr) bool {
	if !holdsObject(v.ExprType(), nil) || n.Kind == ast.KindArrowFunction || n.Kind == ast.KindFunctionExpression {
		return true
	}
	dst := l.checker.GetContextualType(n, checker.ContextFlagsNone)
	if dst == nil {
		return true
	}
	src := l.checker.GetTypeAtLocation(n)
	if why := l.readonlyFlow(src, dst, nil); why != "" {
		l.failReadonlyFlow(n, src, dst, why)
		return false
	}
	return true
}

// failReadonlyFlow refuses, at n, a flow readonlyFlow found.
func (l *lowerer) failReadonlyFlow(n *ast.Node, src, dst *checker.Type, why string) {
	l.failf(n, "a %s can't be used as a %s: %s; cakebear refuses a readonly value where a mutable one is wanted, so nothing frozen can be changed through another name",
		l.checker.TypeToString(src), l.checker.TypeToString(dst), why)
}
