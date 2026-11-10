package main

import (
	"path/filepath"
	"testing"
)

// M7 step 3 (plan D7): default exports and imports, end to end. Each
// program's node output is what Node 24.18 printed for the same sources,
// 5 runs, all identical (~/Desktop/cakebear/spikes/m7-step3/
// gen_defaults_e2e.py, which writes this file). The R numbers are
// spikes/m7-node's.
var defaultPrograms = []struct {
	name  string
	files map[string]string
	node  string
}{
	{
		"R12, R15: each default the subset holds, through each import form",
		map[string]string{
			"d1.ts":   "export default function named(): string {\n  return \"named\";\n}\n",
			"d10.ts":  "export default { a: 1, b: [1, 2] };\n",
			"d11.ts":  "function local(): string {\n  return \"local\";\n}\nexport { local as default };\n",
			"d2.ts":   "export default function (): string {\n  return \"anon\";\n}\n",
			"d3.ts":   "export default (): string => \"arrow\";\n",
			"d6.ts":   "export default (function (): string {\n  return \"paren\";\n});\n",
			"d7.ts":   "const later: () => string = function (): string {\n  return \"later\";\n};\nexport default later;\n",
			"d8.ts":   "export default 42;\n",
			"d9.ts":   "export default \"str\";\n",
			"lib.ts":  "export default function main(): string {\n  return \"main\";\n}\nexport const a: number = 1;\n",
			"main.ts": "import d1 from \"./d1.ts\";\nimport d2 from \"./d2.ts\";\nimport d3 from \"./d3.ts\";\nimport d6 from \"./d6.ts\";\nimport d7 from \"./d7.ts\";\nimport d8 from \"./d8.ts\";\nimport d9 from \"./d9.ts\";\nimport d10 from \"./d10.ts\";\nimport d11 from \"./d11.ts\";\nimport m from \"./lib.ts\";\nimport { default as m2 } from \"./lib.ts\";\nimport m3, { a } from \"./lib.ts\";\nconsole.log(\"calls\", d1(), d2(), d3(), d6(), d7(), d11());\nconsole.log(\"values\", d8, d9, d10);\nconsole.log(\"forms\", m(), m2(), m3(), a);\n",
		},
		"calls named anon arrow paren later local\nvalues 42 str { a: 1, b: [ 1, 2 ] }\nforms main main main 1\n",
	},
	{
		"R13: an export default expression runs at its place in the body",
		map[string]string{
			"e1.ts":   "import { make } from \"./side.ts\";\nconsole.log(\"e1 before\");\nexport default make(\"e1\");\nconsole.log(\"e1 after\");\n",
			"main.ts": "console.log(\"main body\");\nimport d from \"./e1.ts\";\nconsole.log(\"main\", d);\n",
			"side.ts": "console.log(\"side\");\nexport function make(tag: string): string {\n  console.log(\"make\", tag);\n  return tag;\n}\n",
		},
		"side\ne1 before\nmake e1\ne1 after\nmain body\nmain e1\n",
	},
	{
		"R14: export default of a let is a snapshot",
		map[string]string{
			"m1.ts":   "let x: number = 1;\nexport default x;\nexport function bump(): void {\n  x++;\n}\nexport function now(): number {\n  return x;\n}\n",
			"m4.ts":   "let w: number = 100;\nexport default w;\nw = 200;\nexport function getW(): number {\n  return w;\n}\n",
			"main.ts": "import d1, { bump, now } from \"./m1.ts\";\nimport d4, { getW } from \"./m4.ts\";\nconsole.log(d1, now(), d4, getW());\nbump();\nconsole.log(d1, now());\n",
		},
		"1 1 100 200\n1 2\n",
	},
	{
		"R13: a default function is hoisted, so a cycle reads it before its module runs",
		map[string]string{
			"cyc-f.ts": "import \"./cyc-g.ts\";\nexport default function fd(): string {\n  return \"fd\";\n}\n",
			"cyc-g.ts": "import fd from \"./cyc-f.ts\";\nconsole.log(\"cyc-g sees\", fd());\n",
			"cyc-h.ts": "import \"./cyc-i.ts\";\nexport default function (): string {\n  return \"anon\";\n}\n",
			"cyc-i.ts": "import h from \"./cyc-h.ts\";\nconsole.log(\"cyc-i sees\", h());\n",
			"main.ts":  "import fd from \"./cyc-f.ts\";\nimport h from \"./cyc-h.ts\";\nconsole.log(\"main\", fd(), h());\n",
		},
		"cyc-g sees fd\ncyc-i sees anon\nmain fd anon\n",
	},
	{
		"defaults re-exported, and a const exported as default",
		map[string]string{
			"lib.ts":   "console.log(\"lib\");\nexport default function greet(): string {\n  return \"hi\";\n}\n",
			"main.ts":  "import g, { hello } from \"./re.ts\";\nimport n from \"./other.ts\";\nconsole.log(\"main\", g(), hello(), n);\n",
			"other.ts": "export const n: number = 3;\nexport { n as default };\n",
			"re.ts":    "export { default } from \"./lib.ts\";\nexport { default as hello } from \"./lib.ts\";\n",
		},
		"lib\nmain hi hi 3\n",
	},
}

// TestDefaultsMatchNode: each program prints what Node printed, 20 runs out
// of 20, every module evaluated where Node evaluates it.
func TestDefaultsMatchNode(t *testing.T) {
	t.Parallel()

	for _, p := range defaultPrograms {
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
