package lower

import (
	"strings"
	"testing"
)

// S1 step 4 (plan D5): what Promise's statics and adoption still refuse,
// each by name as a limit of this phase. promise_e2e_test.go, in cmd/cakec,
// holds what they lower to Node.
func TestPromiseRefusals(t *testing.T) {
	t.Parallel()
	const fns = "async function n(): Promise<number> {\n  return 1;\n}\nasync function s(): Promise<string> {\n  return \"s\";\n}\n"
	for _, tt := range []struct{ name, src, want string }{
		{"allSettled", "Promise.allSettled([n()]);\n", "Promise.allSettled is not supported yet"},
		{"any", "const p = Promise.any([n()]);\n", "Promise.any is not supported yet"},
		{"then", "n().then((v) => {});\n", "promise methods such as .then are not supported yet"},
		{"catch", "n().catch(() => {});\n", "promise methods such as .catch are not supported yet"},
		{"finally", "n().finally(() => {});\n", "promise methods such as .finally are not supported yet"},
		{"a tuple of values", "console.log(await Promise.all([n(), s()]));\n",
			"Promise.all of promises of different types is a tuple, which is not supported yet: destructure it"},
		{"a tuple bound to a name", "const t = await Promise.all([n(), s()]);\n", "tuples are not supported yet"},
		{"race of different types", "console.log(await Promise.race([n(), s()]));\n",
			"Promise.race of promises of different types is not supported yet"},
		{"race of none", "const u = await Promise.race([]);\n", "Promise.race of an empty array literal"},
		{"a default in the pattern", "const [a = 1] = await Promise.all([n()]);\n", "only a name is supported here, for each of Promise.all's values"},
		{"a rest element", "const [...r] = await Promise.all([n(), n()]);\n", "only a name is supported here"},
		{"a value of nothing", "function sleep(): Promise<void> {\n  return new Promise((resolve) => resolve());\n}\nconst [a, b] = await Promise.all([n(), sleep()]);\n",
			"b would be undefined, the value of a promise of nothing"},
		{"promises of nothing as a value", "function sleep(): Promise<void> {\n  return new Promise((resolve) => resolve());\n}\nconsole.log(await Promise.all([sleep(), sleep()]));\n",
			"Promise.all of promises of nothing as a value is not supported yet"},
		{"adopting in a try", "async function f(): Promise<number> {\n  try {\n    return n();\n  } finally {\n    console.log(\"f\");\n  }\n}\n",
			"returning a promise from inside a try is not supported yet"},
		{"adopting in a catch with a finally", "async function f(): Promise<number> {\n  try {\n    return 1;\n  } catch {\n    return n();\n  } finally {\n    console.log(\"f\");\n  }\n}\n",
			"returning a promise from inside a try is not supported yet"},
		{"a promise shared with handlers", "import { createServer } from \"node:http\";\nconst ready: Promise<number> = n();\n" +
			"createServer(async (req, res) => {\n  const [x] = await Promise.all([ready]);\n  res.end(String(x));\n}).listen(1);\n",
			"can't use `ready`, a promise created outside it"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, fns+tt.src+"export {};\n")
			if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
			for _, e := range errs {
				if e.Kind != Unsupported {
					t.Errorf("%q is reported as an error; it is a limit of this phase", e.Msg)
				}
			}
		})
	}
}

// TestOwnPromiseIsAnOrdinaryBinding: Promise's statics are recognised by
// where Promise is declared, so a program's own is an ordinary object.
func TestOwnPromiseIsAnOrdinaryBinding(t *testing.T) {
	t.Parallel()
	_, errs := lowerSource(t, "interface P { all: (x: number) => number }\nconst Promise: P = { all: (x: number): number => x + 1 };\nconsole.log(Promise.all(1));\nexport {};\n")
	if msgs := joinMsgs(errs); strings.Contains(msgs, "Promise.") {
		t.Fatalf("errors = %q, want none about Promise's statics", msgs)
	}
}
