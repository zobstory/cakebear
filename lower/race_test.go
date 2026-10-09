package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

const raceImport = "import { createServer, IncomingMessage, ServerResponse } from \"node:http\";\n"

// The M1 target: exactly two diagnostics, naming the variable and the
// function, at the line of each. Any third would mean something safe was
// refused; any fewer, that a race compiled.
func TestRaceRuleRefusesSharedStateTarget(t *testing.T) {
	t.Parallel()

	_, errs := lowerSource(t, `import { createServer } from "node:http";

let hits: number = 0;

function bump(): void {
  hits = hits + 1;
}

const server = createServer((req, res) => {
  hits = hits + 1;
  bump();
  res.end("ok\n");
});

server.listen(3000);
`)
	want := []struct {
		line int
		msg  string
	}{
		{10, "this handler runs on many requests at once, so it can't change `hits`, which they all share"},
		{11, "this handler runs on many requests at once, so it can't call `bump()`, which changes `hits`, shared by them all"},
	}
	if len(errs) != len(want) {
		t.Fatalf("got %d diagnostics, want %d:\n%s", len(errs), len(want), joinMsgs(errs))
	}
	for i, w := range want {
		if errs[i].Span.Line != w.line || errs[i].Msg != w.msg || errs[i].Kind != Unsupported {
			t.Errorf("diagnostic %d = line %d %q (kind %d), want line %d %q", i, errs[i].Span.Line, errs[i].Msg, errs[i].Kind, w.line, w.msg)
		}
	}
}

// A handler that reads only consts and calls a function that touches nothing
// shared is accepted, and its literal is marked as run concurrently. fib's own
// parameter is mutable but per call, so it is not shared.
func TestRaceRuleAcceptsConstsAndPureFunctions(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, raceImport+`function fib(n: number): number {
  if (n < 2) {
    return n;
  }
  return fib(n - 1) + fib(n - 2);
}
function counter(): number {
  let n: number = 0;
  const inc = (): void => {
    n = n + 1;
  };
  inc();
  return n;
}
const body: string = "done\n";
const server = createServer((req, res) => {
  let local: number = fib(20) + counter();
  local = local + 1;
  const bumpLocal = (): void => {
    local = local + 1;
  };
  bumpLocal();
  if (local > 0) {
    res.end(body);
  }
});
server.listen(3000, () => {
  console.log("listening");
});
`)
	handler := m.Main[1].(*ir.VarDecl).Init.(*ir.HostCall).Args[0].(*ir.FuncLit)
	if !handler.Concurrent {
		t.Error("createServer's listener is not marked Concurrent")
	}
	listenCB := m.Main[2].(*ir.ExprStmt).Expr.(*ir.HostCall).Args[1].(*ir.FuncLit)
	if listenCB.Concurrent {
		t.Error("listen's callback runs once, on main, and must not be marked Concurrent")
	}
}

func TestRaceRuleRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"read a shared let", `let greeting: string = "hi";
createServer((req, res) => { res.end(greeting); });`,
			"can't read `greeting`, which can change while it runs; copy it into a `const` outside the handler"},
		{"through two calls", `let hits: number = 0;
function inner(): void { hits = hits + 1; }
function outer(): void { inner(); }
createServer((req, res) => { outer(); res.end(); });`,
			"can't call `outer()`, which changes `hits`, shared by them all"},
		{"a function that only reads", `let label: string = "a";
function show(): string { return label; }
createServer((req, res) => { res.end(show()); });`,
			"can't call `show()`, which reads `label`, which can change while it runs"},
		{"mutual recursion", `let hits: number = 0;
function ping(n: number): void { if (n > 0) { pong(n - 1); } }
function pong(n: number): void { hits = hits + 1; ping(n); }
createServer((req, res) => { ping(3); res.end(); });`,
			"can't call `ping()`, which changes `hits`, shared by them all"},
		{"a function passed on", `let hits: number = 0;
function bump(): void { hits = hits + 1; }
function twice(f: () => void): void { f(); f(); }
createServer((req, res) => { twice(bump); res.end(); });`,
			"can't use `bump`, which changes `hits`, shared by them all"},
		{"a const closure over an outer let", `function start(): void {
  let n: number = 0;
  const inc = (): void => { n = n + 1; };
  createServer((req, res) => { inc(); res.end(); }).listen(3000);
}`, "can't call `inc()`, which changes `n`, shared by them all"},
		{"an enclosing parameter", `function start(port: number): void {
  createServer((req, res) => { const p: number = port; res.end(); }).listen(port);
}`, "can't read `port`, which can change while it runs"},
		// ++ is refused by lowering too; the race rule must still call it a write.
		{"postfix increment", `let hits: number = 0;
createServer((req, res) => { hits++; res.end(); });`, "can't change `hits`, which they all share"},
		{"prefix decrement", `let hits: number = 0;
createServer((req, res) => { --hits; res.end(); });`, "can't change `hits`, which they all share"},
		{"a nested closure", `let hits: number = 0;
createServer((req, res) => { const f = (): void => { hits = hits + 1; }; f(); res.end(); });`,
			"can't change `hits`, which they all share"},
		{"a named handler", `let hits: number = 0;
function handle(req: IncomingMessage, res: ServerResponse): void { hits = hits + 1; res.end(); }
createServer(handle);`,
			"the handler `handle` runs on many requests at once, so it can't change `hits`, which they all share"},
		{"a handler it cannot see", `function make(): (req: IncomingMessage, res: ServerResponse) => void {
  return (req, res) => { res.end(); };
}
createServer(make());`,
			"cakebear can only check a request handler written inline or declared as a function"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, raceImport+tt.src+"\n")
			if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
			for _, e := range errs {
				if e.Kind != Unsupported {
					t.Errorf("%q is reported as an error; the race rule is a limit of this phase", e.Msg)
				}
			}
		})
	}
}

// Each shared binding is reported once, at its first write when there is one,
// so `hits = hits + 1` is one diagnostic, not a read and a write.
func TestRaceRuleReportsEachBindingOnce(t *testing.T) {
	t.Parallel()

	_, errs := lowerSource(t, raceImport+`let hits: number = 0;
createServer((req, res) => {
  const before: number = hits;
  hits = hits + 1;
  hits = hits + 1;
  res.end();
});
`)
	if len(errs) != 1 {
		t.Fatalf("got %d diagnostics, want 1:\n%s", len(errs), joinMsgs(errs))
	}
	if !strings.Contains(errs[0].Msg, "change `hits`") || errs[0].Span.Line != 5 {
		t.Errorf("diagnostic = line %d %q, want the first write, on line 5", errs[0].Span.Line, errs[0].Msg)
	}
}
