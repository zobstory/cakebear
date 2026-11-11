package lower

import (
	"strings"
	"testing"
)

// S1 step 5 (plan D6, D2): what URL, URLSearchParams and AbortSignal still
// refuse, each by name as a limit of this phase. url_e2e_test.go and
// abort_e2e_test.go, in cmd/cakec, hold what they lower to Node.
func TestURLRefusals(t *testing.T) {
	t.Parallel()
	const u = "const u = new URL(\"https://a.test/x?q=1\");\nconst p = new URLSearchParams(\"a=1\");\n"
	for _, tt := range []struct{ name, src, want string }{
		{"pairs", "const q = new URLSearchParams([[\"a\", \"1\"]]);\n", "tuples are not supported yet"},
		{"entries", u + "p.entries();\n", "URLSearchParams.entries is not supported yet; forEach goes over its pairs"},
		{"keys", u + "p.keys();\n", "URLSearchParams.keys is not supported yet"},
		{"a method as a value", u + "const g: (n: string) => string | null = p.get;\n", "URLSearchParams.get can only be called, not used as a value, yet"},
		{"AbortSignal.abort", "const s = AbortSignal.abort();\n", "AbortSignal.abort is not supported yet"},
		{"AbortSignal.any", "const s = AbortSignal.any([AbortSignal.timeout(1)]);\n", "AbortSignal.any is not supported yet"},
		{"a signal's members", "const s = AbortSignal.timeout(5);\nconsole.log(s.aborted);\n",
			"AbortSignal.aborted is not supported yet: a signal is only fetch's, from AbortSignal.timeout"},
		{"AbortController", "new AbortController();\n", "new AbortController is not supported yet"},
		{"a setter from a number", u + "u.port = String(1);\nu.hash = `${1}`;\nconst n: number = 2;\n// @ts-ignore\nu.port = n;\n",
			"a URL's port is set from a string here, not a number"},
		{"a spawn's URL", "const s = await spawn(() => new URL(\"https://a.test/\"));\n", "spawn's result holds a URL, which belongs to the spawned function's domain"},
		{"a URL as an unknown", "const x: unknown = new URL(\"https://a.test/\");\n", "a URL as an unknown is not supported"},
		{"throwing a URL", "throw new URL(\"https://a.test/\");\n", "throwing a URL is not supported"},
		{"a URL shared with handlers", "import { createServer } from \"node:http\";\nconst base = new URL(\"https://a.test/\");\n" +
			"createServer((req, res) => {\n  res.end(base.href);\n}).listen(1);\n", "can't use `base`"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, tt.src+"export {};\n")
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

// TestOwnURLIsAnOrdinaryBinding: URL is recognised by where it is
// declared, so a program's own is an ordinary function.
func TestOwnURLIsAnOrdinaryBinding(t *testing.T) {
	t.Parallel()
	_, errs := lowerSource(t, "function URL(s: string): string {\n  return s + \"!\";\n}\nconsole.log(URL(\"x\"));\nexport {};\n")
	if len(errs) > 0 {
		t.Fatalf("errors = %q, want none", joinMsgs(errs))
	}
}
