package lower

import (
	"fmt"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestUpdatesAreAssignments: ++, -- and compound assignment lower to
// `x = x op v`, and type declarations to nothing.
func TestUpdatesAreAssignments(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, "type N = number;\ninterface I {\n  x: number;\n}\nlet n: N = 1;\nn++;\n--n;\nn *= 3;\nn /= 2;\nn -= 1;\nlet s: string = \"a\";\ns += \"b\";\n")
	var got []string
	for _, st := range m.Main {
		a, ok := st.(*ir.Assign)
		if !ok {
			continue
		}
		b, ok := a.Value.(*ir.Binary)
		if !ok {
			t.Fatalf("%s is assigned a %T, want *ir.Binary", a.Name, a.Value)
		}
		var right string
		switch r := b.Right.(type) {
		case *ir.NumberLit:
			right = fmt.Sprint(r.Value)
		case *ir.StringLit:
			right = r.Value
		default:
			right = fmt.Sprintf("%T", r)
		}
		got = append(got, fmt.Sprintf("%s = %s %s %s", a.Name, b.Left.(*ir.Ident).Name, b.Op, right))
	}
	if want := "n = n + 1|n = n - 1|n = n * 3|n = n / 2|n = n - 1|s = s + b"; strings.Join(got, "|") != want {
		t.Errorf("assignments = %q, want %q", strings.Join(got, "|"), want)
	}
	if len(m.Main) != 8 {
		t.Errorf("Main has %d statements, want 8: the type declarations lower to nothing", len(m.Main))
	}
}

// TestForLowersItsClauses: a for loop keeps its own variables in Init, its
// update in Post, and break and continue in the body.
func TestForLowersItsClauses(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, "for (let i: number = 0, j: number = 9; i < j; i++) {\n  if (i === 2) {\n    continue;\n  }\n  break;\n}\nfor (;;) {\n  break;\n}\n")
	loop, ok := m.Main[0].(*ir.For)
	if !ok {
		t.Fatalf("statement 0 is %T, want *ir.For", m.Main[0])
	}
	if len(loop.Init) != 2 || loop.Cond == nil || len(loop.Post) != 1 {
		t.Fatalf("for has %d inits, cond %v and %d posts; want 2, a condition and 1", len(loop.Init), loop.Cond, len(loop.Post))
	}
	for i, name := range []string{"i", "j"} {
		if d, ok := loop.Init[i].(*ir.VarDecl); !ok || d.Name != name || !d.Mutable {
			t.Errorf("init %d = %#v, want the let %s", i, loop.Init[i], name)
		}
	}
	if a, ok := loop.Post[0].(*ir.Assign); !ok || a.Name != "i" {
		t.Errorf("post = %#v, want an assignment to i", loop.Post[0])
	}
	cont := loop.Body[0].(*ir.If).Then[0]
	if _, ok := cont.(*ir.Continue); !ok {
		t.Errorf("the if's body is %T, want *ir.Continue", cont)
	}
	if _, ok := loop.Body[1].(*ir.Break); !ok {
		t.Errorf("the loop's last statement is %T, want *ir.Break", loop.Body[1])
	}
	if forever := m.Main[1].(*ir.For); forever.Init != nil || forever.Cond != nil || forever.Post != nil {
		t.Errorf("for (;;) = %#v, want no clauses", forever)
	}
}

func TestLoopRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, want string
	}{
		{"a labelled break", "outer: for (let i: number = 0; i < 3; i++) {\n  break outer;\n}\n", "labeled statement is not supported yet"},
		{"a labelled continue", "outer: for (let i: number = 0; i < 3; i++) {\n  continue outer;\n}\n", "labeled statement is not supported yet"},
		{"a comma in a clause", "let j: number = 0;\nfor (let i: number = 0; i < 3; i++, j++) {\n}\n", "a comma expression in a for loop's clause is not supported yet"},
		{"++ on a parenthesised name", "let n: number = 0;\n(n)++;\n", "++ on anything but a variable's name, or a field of one, is not supported yet"},
		{"++ on an i32", "let n: i32 = i32(0);\nn++;\n", "++ on a i32 is not supported, so the conversion stays visible: write n = i32(n + …)"},
		{"+= mixing a string and a number", "let s: string = \"a\";\ns += 1;\n", "+= with a string and a number is not supported yet"},
		{"postfix ++ as a value", "let n: number = 0;\nconst m: number = n++;\n", "++ as a value is not supported yet; use it as a statement of its own"},
		{"prefix -- as a value", "let n: number = 0;\nconsole.log(--n);\n", "-- as a value is not supported yet"},
		{"a number as a condition", "const n: number = 1;\nif (n) {\n  console.log(n);\n}\n", "a condition must be a boolean; testing a number for truthiness is not supported yet"},
		{"a string as a while condition", "const s: string = \"x\";\nwhile (s) {\n}\n", "a condition must be a boolean; testing a string"},
		{"a number as a for condition", "for (let i: number = 3; i; i--) {\n}\n", "a condition must be a boolean; testing a number"},
	} {
		_, errs := lowerSource(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors = %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}
}
