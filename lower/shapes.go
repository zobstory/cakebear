package lower

import "github.com/zobstory/cakebear/ir"

// usedShapes is the program's shapes for ir.Program.Shapes: those its types
// reach, numbered again in the order they were interned. Classification
// interns a shape for every object type it looks at, some of which nothing
// in the program holds, such as a type that failed for another reason
// partway through, and the backend need only define those that are used.
func usedShapes(p *ir.Program, all []*ir.Shape) []*ir.Shape {
	used := map[*ir.Shape]bool{}
	var mark func(t ir.Type)
	mark = func(t ir.Type) {
		switch t.Kind {
		case ir.KindObject:
			if t.Shape == nil || used[t.Shape] {
				return
			}
			used[t.Shape] = true
			for _, f := range t.Shape.Fields {
				mark(f.Type)
			}
		case ir.KindArray, ir.KindPromise:
			if t.Elem != nil {
				mark(*t.Elem)
			}
		case ir.KindFunc:
			if t.Sig != nil {
				for _, param := range t.Sig.Params {
					mark(param)
				}
				mark(t.Sig.Result)
			}
		}
	}
	for _, m := range p.Modules {
		for _, fn := range m.Funcs {
			for _, param := range fn.Params {
				mark(param.Type)
			}
			mark(fn.Result)
		}
		ir.Inspect(m, func(node any) {
			switch n := node.(type) {
			case ir.Expr:
				mark(n.ExprType())
			case *ir.VarDecl:
				mark(n.Type)
			case *ir.ForOf:
				mark(n.Type)
			}
			if lit, ok := node.(*ir.FuncLit); ok {
				for _, param := range lit.Params {
					mark(param.Type)
				}
			}
		})
	}
	// Ordered by ID, as interned: deterministic, since lowering is.
	var shapes []*ir.Shape
	for _, s := range all {
		if used[s] {
			s.ID = len(shapes)
			shapes = append(shapes, s)
		}
	}
	return shapes
}
