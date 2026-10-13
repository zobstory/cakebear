package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

const fibDecl = `function fib(n: number): number {
  if (n < 2) {
    return n;
  }
  return fib(n - 1) + fib(n - 2);
}
`

// spawn is a global from cakebear.d.ts, with no import. A call is a host
// operation whose argument is a concurrent function value, and whose result is
// a promise of what that function returns.
func TestLowerSpawn(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, fibDecl+sleepDecl+`const p: Promise<number> = spawn(() => fib(30));
const q: Promise<void> = spawn(async () => {
  await sleep(1); // a promise it makes itself; an outer one would be refused
});
`)
	call := hostCallOf(t, must[*ir.VarDecl](t, m.Main[0], "const p").Init, ir.HostSpawn)
	if !call.Typ.Equal(ir.PromiseOf(ir.Number)) {
		t.Errorf("spawn(() => fib(30)) has type %v, want Promise<number>", call.Typ)
	}
	fn := must[*ir.FuncLit](t, call.Args[0], "the spawned function")
	if !fn.Concurrent || fn.Result != ir.Number || len(fn.Params) != 0 {
		t.Errorf("spawned function: concurrent=%v result=%v params=%d, want a concurrent () => number",
			fn.Concurrent, fn.Result, len(fn.Params))
	}
	async := hostCallOf(t, must[*ir.VarDecl](t, m.Main[1], "const q").Init, ir.HostSpawn)
	if lit := must[*ir.FuncLit](t, async.Args[0], "the async spawned function"); !lit.Async || !lit.Result.Equal(voidPromise) {
		t.Errorf("async spawned function: async=%v result=%v, want async returning Promise<void>", lit.Async, lit.Result)
	}
}

// Recognised by declaration: a program's own spawn is an ordinary call.
func TestOwnSpawnIsAnOrdinaryCall(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, "function spawn(f: () => number): number {\n  return f();\n}\nconst n: number = spawn(() => 1);\n")
	if call := must[*ir.Call](t, must[*ir.VarDecl](t, m.Main[0], "const n").Init, "the call"); call.Callee != "spawn" {
		t.Errorf("callee = %q, want the program's spawn", call.Callee)
	}
}

// A spawned function runs in parallel with its caller, so it is held to the
// race rule a request handler is: nothing it reaches from outside may change,
// directly or through what it calls.
func TestSpawnRaceRule(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"a shared let", "let total: number = 0;\nconst p: Promise<void> = spawn(() => {\n  total = total + 1;\n});\n",
			"this spawned function runs in parallel with its caller, so it can't change `total`, which its caller shares"},
		{"reading a let", "let total: number = 0;\nconst p: Promise<number> = spawn(() => total);\n",
			"so it can't read `total`, which can change while it runs; copy it into a `const` outside the spawned function"},
		{"through a function", "let total: number = 0;\nfunction bump(): void {\n  total = total + 1;\n}\nconst p: Promise<void> = spawn(() => bump());\n",
			"so it can't call `bump()`, which changes `total`, shared with its caller"},
		{"an outer promise", sleepDecl + "const ready: Promise<void> = sleep(1);\nconst p: Promise<void> = spawn(async () => {\n  await ready;\n});\n",
			"so it can't use `ready`, a promise created outside it; a promise can't be shared with a spawned function yet"},
		{"the handler's response", `import { createServer } from "node:http";
createServer(async (req, res) => {
  const r = res;
  await spawn(() => {
    r.end("from a spawn");
  });
}).listen(3000);
`, "so it can't use `r`, a node:http value created outside it; one can't be shared with a spawned function yet"},
		{"a parameter", "function run(n: number): Promise<number> {\n  return spawn(() => n + 1);\n}\n",
			"so it can't read `n`, which can change while it runs"},
		{"a named function", "let total: number = 0;\nfunction work(): number {\n  return total;\n}\nconst p: Promise<number> = spawn(work);\n",
			"the spawned function `work` runs in parallel with its caller, so it can't read `total`"},
		{"a function taking an argument", "function f(x: number): number {\n  return x;\n}\nconst p: Promise<number> = spawn(f);\n",
			"spawn needs a function that takes no arguments, and this one is (number) => number"},
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

// What a spawned function may use: consts holding values that can't change,
// its own locals, and pure functions.
func TestSpawnMayUseWhatCannotChange(t *testing.T) {
	t.Parallel()

	lowerClean(t, fibDecl+`const n: number = 30;
const p: Promise<number> = spawn(() => {
  let local: number = fib(n);
  local = local + 1;
  return local;
});
`)
}

// D10: a const host value is shared state for a handler too. Reaching an
// outer server from a handler would call into it from many goroutines at once.
func TestHandlerReachingAnOuterServerRefused(t *testing.T) {
	t.Parallel()

	_, errs := lowerSource(t, `import { createServer } from "node:http";
const other = createServer((req, res) => {
  res.end("other");
});
createServer((req, res) => {
  other.listen(3001);
  res.end("ok");
}).listen(3000);
`)
	if want := "this handler runs on many requests at once, so it can't use `other`, a node:http value created outside it; one can't be shared between requests yet"; !strings.Contains(joinMsgs(errs), want) {
		t.Errorf("errors = %q, want one containing %q", joinMsgs(errs), want)
	}
}
