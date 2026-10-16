package lower

import (
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestAppendTargets: a string let of its own statement, or a parameter, that
// an append targets is marked Appended and its appends are ir.Appends; a let
// in a for loop's first clause, a destructured one, a prepend and a number's
// += are lowered as before.
func TestAppendTargets(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		appended  bool // the declaration is marked
		stmt      int  // the statement holding the append
		parts     int  // its pieces, when it is an ir.Append; 0 when it isn't
	}{
		{"+=", "let s: string = \"\";\ns += \"a\";\n", true, 1, 1},
		{"+= a template", "let s: string = \"\";\nconst i: number = 1;\ns += `x${i}y`;\n", true, 2, 3},
		{"s = s + a + b", "let s: string = \"\";\ns = s + \"a\" + \"b\";\n", true, 1, 2},
		{"a template starting with s", "let s: string = \"\";\ns = `${s}x`;\n", true, 1, 1},
		{"a prepend", "let s: string = \"\";\ns = \"a\" + s;\n", false, 1, 0},
		{"another appended binding first", "let s: string = \"\";\nlet t: string = \"\";\ns += \"1\";\nt += \"2\";\ns = t + \"x\";\n", true, 4, 0},
		{"a number", "let n: number = 0;\nn += 1;\n", false, 1, 0},
		{"a destructured let", "const o: { s: string } = { s: \"\" };\nlet { s } = o;\ns += \"a\";\n", false, 3, 0},
	} {
		m, errs := lowerSource(t, tt.src)
		if len(errs) > 0 {
			t.Fatalf("%s: unexpected errors: %v", tt.name, joinMsgs(errs))
		}
		var decl *ir.VarDecl
		for _, s := range m.Main {
			if d, ok := s.(*ir.VarDecl); ok && (d.Name == "s" || d.Name == "n") {
				decl = d
			}
		}
		if decl == nil || decl.Appended != tt.appended {
			t.Errorf("%s: declaration %+v, want Appended %v", tt.name, decl, tt.appended)
		}
		a, isAppend := m.Main[tt.stmt].(*ir.Append)
		switch {
		case tt.parts == 0 && isAppend:
			t.Errorf("%s: statement %d is an append, want it lowered as before", tt.name, tt.stmt)
		case tt.parts > 0 && !isAppend:
			t.Errorf("%s: statement %d is %T, want *ir.Append", tt.name, tt.stmt, m.Main[tt.stmt])
		case isAppend && len(a.Parts) != tt.parts:
			t.Errorf("%s: the append has %d pieces, want %d", tt.name, len(a.Parts), tt.parts)
		}
	}
}

// TestAppendedLoopClause: a let in a for loop's first clause has a binding
// per iteration, so it gets no accumulator, and its append is an Assign.
func TestAppendedLoopClause(t *testing.T) {
	t.Parallel()

	m, errs := lowerSource(t, "for (let s: string = \"\"; s.length < 3; s += \"a\") {\n}\n")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", joinMsgs(errs))
	}
	loop := m.Main[0].(*ir.For)
	if d := loop.Init[0].(*ir.VarDecl); d.Appended {
		t.Errorf("the for clause's let is marked Appended")
	}
	if _, ok := loop.Post[0].(*ir.Assign); !ok {
		t.Errorf("the post clause is %T, want *ir.Assign", loop.Post[0])
	}
}

// TestAppendedParametersAndReferences: a parameter appended to is marked,
// in a function and in a closure, and so is every reference to an appended
// binding, which is what lets `.length` use the accumulator.
func TestAppendedParametersAndReferences(t *testing.T) {
	t.Parallel()

	src := "function f(p: string): string {\n  p += \"x\";\n  return p;\n}\n" +
		"const g = (q: string): number => {\n  q += \"y\";\n  return q.length;\n};\n"
	m, errs := lowerSource(t, src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", joinMsgs(errs))
	}
	if fn := m.Funcs[0]; !fn.Params[0].Appended {
		t.Errorf("f's parameter isn't marked Appended")
	}
	lit := m.Main[0].(*ir.VarDecl).Init.(*ir.FuncLit)
	if !lit.Params[0].Appended {
		t.Errorf("the closure's parameter isn't marked Appended")
	}
	ret := lit.Body[1].(*ir.Return).Value.(*ir.BuiltinCall)
	if id, ok := ret.Args[0].(*ir.Ident); !ok || !id.Appended {
		t.Errorf("q in q.length is %+v, want a reference marked Appended", ret.Args[0])
	}
}
