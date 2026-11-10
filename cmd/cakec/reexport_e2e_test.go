package main

import (
	"path/filepath"
	"testing"
)

// M7 step 1 (plan D2): re-exports, end to end. Each program's node output is
// what Node 24.18 printed for the same sources, 5 runs, all identical
// (~/Desktop/cakebear/spikes/m7-step1/gen_reexport_e2e.py, which writes this
// file). The R numbers are spikes/m7-node's.
var reexportPrograms = []struct {
	name  string
	files map[string]string
	node  string
}{
	{
		"R8: a barrel's requests, imports and re-exports, in source order",
		map[string]string{
			"a.ts":      "console.log(\"a\");\nexport const av: number = 1;\n",
			"b.ts":      "console.log(\"b\");\nexport const bv: number = 2;\n",
			"barrel.ts": "console.log(\"barrel\");\nexport * from \"./d.ts\";\nimport { cv } from \"./c.ts\";\nexport { bv } from \"./b.ts\";\nexport { cv };\n",
			"c.ts":      "console.log(\"c\");\nexport const cv: number = 3;\n",
			"d.ts":      "console.log(\"d\");\nexport const dv: number = 4;\n",
			"main.ts":   "import { av } from \"./a.ts\";\nimport { bv, cv, dv } from \"./barrel.ts\";\nimport { zv } from \"./z.ts\";\nconsole.log(\"main\", av, bv, cv, dv, zv);\n",
			"quiet.ts":  "export { av } from \"./a.ts\";\nexport * from \"./z.ts\";\n",
			"z.ts":      "console.log(\"z\");\nexport const zv: number = 26;\n",
		},
		"a\nd\nc\nb\nbarrel\nz\nmain 1 2 3 4 26\n",
	},
	{
		"R8: a module the barrel requests, evaluated first",
		map[string]string{
			"a.ts":      "console.log(\"a\");\nexport const av: number = 1;\n",
			"b.ts":      "console.log(\"b\");\nexport const bv: number = 2;\n",
			"barrel.ts": "console.log(\"barrel\");\nexport * from \"./d.ts\";\nimport { cv } from \"./c.ts\";\nexport { bv } from \"./b.ts\";\nexport { cv };\n",
			"c.ts":      "console.log(\"c\");\nexport const cv: number = 3;\n",
			"d.ts":      "console.log(\"d\");\nexport const dv: number = 4;\n",
			"main.ts":   "import { cv } from \"./c.ts\";\nimport { bv } from \"./barrel.ts\";\nconsole.log(\"main2\", bv, cv);\n",
			"quiet.ts":  "export { av } from \"./a.ts\";\nexport * from \"./z.ts\";\n",
			"z.ts":      "console.log(\"z\");\nexport const zv: number = 26;\n",
		},
		"c\nd\nb\nbarrel\nmain2 2 3\n",
	},
	{
		"R8: a module of re-exports alone takes its place",
		map[string]string{
			"a.ts":      "console.log(\"a\");\nexport const av: number = 1;\n",
			"b.ts":      "console.log(\"b\");\nexport const bv: number = 2;\n",
			"barrel.ts": "console.log(\"barrel\");\nexport * from \"./d.ts\";\nimport { cv } from \"./c.ts\";\nexport { bv } from \"./b.ts\";\nexport { cv };\n",
			"c.ts":      "console.log(\"c\");\nexport const cv: number = 3;\n",
			"d.ts":      "console.log(\"d\");\nexport const dv: number = 4;\n",
			"main.ts":   "import { zv } from \"./quiet.ts\";\nimport { bv } from \"./barrel.ts\";\nconsole.log(\"main3\", zv, bv);\n",
			"quiet.ts":  "export { av } from \"./a.ts\";\nexport * from \"./z.ts\";\n",
			"z.ts":      "console.log(\"z\");\nexport const zv: number = 26;\n",
		},
		"a\nz\nd\nc\nb\nbarrel\nmain3 26 2\n",
	},
	{
		"R9: a chain of re-exports, renamed and starred",
		map[string]string{
			"lib.ts":  "console.log(\"lib\");\nexport function f(): string {\n  return \"f\";\n}\n",
			"main.ts": "import { f, g } from \"./r1.ts\";\nconsole.log(\"main\", f(), g());\n",
			"r1.ts":   "console.log(\"r1\");\nexport { f as g } from \"./r2.ts\";\nexport * from \"./r2.ts\";\n",
			"r2.ts":   "console.log(\"r2\");\nexport { f } from \"./lib.ts\";\n",
		},
		"lib\nr2\nr1\nmain f f\n",
	},
	{
		"a cycle through export *",
		map[string]string{
			"main.ts": "import { one } from \"./s2.ts\";\nimport { two } from \"./s1.ts\";\nconsole.log(\"main\", one, two);\n",
			"s1.ts":   "console.log(\"s1\");\nexport * from \"./s2.ts\";\nexport const one: number = 1;\n",
			"s2.ts":   "console.log(\"s2\");\nexport * from \"./s1.ts\";\nexport const two: number = 2;\n",
		},
		"s1\ns2\nmain 1 2\n",
	},
	{
		"re-exporting an import, whose module's state the calls share",
		map[string]string{
			"lib.ts":  "let n: number = 1;\nexport function inc(): void {\n  n++;\n}\nexport function get(): number {\n  return n;\n}\nexport const k: string = \"k\";\n",
			"main.ts": "import { inc, get, kay } from \"./re.ts\";\nconsole.log(get(), kay);\ninc();\nconsole.log(get());\n",
			"re.ts":   "import { inc, get, k } from \"./lib.ts\";\nexport { inc, get, k as kay };\nconsole.log(\"re\");\n",
		},
		"re\n1 k\n2\n",
	},
	{
		"R3: a local export shadows a star's",
		map[string]string{
			"lib.ts":  "export const a: string = \"lib\";\nexport const b: string = \"b\";\n",
			"main.ts": "import { a, b } from \"./star.ts\";\nconsole.log(\"main\", a, b);\n",
			"star.ts": "export * from \"./lib.ts\";\nexport const a: string = \"local\";\n",
		},
		"main local b\n",
	},
}

// TestReexportsMatchNode: each program prints what Node printed, 20 runs out
// of 20, every module evaluated where Node evaluates it.
func TestReexportsMatchNode(t *testing.T) {
	t.Parallel()

	for _, p := range reexportPrograms {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeProject(t, dir, moduleProject(p.files))
			if code, stderr := projectBuild(t, dir, buildOptions{}); code != exitOK {
				t.Fatalf("build: exit %d\n%s", code, stderr)
			}
			matchesNode(t, filepath.Join(dir, "app"), p.node, 20)
		})
	}
}
