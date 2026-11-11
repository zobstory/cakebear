package lower

import (
	"strings"
	"testing"
)

// S1 step 7, done-check 10: the race rule over S1's I/O. A request handler
// may use only what it makes itself, and what never changes: each I/O value
// made outside it is refused, naming what it is, and a primitive, a frozen
// const and process.env are read freely. io_race_e2e_test.go, in cmd/cakec,
// serves the handler that uses all of it from a race build.
func TestIORaceRefusals(t *testing.T) {
	t.Parallel()
	const head = "import { createServer } from \"node:http\";\ninterface Config { url: string }\n"
	handler := func(outer, body string) string {
		return head + outer + "createServer(async (req, res) => {\n" + body + "}).listen(1);\nexport {};\n"
	}
	for _, tt := range []struct{ name, src, want string }{
		{"a Response", handler("const r0 = await fetch(\"http://a.test/\");\n", "  res.end(String(r0.status));\n"),
			"can't use `r0`, a Response created outside it; one can't be shared between requests yet"},
		{"a Headers", handler("const h = new Headers();\n", "  const r = await fetch(\"http://a.test/\", { headers: h });\n  res.end(String(r.status));\n"),
			"can't use `h`, a Headers created outside it"},
		{"a URL", handler("const base = new URL(\"http://a.test/\");\n", "  res.end(base.href);\n"),
			"can't use `base`, a URL created outside it"},
		{"a URLSearchParams", handler("const params = new URLSearchParams(\"a=1\");\n", "  res.end(params.toString());\n"),
			"can't use `params`, a URLSearchParams created outside it"},
		{"an AbortSignal", handler("const sig = AbortSignal.timeout(1000);\n", "  const r = await fetch(\"http://a.test/\", { signal: sig });\n  res.end(String(r.status));\n"),
			"can't use `sig`, an AbortSignal created outside it"},
		{"a fetch's promise", handler("const pending = fetch(\"http://a.test/\");\n", "  const r = await pending;\n  res.end(String(r.status));\n"),
			"can't use `pending`, a promise created outside it"},
		{"JSON as an unknown", handler("const data: unknown = JSON.parse(\"{}\");\n", "  res.end(String(data));\n"),
			"can't use `data`, an unknown created outside it"},
		{"JSON cast to a shape", handler("const cfg: Config = JSON.parse(\"{}\");\n", "  res.end(cfg.url);\n"),
			"can't use `cfg`, an object created outside it"},
		{"a let a handler writes", handler("let last: string = \"\";\n", "  const r = await fetch(\"http://a.test/\");\n  last = await r.text();\n  res.end(last);\n"),
			"can't change `last`, which they all share"},
		{"through a function", handler("const shared = new URL(\"http://a.test/\");\nasync function get(): Promise<number> {\n  const r = await fetch(shared);\n  return r.status;\n}\n",
			"  res.end(String(await get()));\n"), "can't call `get()`, which uses `shared`, a URL created outside the handler"},
		{"a spawn's Response", "const s = await spawn(async () => await fetch(\"http://a.test/\"));\nexport {};\n",
			"spawn's result holds a Response, which belongs to the spawned function's domain"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, tt.src)
			if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
		})
	}
}

// TestIOSharedFreely: what never changes is read freely by a handler: a
// string from process.env, a frozen const, and process.env itself.
func TestIOSharedFreely(t *testing.T) {
	t.Parallel()
	_, errs := lowerSource(t, "import { createServer } from \"node:http\";\n"+
		"const api: string = process.env.API ?? \"http://a.test\";\nconst paths: { readonly items: string } = { items: \"/items\" };\n"+
		"createServer(async (req, res) => {\n  const r = await fetch(api + paths.items, { signal: AbortSignal.timeout(100) });\n"+
		"  res.end(String(r.status) + (process.env.WHO ?? \"\"));\n}).listen(1);\nexport {};\n")
	if len(errs) > 0 {
		t.Fatalf("errors = %q, want none", joinMsgs(errs))
	}
}
