package lower

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestDefaultsReferIntoTheirModule (M7 plan D7, R12, R15): a default import
// refers to what the default export declares, by its name there: a named
// default function by its own name, an anonymous one and any expression by
// `default`, `export default <const>` and `export { f as default }` by the
// binding's; through every import form and `export { default } from`.
func TestDefaultsReferIntoTheirModule(t *testing.T) {
	t.Parallel()

	p := mustLowerFiles(t, map[string]string{
		"named.ts":  "export default function greet(): number {\n  return 1;\n}\n",
		"anon.ts":   "export default function (): number {\n  return 2;\n}\n",
		"byname.ts": "export const limit: number = 3;\nexport default limit;\n",
		"snap.ts":   "let v: number = 4;\nexport default v;\nv = 5;\n",
		"expr.ts":   "export default 6 * 1;\n",
		"alias.ts":  "function f(): number {\n  return 7;\n}\nexport { f as default };\n",
		"both.ts":   "export const a: number = 8;\nexport default function d8(): number {\n  return a;\n}\n",
		"re.ts":     "export { default } from \"./named\";\nexport { default as six } from \"./expr\";\n",
		"main.ts": "import greet from \"./named\";\nimport two from \"./anon\";\nimport lim from \"./byname\";\nimport snap from \"./snap\";\n" +
			"import { default as six } from \"./expr\";\nimport seven from \"./alias\";\nimport d8, { a } from \"./both\";\n" +
			"import again, { six as six2 } from \"./re\";\n" +
			"console.log(greet(), two(), lim, snap, six, seven(), d8(), a, again(), six2);\n",
	}, "main.ts")

	refs := refsIn(p.EntryModule())
	for key, want := range map[string]string{
		"call greet": "named.ts", "call default": "anon.ts", "ident limit": "byname.ts", "ident default": "",
		"call f": "alias.ts", "call d8": "both.ts", "ident a": "both.ts",
	} {
		if key == "ident default" {
			continue // two modules' defaults are read; checked below
		}
		if got, ok := refs[key]; !ok || got != want {
			t.Errorf("%s: qualified %q (present %v), want %q", key, got, ok, want)
		}
	}
	// The hidden consts, `default` of snap.ts and of expr.ts, each read by
	// that module's file.
	defaults := map[string]bool{}
	ir.Inspect(p.EntryModule(), func(node any) {
		if id, ok := node.(*ir.Ident); ok && id.Name == "default" {
			defaults[filepath.Base(id.Module)] = true
		}
	})
	if !defaults["snap.ts"] || !defaults["expr.ts"] || len(defaults) != 2 {
		t.Errorf("hidden consts read from %v, want snap.ts and expr.ts", defaults)
	}
	for _, m := range p.Modules {
		var decls []string
		for _, s := range m.Main {
			if d, ok := s.(*ir.VarDecl); ok {
				decls = append(decls, d.Name)
			}
		}
		got, base := strings.Join(decls, " "), filepath.Base(m.File)
		want := map[string]string{"byname.ts": "limit", "snap.ts": "v default", "expr.ts": "default", "both.ts": "a"}[base]
		if got != want {
			t.Errorf("%s declares %q at its top level, want %q", base, got, want)
		}
	}
}

// TestDefaultsUnderTheRaceRule: a request handler reaching shared state
// through a default import is refused as through a named one, and a hidden
// const of a primitive is shareable.
func TestDefaultsUnderTheRaceRule(t *testing.T) {
	t.Parallel()

	handler := func(imp, body string) string {
		return "import { createServer } from \"node:http\";\n" + imp +
			"createServer((req, res) => {\n  " + body + "\n  res.end(\"ok\");\n}).listen(8080);\n"
	}
	for _, tt := range []struct {
		name, lib, imp, body, want string
	}{
		{"a named default function writing a let",
			"let hits: number = 0;\nexport default function hit(): void {\n  hits = hits + 1;\n}\n",
			"import hit from \"./lib\";\n", "hit();", "so it can't call `hit()`, which changes `hits` in lib.ts"},
		{"an anonymous default function writing a let",
			"let hits: number = 0;\nexport default function (): void {\n  hits = hits + 1;\n}\n",
			"import tick from \"./lib\";\n", "tick();", "so it can't call `tick()`, which changes `hits` in lib.ts"},
		{"a default arrow writing a let",
			"let hits: number = 0;\nexport default (): void => {\n  hits = hits + 1;\n};\n",
			"import tick from \"./lib\";\n", "tick();", "so it can't call `tick()`, which changes `hits` in lib.ts"},
		{"a default function from a call",
			"let hits: number = 0;\nfunction make(): () => void {\n  return () => {\n    hits = hits + 1;\n  };\n}\nexport default make();\n",
			"import tick from \"./lib\";\n", "tick();", "so it can't use `tick` in lib.ts, a function value cakebear can't see into"},
		{"a default object",
			"interface Home {\n  path: string;\n}\nexport default { path: \"/\" } as Home;\n",
			"import home from \"./lib\";\n", "const p: string = home.path;",
			"an object created outside it; an object can be shared between requests only if it is frozen (a const, readonly all the way down, built from literals), and this one isn't: it is the default export of an expression"},
		{"a default number, a hidden const",
			"let n: number = 1;\nexport default n;\nn = 2;\n",
			"import n from \"./lib\";\n", "const m: number = n;", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerFiles(t, map[string]string{"lib.ts": tt.lib, "main.ts": handler(tt.imp, tt.body)}, "main.ts")
			msgs := joinMsgs(errs)
			switch {
			case tt.want == "" && len(errs) > 0:
				t.Errorf("refused a program that shares nothing:\n%s", msgs)
			case tt.want != "" && !strings.Contains(msgs, tt.want):
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
		})
	}
}
