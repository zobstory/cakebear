package lower

import (
	"strings"
	"testing"
)

// TestCastRefusals: a cast cakebear can't check is refused by name.
func TestCastRefusals(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, src, want string }{
		{"between two types", "interface A { a: number }\ninterface B { a: number; b: number }\nconst x: B = { a: 1, b: 2 };\nconst y = x as A;\nconsole.log(y.a);\n",
			"a cast from a B to an A is not supported yet: cakebear casts only from an unknown"},
		{"to a type holding a function", "const u: unknown = 1;\nconst f = u as { run: () => void };\nconsole.log(f === f);\n",
			"which holds a () => void, is not supported: cakebear checks a cast, and can't check that"},
		{"to an extension type", "const u: unknown = 1;\nconst n = u as i32;\nconsole.log(n);\n",
			"which holds an extension type such as i32"},
		{"to a Record", "const u: unknown = 1;\nconst r = u as Record<string, number>;\nconsole.log(r === r);\n",
			"index signatures, and Record, are not supported yet"},
		{"JSON.parse into a function's slot", "const f: () => void = JSON.parse(\"1\");\nf();\n",
			"JSON.parse returns any, which cakebear doesn't lower: give its result a type"},
	} {
		_, errs := lowerProgram(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors = %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}
}

// TestCastsLower: what a cast can be.
func TestCastsLower(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"interface P { x: number; y: number }\nconst p = { x: 1, y: 2 } satisfies P;\nconsole.log(p.x);\n",
		"interface P { x: number; y: number }\nconst p: P = JSON.parse(\"{}\");\nconsole.log(p.x);\n",
		"const u: unknown = JSON.parse(\"[1]\");\nconst xs = u as number[];\nconsole.log(xs.length);\n",
		"const n: number = 1;\nconst u = n as unknown;\nconsole.log(u);\n",
		"type Mode = \"a\" | \"b\";\nconst m = \"a\" as Mode;\nconsole.log(m);\n",
	} {
		if _, errs := lowerProgram(t, src); len(errs) > 0 {
			t.Errorf("%s: errors = %q, want none", src, joinMsgs(errs))
		}
	}
}
