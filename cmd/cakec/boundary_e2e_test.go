package main

import (
	"path/filepath"
	"testing"
)

// M7 step 2 (plan D4-D6, D10): the program's own TypeScript source by any
// specifier the checker resolves, end to end. Each program's node output is
// what Node 24.18 printed for the same files and links, run from the
// project's root, 5 runs, all identical; a program importing through tsconfig
// `paths` ran with testdata/paths-hook.mjs passed to --import, since Node
// never reads a tsconfig (R24). ~/Desktop/cakebear/spikes/m7-step2/
// gen_boundary_e2e.py records them and writes this file.
var boundaryPrograms = []struct {
	name  string
	files map[string]string
	links map[string]string // a symlink's path, and what it points at
	node  string
}{
	{
		"tsconfig paths: an @/ alias and a relative path reach one module",
		map[string]string{
			"src/lib/count.ts": "console.log(\"count runs\");\nlet n: number = 0;\nexport function next(): number {\n  n++;\n  return n;\n}\n",
			"src/lib/greet.ts": "import { next } from \"@/lib/count.ts\";\nexport function greet(name: string): string {\n  return \"hello \" + name + \" \" + String(next());\n}\n",
			"src/main.ts":      "import { greet } from \"@/lib/greet.ts\";\nimport { next } from \"./lib/count.ts\";\nconsole.log(greet(\"a\"));\nconsole.log(next());\nconsole.log(greet(\"b\"));\n",
			"tsconfig.json":    "{\n  \"compilerOptions\": {\n    \"strict\": true,\n    \"noEmit\": true,\n    \"allowImportingTsExtensions\": true,\n    \"module\": \"esnext\",\n    \"moduleResolution\": \"bundler\",\n    \"paths\": {\n      \"@/*\": [\n        \"./src/*\"\n      ]\n    }\n  },\n  \"include\": [\n    \"src\"\n  ],\n  \"cakebear\": {\n    \"main\": \"src/main.ts\",\n    \"output\": \"app\"\n  }\n}\n",
		},
		map[string]string{},
		"count runs\nhello a 1\n2\nhello b 3\n",
	},
	{
		"package.json imports",
		map[string]string{
			"package.json":    "{\"name\": \"app\", \"type\": \"module\", \"imports\": {\"#lib/*\": \"./src/lib/*\"}}\n",
			"src/lib/util.ts": "console.log(\"util runs\");\nexport const twice = (n: number): number => n * 2;\n",
			"src/main.ts":     "import { twice } from \"#lib/util.ts\";\nimport { twice as again } from \"./lib/util.ts\";\nconsole.log(twice(2), again(3));\n",
			"tsconfig.json":   "{\n  \"compilerOptions\": {\n    \"strict\": true,\n    \"noEmit\": true,\n    \"allowImportingTsExtensions\": true,\n    \"module\": \"esnext\",\n    \"moduleResolution\": \"bundler\"\n  },\n  \"include\": [\n    \"src\"\n  ],\n  \"cakebear\": {\n    \"main\": \"src/main.ts\",\n    \"output\": \"app\"\n  }\n}\n",
		},
		map[string]string{},
		"util runs\n4 6\n",
	},
	{
		"a workspace package linked through node_modules",
		map[string]string{
			"package.json":              "{\"name\": \"app\", \"private\": true, \"type\": \"module\", \"workspaces\": [\"packages/*\"]}\n",
			"packages/pkg/package.json": "{\"name\": \"@scope/pkg\", \"version\": \"1.0.0\", \"type\": \"module\", \"exports\": {\".\": \"./src/index.ts\", \"./util\": \"./src/util.ts\"}}\n",
			"packages/pkg/src/index.ts": "import { u } from \"./util.ts\";\nconsole.log(\"index runs\");\nexport function hello(name: string): string {\n  return \"hello \" + name;\n}\nexport { u };\n",
			"packages/pkg/src/util.ts":  "console.log(\"util runs\");\nexport const u: number = 7;\n",
			"src/main.ts":               "import { hello, u } from \"@scope/pkg\";\nimport { u as u2 } from \"@scope/pkg/util\";\nconsole.log(\"main\", hello(\"w\"), u, u2);\n",
			"tsconfig.json":             "{\n  \"compilerOptions\": {\n    \"strict\": true,\n    \"noEmit\": true,\n    \"allowImportingTsExtensions\": true,\n    \"module\": \"esnext\",\n    \"moduleResolution\": \"bundler\"\n  },\n  \"include\": [\n    \"src\"\n  ],\n  \"cakebear\": {\n    \"main\": \"src/main.ts\",\n    \"output\": \"app\"\n  }\n}\n",
		},
		map[string]string{
			"node_modules/@scope/pkg": "../../packages/pkg",
		},
		"util runs\nindex runs\nmain hello w 7 7\n",
	},
	{
		"a package's own name",
		map[string]string{
			"package.json":  "{\"name\": \"app\", \"type\": \"module\", \"exports\": {\"./lib\": \"./src/lib.ts\"}}\n",
			"src/lib.ts":    "console.log(\"lib runs\");\nexport const label: string = \"self\";\n",
			"src/main.ts":   "import { label } from \"app/lib\";\nimport { label as same } from \"./lib.ts\";\nconsole.log(\"main\", label, same);\n",
			"tsconfig.json": "{\n  \"compilerOptions\": {\n    \"strict\": true,\n    \"noEmit\": true,\n    \"allowImportingTsExtensions\": true,\n    \"module\": \"esnext\",\n    \"moduleResolution\": \"bundler\"\n  },\n  \"include\": [\n    \"src\"\n  ],\n  \"cakebear\": {\n    \"main\": \"src/main.ts\",\n    \"output\": \"app\"\n  }\n}\n",
		},
		map[string]string{},
		"lib runs\nmain self self\n",
	},
}

// TestOwnSourceByAnySpecifierMatchesNode: each program prints what Node
// printed, 20 runs out of 20, and a module two specifiers reach runs once.
func TestOwnSourceByAnySpecifierMatchesNode(t *testing.T) {
	t.Parallel()

	for _, p := range boundaryPrograms {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeProject(t, dir, p.files)
			writeLinks(t, dir, p.links)
			if code, stderr := projectBuild(t, dir, buildOptions{}); code != exitOK {
				t.Fatalf("build: exit %d\n%s", code, stderr)
			}
			matchesNode(t, filepath.Join(dir, "app"), p.node, 20)
		})
	}
}
