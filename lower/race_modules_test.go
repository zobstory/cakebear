package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/internal/ast"
)

// The handler race rule across modules (M3 plan step 5, check 7). Each case
// is a program TypeScript accepts; the rule must see through the imports.

const (
	serve = "import { createServer } from \"node:http\";\n"
	// counter's bump changes a let only its own module can see.
	counter = "let count: number = 0;\nexport function bump(): number {\n  count = count + 1;\n  return count;\n}\n" +
		"export function peek(): number {\n  return count;\n}\n" +
		"export function double(n: number): number {\n  return n * 2;\n}\n"
)

func TestRaceRuleAcrossModules(t *testing.T) {
	t.Parallel()

	handler := func(body string) string {
		return serve + "createServer((req, res) => {\n  " + body + "\n  res.end(\"ok\");\n}).listen(8080);\n"
	}
	tests := []struct {
		name  string
		files map[string]string
		want  string // "" when the program is fine
	}{
		{
			"check 7: an imported function changes its module's let",
			map[string]string{"lib.ts": counter, "main.ts": "import { bump } from \"./lib\";\n" + handler("bump();")},
			"this handler runs on many requests at once, so it can't call `bump()`, which changes `count` in lib.ts, shared by them all",
		},
		{
			"an imported function reads its module's let",
			map[string]string{"lib.ts": counter, "main.ts": "import { peek } from \"./lib\";\n" + handler("const n: number = peek();")},
			"so it can't call `peek()`, which reads `count` in lib.ts, which can change while it runs",
		},
		{
			"renamed",
			map[string]string{"lib.ts": counter, "main.ts": "import { bump as tick } from \"./lib\";\n" + handler("tick();")},
			"so it can't call `tick()`, which changes `count` in lib.ts",
		},
		{
			"through a third module",
			map[string]string{
				"util.ts": "let hits: number = 0;\nexport function hit(): void {\n  hits = hits + 1;\n}\n",
				"lib.ts":  "import { hit } from \"./util\";\nexport function handle(): void {\n  hit();\n}\n",
				"main.ts": "import { handle } from \"./lib\";\n" + handler("handle();"),
			},
			"so it can't call `handle()`, which changes `hits` in util.ts",
		},
		{
			"an imported handler",
			map[string]string{
				"routes.ts": "import { IncomingMessage, ServerResponse } from \"node:http\";\nlet served: number = 0;\n" +
					"export function onRequest(req: IncomingMessage, res: ServerResponse): void {\n  served = served + 1;\n  res.end(\"ok\");\n}\n",
				"main.ts": serve + "import { onRequest } from \"./routes\";\ncreateServer(onRequest).listen(8080);\n",
			},
			"the handler `onRequest` runs on many requests at once, so it can't change `served` in routes.ts",
		},
		{
			"an imported promise",
			map[string]string{
				"lib.ts":  "export const ready: Promise<void> = new Promise((resolve) => {\n  resolve();\n});\n",
				"main.ts": serve + "import { ready } from \"./lib\";\ncreateServer(async (req, res) => {\n  await ready;\n  res.end(\"ok\");\n}).listen(8080);\n",
			},
			"so it can't use `ready` in lib.ts, a promise created outside it",
		},
		{
			"a spawned imported function",
			map[string]string{"lib.ts": counter, "main.ts": "import { bump } from \"./lib\";\nconst p: Promise<number> = spawn(() => bump());\nexport { p };\n"},
			"this spawned function runs in parallel with its caller, so it can't call `bump()`, which changes `count` in lib.ts",
		},
		{
			"an imported function that shares nothing",
			map[string]string{"lib.ts": counter, "main.ts": "import { double } from \"./lib\";\n" + handler("const n: number = double(2);")},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerFiles(t, tt.files, "main.ts")
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

// TestWithinIsPerFile: positions are offsets into a file, and the rule once
// compared them across files, so a callee in another module whose offsets
// fell inside a handler's was taken to be declared inside it, and skipped.
func TestWithinIsPerFile(t *testing.T) {
	t.Parallel()

	_, files := checkFiles(t, map[string]string{
		"lib.ts":  counter,
		"main.ts": "import { peek } from \"./lib\";\n" + serve + "createServer((req, res) => {\n  const n: number = peek();\n  res.end(\"ok\");\n}).listen(8080);\n",
	})
	var handler, peek *ast.Node
	var find func(n *ast.Node) bool
	find = func(n *ast.Node) bool {
		switch {
		case n.Kind == ast.KindArrowFunction && handler == nil:
			handler = n
		case n.Kind == ast.KindFunctionDeclaration && n.Name() != nil && n.Name().Text() == "peek":
			peek = n
		}
		n.ForEachChild(find)
		return false
	}
	files["main.ts"].AsNode().ForEachChild(find)
	files["lib.ts"].AsNode().ForEachChild(find)
	if handler == nil || peek == nil {
		t.Fatalf("handler %v, peek %v", handler, peek)
	}
	// The precondition that makes this test mean something.
	if peek.Pos() < handler.Pos() || peek.End() > handler.End() {
		t.Fatalf("peek (%d-%d) no longer lies within the handler's offsets (%d-%d); adjust the fixture", peek.Pos(), peek.End(), handler.Pos(), handler.End())
	}
	if within(peek, handler) {
		t.Error("within reports lib.ts's peek as declared inside main.ts's handler")
	}
}
