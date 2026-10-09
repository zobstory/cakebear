package main

import (
	"strings"
	"testing"
)

// helloServer is the M1 acceptance program. It also runs unmodified under
// Node, so it must type-check here with no import beyond node:http.
const helloServer = `import { createServer } from "node:http";

const server = createServer((req, res) => {
  res.setHeader("Content-Type", "text/plain");
  if (req.url === "/health") {
    res.end("ok\n");
  } else {
    res.end("hello from cakebear\n");
  }
});

server.listen(3000, () => {
  console.log("listening on http://localhost:3000");
});
`

// cakec bundles node:http's declarations, so a Node server type-checks with no
// npm install, no @types/node and no reference directive. Strict mode is on,
// so this also proves req and res are typed from createServer's signature
// rather than falling back to an implicit any.
func TestNodeHTTPServerTypeChecks(t *testing.T) {
	t.Parallel()

	code, out := build(t, writeTS(t, "hello.ts", helloServer))
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d\n%s", code, exitOK, out)
	}
	if !strings.Contains(out, "type-checks clean") {
		t.Errorf("output = %q, want a clean check", out)
	}
}

// The forms of import a Node program uses all reach the same declarations.
func TestNodeHTTPImportForms(t *testing.T) {
	t.Parallel()

	const body = `((req, res) => { res.end(req.url); }).listen(8080);` + "\n"
	for _, tt := range []struct{ name, src string }{
		{"named from http", `import { createServer } from "http"; createServer` + body},
		{"renamed", `import { createServer as serve } from "node:http"; serve` + body},
		{"namespace node:http", `import * as http from "node:http"; http.createServer` + body},
		{"namespace http", `import * as http from "http"; http.createServer` + body},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if code, out := build(t, writeTS(t, "server.ts", tt.src)); code != exitOK {
				t.Errorf("exit code = %d, want %d\n%s", code, exitOK, out)
			}
		})
	}
}

// The declarations hold only what cakebear's runtime implements, so an API
// it lacks is caught by the type checker at the call site, in TypeScript's
// own words, rather than refused later by lowering.
func TestNodeHTTPUndeclaredAPIIsATypeError(t *testing.T) {
	t.Parallel()

	src := `import { createServer } from "node:http";
createServer((req, res) => { res.flushHeaders(); });
`
	code, out := build(t, writeTS(t, "flush.ts", src))
	if code != exitErrors {
		t.Errorf("exit code = %d, want %d", code, exitErrors)
	}
	for _, want := range []string{"TS2339", "flushHeaders", "ServerResponse"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// A deliberate narrowing from @types/node, which has `string | undefined`
// because IncomingMessage doubles as a client response. A server request
// always has both, and cakebear has no nullable representation yet.
func TestNodeHTTPRequestURLAndMethodAreStrings(t *testing.T) {
	t.Parallel()

	src := `import { createServer } from "node:http";
createServer((req, res) => {
  const path: string = req.url;
  const verb: string = req.method;
  res.end(verb + " " + path);
});
`
	if code, out := build(t, writeTS(t, "narrow.ts", src)); code != exitOK {
		t.Errorf("exit code = %d, want %d\n%s", code, exitOK, out)
	}
}
