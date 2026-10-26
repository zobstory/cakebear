package main

import (
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestUncaughtReportsQuoteAsNode (M6 plan D6): an uncaught error's report
// names the place it points at, quotes that line of the source with its
// types blanked, and puts a caret under the column V8 records for the
// operation that threw, which is often not where its expression starts
// (lower/caret.go). Each case's excerpt and error are Node 24.18's for the
// same file, under --stack-trace-limit=0. Each throws from a timer, as a
// request handler would, so the report is the one after the program's own
// output.
func TestUncaughtReportsQuoteAsNode(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, src string
		line      int    // the line the report names
		excerpt   string // the source line and the caret under it
		body      string // the error, as Node prints it
	}{
		{"read", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[] }\nconst b: B = {};\nsetTimeout(() => {\n  console.log(b.p!.x);\n}, 0);\n", 6, "  console.log(b.p .x);\n                   ^", "[TypeError: Cannot read properties of undefined (reading 'x')]"},
		{"write", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[] }\nconst b: B = {};\nsetTimeout(() => {\n  b.p!.x = 1;\n}, 0);\n", 6, "  b.p .x = 1;\n         ^", "[TypeError: Cannot set properties of undefined (setting 'x')]"},
		{"index", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[] }\nconst b: B = {};\nsetTimeout(() => {\n  console.log(b.xs![0]);\n}, 0);\n", 6, "  console.log(b.xs [0]);\n                   ^", "[TypeError: Cannot read properties of undefined (reading '0')]"},
		{"idxread2", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  console.log(u![0]);\n}, 0);\n", 8, "  console.log(u [0]);\n                ^", "[TypeError: Cannot read properties of undefined (reading '0')]"},
		{"idxmaybe", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  let w: (number | undefined)[] | undefined; console.log(w![0]);\n}, 0);\n", 8, "  let w                                    ; console.log(w [0]);\n                                                           ^", "[TypeError: Cannot read properties of undefined (reading '0')]"},
		{"stridx", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  console.log(b.s![0]);\n}, 0);\n", 8, "  console.log(b.s [0]);\n                  ^", "[TypeError: Cannot read properties of undefined (reading '0')]"},
		{"iterate", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[] }\nconst b: B = {};\nsetTimeout(() => {\n  for (const v of b.xs!) { console.log(v); }\n}, 0);\n", 6, "  for (const v of b.xs ) { console.log(v); }\n                    ^", "[TypeError: b.xs is not iterable]"},
		{"iterparen", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  for (const v of (b.xs!)) { console.log(v); }\n}, 0);\n", 8, "  for (const v of (b.xs )) { console.log(v); }\n                     ^", "[TypeError: b.xs is not iterable]"},
		{"iterelem", "export {};\nconst zs: (number[] | undefined)[] = [undefined];\nsetTimeout(() => {\n  for (const v of zs[0]!) { console.log(v); }\n}, 0);\n", 4, "  for (const v of zs[0] ) { console.log(v); }\n                    ^", "[TypeError: zs[0] is not iterable]"},
		{"bareiter", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  for (const v of u!) { console.log(v); }\n}, 0);\n", 8, "  for (const v of u ) { console.log(v); }\n                  ^", "[TypeError: u is not iterable]"},
		{"in", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[] }\nconst b: B = {};\nsetTimeout(() => {\n  console.log(\"x\" in b.p!);\n}, 0);\n", 6, "  console.log(\"x\" in b.p );\n                  ^", "[TypeError: Cannot use 'in' operator to search for 'x' in undefined]"},
		{"deleteprop", "export {};\ninterface Q { x?: number }\ninterface B { q?: Q }\nconst b: B = {};\nsetTimeout(() => {\n  delete b.q!.x;\n}, 0);\n", 6, "  delete b.q .x;\n           ^", "[TypeError: Cannot convert undefined or null to object]"},
		{"deleteelem", "export {};\ninterface Q { x?: number }\nconst qs: (Q | undefined)[] = [undefined];\nsetTimeout(() => {\n  delete qs[0]!.x;\n}, 0);\n", 5, "  delete qs[0] .x;\n           ^", "[TypeError: Cannot convert undefined or null to object]"},
		{"deleteident", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  let q: { x?: number } | undefined; delete q!.x;\n}, 0);\n", 8, "  let q                            ; delete q .x;\n                                     ^", "[TypeError: Cannot convert undefined or null to object]"},
		{"idxset", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  b.xs![0] = 1;\n}, 0);\n", 8, "  b.xs [0] = 1;\n           ^", "[TypeError: Cannot set properties of undefined (setting '0')]"},
		{"setlen", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  xs.length = -1;\n}, 0);\n", 8, "  xs.length = -1;\n            ^", "[RangeError: Invalid array length]"},
		{"lencompound", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  xs.length -= 5;\n}, 0);\n", 8, "  xs.length -= 5;\n            ^", "[RangeError: Invalid array length]"},
		{"lendec", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  const zs: number[] = []; zs.length--;\n}, 0);\n", 8, "  const zs           = []; zs.length--;\n                                    ^", "[RangeError: Invalid array length]"},
		{"lenpredec", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  const zs: number[] = []; --zs.length;\n}, 0);\n", 8, "  const zs           = []; --zs.length;\n                                ^", "[RangeError: Invalid array length]"},
		{"bareread", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  console.log(u!.length);\n}, 0);\n", 8, "  console.log(u .length);\n                 ^", "[TypeError: Cannot read properties of undefined (reading 'length')]"},
		{"push", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  b.xs!.push(1);\n}, 0);\n", 8, "  b.xs .push(1);\n        ^", "[TypeError: Cannot read properties of undefined (reading 'push')]"},
		{"mapcall", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  console.log(b.xs!.map((x) => x + 1));\n}, 0);\n", 8, "  console.log(b.xs .map((x) => x + 1));\n                    ^", "[TypeError: Cannot read properties of undefined (reading 'map')]"},
		{"popstmt", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  b.xs!.pop();\n}, 0);\n", 8, "  b.xs .pop();\n        ^", "[TypeError: Cannot read properties of undefined (reading 'pop')]"},
		{"popval", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  console.log(b.xs!.pop());\n}, 0);\n", 8, "  console.log(b.xs .pop());\n                    ^", "[TypeError: Cannot read properties of undefined (reading 'pop')]"},
		{"strlen", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  console.log(b.s!.length);\n}, 0);\n", 8, "  console.log(b.s .length);\n                   ^", "[TypeError: Cannot read properties of undefined (reading 'length')]"},
		{"strrecv", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  console.log(b.s!.repeat(2));\n}, 0);\n", 8, "  console.log(b.s .repeat(2));\n                   ^", "[TypeError: Cannot read properties of undefined (reading 'repeat')]"},
		{"repeat", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[] }\nconst b: B = {};\nsetTimeout(() => {\n  console.log(\"ab\".repeat(-1));\n}, 0);\n", 6, "  console.log(\"ab\".repeat(-1));\n                   ^", "[RangeError: Invalid count value: -1]"},
		{"multiline", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  console.log(\"ab\"\n    .repeat(-1));\n}, 0);\n", 9, "    .repeat(-1));\n     ^", "[RangeError: Invalid count value: -1]"},
		{"tofixed", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[] }\nconst b: B = {};\nsetTimeout(() => {\n  console.log((1.5).toFixed(101));\n}, 0);\n", 6, "  console.log((1.5).toFixed(101));\n                    ^", "[RangeError: toFixed() digits argument must be between 0 and 100]"},
		{"tofixed0", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  let n: number | undefined; console.log(n!.toFixed());\n}, 0);\n", 8, "  let n                    ; console.log(n .toFixed());\n                                            ^", "[TypeError: Cannot read properties of undefined (reading 'toFixed')]"},
		{"numtostr", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  let n: number | undefined; console.log(n!.toString());\n}, 0);\n", 8, "  let n                    ; console.log(n .toString());\n                                            ^", "[TypeError: Cannot read properties of undefined (reading 'toString')]"},
		{"errtostr", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  let e: Error | undefined; console.log(e!.toString());\n}, 0);\n", 8, "  let e                   ; console.log(e .toString());\n                                           ^", "[TypeError: Cannot read properties of undefined (reading 'toString')]"},
		{"uri", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[]; s?: string }\nconst b: B = {};\nlet u: number[] | undefined;\nconst xs: number[] = [1];\nsetTimeout(() => {\n  console.log(decodeURIComponent(\"%\"));\n}, 0);\n", 8, "  console.log(decodeURIComponent(\"%\"));\n              ^", "[URIError: URI malformed]"},
		{"stringify", "export {};\ninterface P { x: number }\ninterface B { p?: P; xs?: number[] }\nconst b: B = {};\nsetTimeout(() => {\n  const o: { self?: unknown } = {};\n  o.self = o;\n  console.log(JSON.stringify(o));\n}, 0);\n", 8, "  console.log(JSON.stringify(o));\n                   ^", "[TypeError: Converting circular structure to JSON\n    --> starting at object with constructor 'Object'\n    --- property 'self' closes the circle]"},
		{"throwstr", "export {};\nfunction f(n: number): number {\n  if (n > 1) {\n    throw new RangeError(\"too big: \" + String(n));\n  }\n  return n;\n}\nsetTimeout(() => {\n  console.log(f(1));\n  console.log(f(2));\n}, 0);\n", 4, "    throw new RangeError(\"too big: \" + String(n));\n    ^", "[RangeError: too big: 2]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkReport(t, buildProgram(t, "report_"+tt.name, tt.src), tt.line, tt.excerpt, tt.body)
		})
	}
}

