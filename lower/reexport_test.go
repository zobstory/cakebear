package lower

import (
	"path/filepath"
	"strings"
	"testing"
)

// importsOf names the files a module requests, by base name, in order.
func importsOf(files []string) string {
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	return strings.Join(names, " ")
}

// TestReexportsReferIntoTheDeclaringModule (M7 plan D2, R1, R9): a name
// re-exported by `export { … } from`, renamed, by `export *`, as a
// re-exported import, or along a chain refers into the module declaring it,
// by the name declared there. A module of re-exports alone is in the
// program, and each `export … from` is a module request at its place.
func TestReexportsReferIntoTheDeclaringModule(t *testing.T) {
	t.Parallel()

	p := mustLowerFiles(t, map[string]string{
		"lib.ts":  "export function a(): number {\n  return 1;\n}\nexport function b(): number {\n  return 2;\n}\nexport const c: number = 3;\n",
		"lib2.ts": "export const d: number = 4;\nexport function e(): number {\n  return 5;\n}\n",
		"re.ts": "export { a } from \"./lib\";\nexport { b as bee, c } from \"./lib\";\nexport * from \"./lib2\";\n" +
			"import { e as five } from \"./lib2\";\nexport { five };\nconsole.log(\"re\");\n",
		"chain.ts": "export { bee as bee2 } from \"./re\";\n",
		"main.ts":  "import { a, bee, c, d, five } from \"./re\";\nimport { bee2 } from \"./chain\";\nconsole.log(a(), bee(), c, d, five(), bee2());\n",
	}, "main.ts")

	if got, want := strings.Join(modulesOf(p), " "), "chain.ts lib.ts lib2.ts main.ts re.ts"; got != want {
		t.Fatalf("modules = %s, want %s", got, want)
	}
	refs := refsIn(p.EntryModule())
	for key, want := range map[string]string{
		"call a": "lib.ts", "call b": "lib.ts", "ident c": "lib.ts", "ident d": "lib2.ts", "call e": "lib2.ts",
	} {
		if got, ok := refs[key]; !ok || got != want {
			t.Errorf("%s: qualified %q (present %v), want %q", key, got, ok, want)
		}
	}
	for _, local := range []string{"call bee", "call bee2", "call five"} {
		if _, ok := refs[local]; ok {
			t.Errorf("a re-exported name kept its own name: %s", local)
		}
	}
	for file, want := range map[string]string{
		"main.ts": "re.ts chain.ts", "re.ts": "lib.ts lib.ts lib2.ts lib2.ts", "chain.ts": "re.ts", "lib.ts": "",
	} {
		for _, m := range p.Modules {
			if filepath.Base(m.File) == file {
				if got := importsOf(m.Imports); got != want {
					t.Errorf("%s requests %q, want %q", file, got, want)
				}
			}
		}
	}
}

// TestReexportsEvaluateInSourceOrder (R8): a module's imports and
// `export … from`s are its requests, in source order, each evaluated before
// the module's body; the order is Node 24.18's for the same graph
// (spikes/m7-node c2-reexport-order).
func TestReexportsEvaluateInSourceOrder(t *testing.T) {
	t.Parallel()

	logs := func(name, v string, n int) string {
		return "console.log(\"" + name + "\");\nexport const " + v + ": number = " + string(rune('0'+n)) + ";\n"
	}
	shared := map[string]string{
		"a.ts":      logs("a", "av", 1),
		"b.ts":      logs("b", "bv", 2),
		"c.ts":      logs("c", "cv", 3),
		"d.ts":      logs("d", "dv", 4),
		"z.ts":      logs("z", "zv", 9),
		"barrel.ts": "console.log(\"barrel\");\nexport * from \"./d\";\nimport { cv } from \"./c\";\nexport { bv } from \"./b\";\nexport { cv };\n",
		"quiet.ts":  "export { av } from \"./a\";\nexport * from \"./z\";\n",
	}
	for _, tt := range []struct{ main, order string }{
		{"import { av } from \"./a\";\nimport { bv, cv, dv } from \"./barrel\";\nimport { zv } from \"./z\";\nconsole.log(\"main\", av, bv, cv, dv, zv);\n",
			"a d c b barrel z main"},
		{"import { cv } from \"./c\";\nimport { bv } from \"./barrel\";\nconsole.log(\"main2\", bv, cv);\n",
			"c d b barrel main"},
		{"import { zv } from \"./quiet\";\nimport { bv } from \"./barrel\";\nconsole.log(\"main3\", zv, bv);\n",
			"a z quiet d c b barrel main"},
	} {
		files := map[string]string{"main.ts": tt.main}
		for k, v := range shared {
			files[k] = v
		}
		p := mustLowerFiles(t, files, "main.ts")
		var order []string
		for _, i := range p.EvaluationOrder() {
			order = append(order, strings.TrimSuffix(filepath.Base(p.Modules[i].File), ".ts"))
		}
		if got := strings.Join(order, " "); got != tt.order {
			t.Errorf("evaluation order %s, want %s", got, tt.order)
		}
	}
}
