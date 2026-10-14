package lower

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// mustLowerFiles lowers files with entry as the entry module and fails the
// test on any refusal.
func mustLowerFiles(t *testing.T, files map[string]string, entry string) *ir.Program {
	t.Helper()
	p, errs := lowerFiles(t, files, entry)
	if len(errs) > 0 {
		t.Fatalf("lowering refused:\n%s", joinMsgs(errs))
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return p
}

// modulesOf names a program's modules by base name, in order.
func modulesOf(p *ir.Program) []string {
	var names []string
	for _, m := range p.Modules {
		names = append(names, filepath.Base(m.File))
	}
	return names
}

// refsIn returns each reference in m as "callee name" or "ident name",
// mapped to the base name of the module it is qualified with, or "".
func refsIn(m *ir.Module) map[string]string {
	refs := map[string]string{}
	base := func(module string) string {
		if module == "" {
			return ""
		}
		return filepath.Base(module)
	}
	ir.Inspect(m, func(node any) {
		switch n := node.(type) {
		case *ir.Ident:
			refs["ident "+n.Name] = base(n.Module)
		case *ir.Call:
			refs["call "+n.Callee] = base(n.Module)
		}
	})
	return refs
}

// TestImportsReferIntoTheirModule (M3 plan D7): named imports of a function
// and a const, one renamed, refer into the module declaring them, by the name
// declared there.
func TestImportsReferIntoTheirModule(t *testing.T) {
	t.Parallel()

	p := mustLowerFiles(t, map[string]string{
		"lib.ts": "export function double(n: number): number {\n  return n * 2;\n}\n" +
			"export const limit: number = 3;\nconsole.log(limit);\n",
		"main.ts": "import { double, limit as max } from \"./lib\";\nconsole.log(double(max));\n",
	}, "main.ts")

	if got := strings.Join(modulesOf(p), " "); got != "lib.ts main.ts" {
		t.Fatalf("modules = %s, want lib.ts main.ts", got)
	}
	if p.Entry != 1 {
		t.Errorf("Entry = %d, want 1 (main.ts)", p.Entry)
	}
	refs := refsIn(p.EntryModule())
	for key, want := range map[string]string{"call double": "lib.ts", "ident limit": "lib.ts"} {
		if got, ok := refs[key]; !ok || got != want {
			t.Errorf("%s: qualified %q (present %v), want %q", key, got, ok, want)
		}
	}
	if _, ok := refs["ident max"]; ok {
		t.Error("the renamed import kept its local name, max")
	}
	if lib := p.Modules[0]; len(lib.Funcs) != 1 || len(lib.Main) != 2 {
		t.Errorf("lib.ts lowered to %d functions and %d statements, want 1 and 2", len(lib.Funcs), len(lib.Main))
	}
}

// TestTwoModulesEachDeclareHelper (groundwork for M3 check 6): the two
// helpers stay apart, by module.
func TestTwoModulesEachDeclareHelper(t *testing.T) {
	t.Parallel()

	helper := "export function helper(): number {\n  return %s;\n}\n"
	p := mustLowerFiles(t, map[string]string{
		"a.ts":    strings.Replace(helper, "%s", "1", 1),
		"b.ts":    strings.Replace(helper, "%s", "2", 1),
		"main.ts": "import { helper } from \"./a\";\nimport { helper as other } from \"./b\";\nconsole.log(helper());\nconsole.log(other());\n",
	}, "main.ts")

	var callees []string
	ir.Inspect(p.EntryModule(), func(node any) {
		if c, ok := node.(*ir.Call); ok {
			callees = append(callees, c.Callee+"@"+filepath.Base(c.Module))
		}
	})
	if got := strings.Join(callees, " "); got != "helper@a.ts helper@b.ts" {
		t.Errorf("calls = %s, want helper@a.ts helper@b.ts", got)
	}
}

// TestModuleGraph: imports are followed transitively, a cycle settles, an
// import for its effects alone counts, and a file nothing imports is no part
// of the program.
func TestModuleGraph(t *testing.T) {
	t.Parallel()

	p := mustLowerFiles(t, map[string]string{
		"a.ts":      "import { b } from \"./b\";\nimport \"./setup\";\nexport function a(): number {\n  return 1;\n}\nconsole.log(b());\n",
		"b.ts":      "import { c } from \"./c\";\nimport { a } from \"./a\";\nexport function b(): number {\n  return c() + a();\n}\n",
		"c.ts":      "export function c(): number {\n  return 2;\n}\n",
		"setup.ts":  "console.log(\"setup\");\nexport {};\n",
		"unused.ts": "export function unused(): number {\n  return 3;\n}\n",
	}, "a.ts")
	if got := strings.Join(modulesOf(p), " "); got != "a.ts b.ts c.ts setup.ts" {
		t.Errorf("modules = %s, want a.ts b.ts c.ts setup.ts", got)
	}
	if p.Entry != 0 {
		t.Errorf("Entry = %d, want 0 (a.ts)", p.Entry)
	}
}

// TestModuleRefusals: what M3 plan D7 and D8 leave out, each refused by name.
func TestModuleRefusals(t *testing.T) {
	t.Parallel()

	const f = "export function f(): number {\n  return 1;\n}\n"
	const useF = "import { f } from \"./lib\";\nconsole.log(f());\n"
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"a default import", map[string]string{"lib.ts": "function f(): number {\n  return 1;\n}\nexport default f;\n",
			"main.ts": "import f from \"./lib\";\nconsole.log(f());\n"}, "a default import is not supported yet"},
		{"a default export of a function", map[string]string{"lib.ts": "export default function f(): number {\n  return 1;\n}\n",
			"main.ts": "import { } from \"./lib\";\nexport {};\n"}, "a default export is not supported yet; export the function by name"},
		{"a default export of a value", map[string]string{"lib.ts": "export default 5;\n",
			"main.ts": "import { } from \"./lib\";\nexport {};\n"}, "a default export is not supported yet; export the value by name"},
		{"import * as", map[string]string{"lib.ts": f,
			"main.ts": "import * as lib from \"./lib\";\nconsole.log(lib.f());\n"}, "`import * as` is supported only for node:http"},
		{"export … from", map[string]string{"lib.ts": f, "mid.ts": "export { f } from \"./lib\";\n",
			"main.ts": "import { f } from \"./mid\";\nconsole.log(f());\n"}, "re-exporting with `export … from` is not supported yet"},
		{"export * from", map[string]string{"lib.ts": f, "mid.ts": "export * from \"./lib\";\n",
			"main.ts": "import { f } from \"./mid\";\nconsole.log(f());\n"}, "re-exporting with `export … from` is not supported yet"},
		{"re-exporting an import", map[string]string{"lib.ts": f, "mid.ts": "import { f } from \"./lib\";\nexport { f };\n",
			"main.ts": "import { f } from \"./mid\";\nconsole.log(f());\n"}, "exporting f, which this module imports, is not supported yet"},
		{"export let", map[string]string{"lib.ts": "export let n: number = 1;\n",
			"main.ts": "import { n } from \"./lib\";\nconsole.log(n);\n"}, "exporting a let is not supported yet"},
		{"export { a let }", map[string]string{"lib.ts": "let n: number = 1;\nexport { n };\n",
			"main.ts": "import { n } from \"./lib\";\nconsole.log(n);\n"}, "exporting n, a let, is not supported yet"},
		{"top-level await outside the entry", map[string]string{
			"lib.ts":  "const ready: Promise<void> = new Promise((resolve) => {\n  resolve();\n});\nawait ready;\n" + f,
			"main.ts": useF}, "a top-level await is supported only in the entry module"},
		{"a declaration file", map[string]string{"lib.d.ts": "export declare function f(): number;\n",
			"main.ts": useF}, "imports from \"./lib\" are not supported; cakebear compiles node:http and the program's own TypeScript modules"},
		{"a script the entry doesn't import", map[string]string{"util.ts": "function helper(): number {\n  return 1;\n}\n",
			"main.ts": "console.log(helper());\nexport {};\n"}, "helper is declared in util.ts, which the entry module doesn't import"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerFiles(t, tt.files, "main.ts")
			if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
		})
	}
}