// TestUncaughtHostErrorsQuoteTheCall: an error node:http throws is thrown
// from Node's own JavaScript, so Node's report quotes a line of that, such
// as `node:internal/modules/run_main:107` for this one, which a compiled
// program doesn't have. cakebear's quotes the program's call instead, the
// caret under the member, as for any method call (see the accepted
// differences). The error itself is Node's.
func TestUncaughtHostErrorsQuoteTheCall(t *testing.T) {
	t.Parallel()
	src := "import { createServer } from \"node:http\";\nconst server = createServer((req, res) => {\n  res.end(\"ok\");\n});\n" +
		"console.log(\"before\");\nserver.listen(-1, () => {\n  console.log(\"listening\");\n});\n"
	checkReport(t, buildProgram(t, "report_listen", src), 6, "server.listen(-1, () => {\n       ^",
		"[RangeError [ERR_SOCKET_BAD_PORT]: options.port should be >= 0 and < 65536. Received type number (-1).] {\n  code: 'ERR_SOCKET_BAD_PORT'\n}")
}

// checkReport runs bin, expecting it to end with an uncaught error whose
// report names line of its source, quotes excerpt, and prints body.
func checkReport(t *testing.T, bin string, line int, excerpt, body string) {
	t.Helper()
	var errOut strings.Builder
	cmd := exec.Command(bin)
	cmd.Stderr = &errOut
	var exit *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("%v, want exit 1; stderr:\n%s", err, errOut.String())
	}
	// buildProgram's source is bin's path with .ts, which a file URL writes
	// as it is while it holds only these.
	path := bin + ".ts"
	if !plainPath.MatchString(path) {
		t.Skipf("%s needs escaping in a file URL", path)
	}
	want := "file://" + path + ":" + strconv.Itoa(line) + "\n" + excerpt + "\n\n" + body + "\n\ncakebear " + version + "\n"
	if got := errOut.String(); got != want {
		t.Errorf("stderr is\n%s\nwant, as Node's report reads\n%s", got, want)
	}
}

var plainPath = regexp.MustCompile(`^[A-Za-z0-9/_.-]+$`)
