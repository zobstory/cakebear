package main

// The M7 race fixture (step 4, done-check 6): a server whose handler reads
// frozen consts and calls functions through a barrel's `export *` and
// `export { } from`, a tsconfig `paths` alias, a workspace package's
// node_modules link, and default imports. raceModulesReplies is what Node
// 24.18 served for each path, 5 requests each, with testdata/paths-hook.mjs
// (~/Desktop/cakebear/spikes/m7-step4/gen_race_data.py, which writes this
// file).
var raceModulesProject = map[string]string{
	"package.json":               "{\"name\": \"app\", \"private\": true, \"type\": \"module\", \"workspaces\": [\"packages/*\"]}\n",
	"packages/menu/package.json": "{\"name\": \"menu\", \"type\": \"module\", \"exports\": \"./src/index.ts\"}\n",
	"packages/menu/src/index.ts": "export const items: readonly string[] = [\"tea\", \"cake\"];\nexport default function count(): number {\n  return items.length;\n}\n",
	"src/data/greet.ts":          "export function greet(name: string): string {\n  return \"hi \" + name;\n}\n",
	"src/data/index.ts":          "export * from \"./routes.ts\";\nexport { greet } from \"./greet.ts\";\n",
	"src/data/routes.ts":         "export interface Route {\n  readonly path: string;\n  readonly body: string;\n}\nexport const routes: readonly Route[] = [\n  { path: \"/a\", body: \"alpha\" },\n  { path: \"/b\", body: \"beta\" },\n];\n",
	"src/home.ts":                "const home: { readonly path: string } = { path: \"/\" };\nexport default home;\n",
	"src/main.ts":                "import { createServer } from \"node:http\";\nimport { routes, greet } from \"@/data/index.ts\";\nimport count, { items } from \"menu\";\nimport home from \"./home.ts\";\n\nconst port: number = 3000;\n\ncreateServer((req, res) => {\n  const url: string = req.url;\n  let body: string = \"none\";\n  for (const r of routes) {\n    if (r.path === url) {\n      body = r.body;\n    }\n  }\n  if (url === \"/menu\") {\n    body = items.join(\",\") + \" \" + String(count());\n  }\n  if (url === home.path) {\n    body = greet(\"home\");\n  }\n  res.setHeader(\"Content-Type\", \"text/plain\");\n  res.end(body);\n}).listen(port, () => {\n  console.log(\"listening on http://localhost:\" + String(port));\n});\n",
	"tsconfig.json":              "{\n  \"compilerOptions\": {\n    \"strict\": true,\n    \"noEmit\": true,\n    \"allowImportingTsExtensions\": true,\n    \"module\": \"esnext\",\n    \"moduleResolution\": \"bundler\",\n    \"paths\": {\n      \"@/*\": [\n        \"./src/*\"\n      ]\n    }\n  },\n  \"include\": [\n    \"src\"\n  ],\n  \"cakebear\": {\n    \"main\": \"src/main.ts\",\n    \"output\": \"bin/server\"\n  }\n}\n",
}

var raceModulesLinks = map[string]string{
	"node_modules/menu": "../packages/menu",
}

var raceModulesReplies = []struct {
	path, contentType, body string
	status                  int
}{
	{"/a", "text/plain", "alpha", 200},
	{"/b", "text/plain", "beta", 200},
	{"/menu", "text/plain", "tea,cake 2", 200},
	{"/", "text/plain", "hi home", 200},
	{"/x", "text/plain", "none", 200},
}
