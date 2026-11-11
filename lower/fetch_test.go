package lower

import (
	"strings"
	"testing"
)

// TestFetchRefusals: what S1's first step doesn't lower of fetch, a Response
// or a Headers is refused by name, saying what to do where there is
// something.
func TestFetchRefusals(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, src, want string }{
		{"an init in a variable", "const init: RequestInit = { method: \"POST\" };\nconst r: Response = await fetch(\"http://a.test/\", init);\nexport {};\n",
			"fetch's init must be an object literal here"},
		{"a signal from a controller", "const r: Response = await fetch(\"http://a.test/\", { signal: new AbortController().signal });\nexport {};\n",
			"AbortController is not supported yet: fetch's signal comes from AbortSignal.timeout"},
		{"a mode", "const r: Response = await fetch(\"http://a.test/\", { mode: \"cors\" });\nexport {};\n",
			"fetch's mode is not supported yet"},
		{"a redirect that isn't written", "const m: RequestRedirect = \"manual\";\nconst r: Response = await fetch(\"http://a.test/\", { redirect: m });\nexport {};\n",
			"fetch's redirect must be"},
		{"spread headers", "const base = { a: \"1\" };\nconst r: Response = await fetch(\"http://a.test/\", { headers: { ...base } });\nexport {};\n",
			"spreading into headers is not supported yet"},
		{"a body that may be undefined", "const b: string | undefined = undefined;\nconst r: Response = await fetch(\"http://a.test/\", { method: \"POST\", body: b });\nexport {};\n",
			"fetch's body must be a string here"},
		{"json() as any", "const r: Response = await fetch(\"http://a.test/\");\nconst v = await r.json();\nexport {};\n",
			"Response.json() returns any, which cakebear doesn't lower: give what it resolves to a type"},
		{"JSON.parse as any", "const v = JSON.parse(\"1\");\nexport {};\n",
			"JSON.parse returns any, which cakebear doesn't lower: give its result a type"},
		{"a reviver", "const v: unknown = JSON.parse(\"1\", (k: string, x: unknown): unknown => x);\nexport {};\n",
			"JSON.parse with a reviver is not supported yet"},
		{"an element as any", "const v: unknown = JSON.parse(\"[1]\");\nif (Array.isArray(v)) {\n  const x = v[0];\n  console.log(x);\n}\nexport {};\n",
			"any is not supported yet"},
		{"Array.isArray of a known type", "const xs: number[] = [1];\nconsole.log(Array.isArray(xs));\n",
			"Array.isArray of a value whose type is known is not supported yet"},
		{"arrayBuffer()", "const r: Response = await fetch(\"http://a.test/\");\nawait r.arrayBuffer();\nexport {};\n",
			"a body as bytes is not supported yet"},
		{"clone()", "const r: Response = await fetch(\"http://a.test/\");\nconst c: Response = r.clone();\nexport {};\n",
			"Response.clone() is not supported yet"},
		{"a Headers' forEach", "const h: Headers = new Headers();\nh.forEach((v: string): void => {});\n",
			"Headers.forEach is not supported yet"},
		{"text as a value", "const r: Response = await fetch(\"http://a.test/\");\nconst f: () => Promise<string> = r.text;\nexport {};\n",
			"Response.text can only be called"},
	} {
		_, errs := lowerProgram(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors = %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}
}

// TestOwnFetchIsAnOrdinaryFunction: fetch is recognised by where it is
// declared, so a program's own is called as any function is.
func TestOwnFetchIsAnOrdinaryFunction(t *testing.T) {
	t.Parallel()
	_, errs := lowerProgram(t, "function fetch(url: string): string {\n  return url + \"!\";\n}\nconsole.log(fetch(\"x\"));\n")
	if len(errs) > 0 {
		t.Fatalf("errors = %q, want none", joinMsgs(errs))
	}
}