// TestImportedFunctionIsNotALibraryFunction: lowering once called an
// imported function "a library function cakebear does not implement yet".
func TestImportedFunctionIsNotALibraryFunction(t *testing.T) {
	t.Parallel()

	_, errs := lowerFiles(t, map[string]string{
		"lib.ts":  "export function parseInt(n: number): number {\n  return n;\n}\n",
		"main.ts": "import { parseInt } from \"./lib\";\nconsole.log(parseInt(4));\n",
	}, "main.ts")
	if len(errs) > 0 {
		t.Errorf("lowering refused an imported function:\n%s", joinMsgs(errs))
	}
}

// TestImportsAreEvaluationEdges: an import of the program's own module
// evaluates it, unless it is `import type` or names only types. Both are
// erased, as in TypeScript's emit, and so bring no module into the program.
func TestImportsAreEvaluationEdges(t *testing.T) {
	t.Parallel()

	p := mustLowerFiles(t, map[string]string{
		"a.ts":      "export function a(): number {\n  return 1;\n}\n",
		"setup.ts":  "console.log(\"setup\");\nexport {};\n",
		"types.ts":  "export type Count = number;\n",
		"unused.ts": "export function unused(): number {\n  return 2;\n}\n",
		"main.ts": "import type { Count } from \"./types\";\nimport { a } from \"./a\";\nimport \"./setup\";\n" +
			"import { unused } from \"./unused\";\nimport { Name, type Tagged, type label } from \"./names\";\nimport { Id, id } from \"./mixed\";\n" +
			"const n: Count = a();\nconsole.log(n);\n",
		"names.ts": "export type Name = string;\nexport interface Tagged {\n  tag: string;\n}\nexport function label(): string {\n  return \"x\";\n}\nconsole.log(\"names\");\n",
		"mixed.ts": "export type Id = number;\nexport function id(): number {\n  return 3;\n}\n",
	}, "main.ts")
	var imports []string
	for _, file := range p.EntryModule().Imports {
		imports = append(imports, filepath.Base(file))
	}
	if got, want := strings.Join(imports, " "), "a.ts setup.ts unused.ts mixed.ts"; got != want {
		t.Errorf("main's imports = %s, want %s: an import evaluates, used or not, unless it names only types", got, want)
	}
	if got, want := strings.Join(modulesOf(p), " "), "a.ts main.ts mixed.ts setup.ts unused.ts"; got != want {
		t.Errorf("modules = %s, want %s, with no types.ts or names.ts", got, want)
	}
}
