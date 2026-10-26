package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// sleepDecl is the helper every M2 target program starts with.
const sleepDecl = `function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}
`

// The M2 target programs (M2 plan §3), kept here so the tests do not depend
// on files outside the repository.
const (
	sleepSrc = sleepDecl + `
async function main(): Promise<void> {
  console.log("a");
  await sleep(20);
  console.log("b");
}

main();
console.log("c");
`

	orderingSrc = sleepDecl + `
async function now(n: number): Promise<number> {
  return n;
}

async function quick(name: string): Promise<number> {
  console.log(name + ": sync part");
  const one: number = await now(1);
  console.log(name + ": resumed (microtask)");
  return one;
}

async function slow(name: string): Promise<number> {
  console.log(name + ": before the timer");
  await sleep(0);
  console.log(name + ": after the timer (macrotask)");
  return 2;
}

async function both(): Promise<void> {
  const a: number = await quick("q");
  const b: number = await slow("s");
  if (a + b === 3) {
    console.log("both: done");
  }
}

setTimeout(() => {
  console.log("timeout 0, scheduled first");
}, 0);
both();
quick("q2");
console.log("top level done");
`

	asyncHandlerSrc = `import { createServer } from "node:http";
` + sleepDecl + `
function fib(n: number): number {
  if (n < 2) {
    return n;
  }
  return fib(n - 1) + fib(n - 2);
}

createServer(async (req, res) => {
  await sleep(10);
  const result: number = fib(36);
  if (result > 0) {
    res.end("done\n");
  }
}).listen(3000, () => {
  console.log("listening on http://localhost:3000");
});
`

	sharedAfterAwaitSrc = `import { createServer } from "node:http";

let hits: number = 0;
` + sleepDecl + `
createServer(async (req, res) => {
  await sleep(1);
  hits = hits + 1; // refused: the handler changes shared hits
  res.end("ok\n");
}).listen(3000);
`
)

// must asserts that x has type T, failing the test rather than panicking.
func must[T any](t *testing.T, x any, what string) T {
	t.Helper()
	v, ok := x.(T)
	if !ok {
		t.Fatalf("%s is %#v, want a %T", what, x, v)
	}
	return v
}

var voidPromise = ir.PromiseOf(ir.Void)

// sleep.ts: a plain function returning a promise its executor resolves from a
// timer, and an async function awaiting it.
func TestLowerSleep(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, sleepSrc)
	sleep, main := m.Funcs[0], m.Funcs[1]

	if sleep.Async || !sleep.Result.Equal(voidPromise) {
		t.Errorf("sleep: async=%v result=%v, want a plain function returning Promise<void>", sleep.Async, sleep.Result)
	}
	np := must[*ir.NewPromise](t, must[*ir.Return](t, sleep.Body[0], "sleep's statement").Value, "sleep's return value")
	exec := must[*ir.FuncLit](t, np.Executor, "the executor")
	if len(exec.Params) != 2 || exec.Params[0].Name != "resolve" || !exec.Params[0].Type.Equal(ir.ResolveType(ir.Void)) ||
		exec.Params[1].Name != "" || !exec.Params[1].Type.Equal(ir.RejectType()) {
		t.Errorf("executor params = %+v, want resolve: () => void, then reject, unnamed: the slot's, padded", exec.Params)
	}
	timer := hostCallOf(t, must[*ir.ExprStmt](t, exec.Body[0], "the executor's statement").Expr, ir.HostSetTimeout)
	if cb := must[*ir.Ident](t, timer.Args[0], "the timer's callback"); cb.Name != "resolve" || !cb.Typ.Equal(voidCallback) {
		t.Errorf("timer callback = %+v, want resolve as () => void", cb)
	}

	if !main.Async || !main.Result.Equal(voidPromise) {
		t.Errorf("main: async=%v result=%v, want async returning Promise<void>", main.Async, main.Result)
	}
	await := must[*ir.Await](t, must[*ir.ExprStmt](t, main.Body[1], "main's second statement").Expr, "await sleep(20)")
	if await.Typ != ir.Void || !must[*ir.Call](t, await.Value, "the awaited call").Typ.Equal(voidPromise) {
		t.Errorf("await = %+v, want a void await of a Promise<void> call", await)
	}
	if call := must[*ir.Call](t, must[*ir.ExprStmt](t, m.Main[0], "main()").Expr, "main()"); !call.Typ.Equal(voidPromise) {
		t.Errorf("main() has type %v, want Promise<void>", call.Typ)
	}
}

// ordering.ts lowers whole; an await's value is the promise's element, and a
// timer callback literal is a void function.
func TestLowerOrdering(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, orderingSrc)
	quick := m.Funcs[2]
	if quick.Name != "quick" || !quick.Async || !quick.Result.Equal(ir.PromiseOf(ir.Number)) {
		t.Fatalf("funcs[2] = %s async=%v result=%v, want async quick returning Promise<number>", quick.Name, quick.Async, quick.Result)
	}
	one := must[*ir.VarDecl](t, quick.Body[1], "const one")
	if await := must[*ir.Await](t, one.Init, "await now(1)"); await.Typ != ir.Number {
		t.Errorf("await now(1) has type %v, want number", await.Typ)
	}
	// An async function's return carries the element, not the promise.
	if ret := must[*ir.Return](t, quick.Body[3], "return one"); ret.Value.ExprType() != ir.Number {
		t.Errorf("return one carries %v, want number", ret.Value.ExprType())
	}
	timer := hostCallOf(t, must[*ir.ExprStmt](t, m.Main[0], "setTimeout(…)").Expr, ir.HostSetTimeout)
	if cb := must[*ir.FuncLit](t, timer.Args[0], "the timer's callback"); cb.Result != ir.Void || len(cb.Params) != 0 {
		t.Errorf("timer callback = %v, want () => void", cb.Typ)
	}
}

// An async request handler is an async literal in a void slot: it runs on
// many requests at once, and the promise it returns is dropped.
func TestLowerAsyncHandler(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, asyncHandlerSrc)
	listen := hostCallOf(t, must[*ir.ExprStmt](t, m.Main[0], "the server statement").Expr, ir.HostListen)
	create := hostCallOf(t, listen.Recv, ir.HostCreateServer)
	handler := must[*ir.FuncLit](t, create.Args[0], "the handler")
	if !handler.Async || !handler.Concurrent || handler.Result != ir.Void {
		t.Errorf("handler: async=%v concurrent=%v result=%v, want an async, concurrent, void literal",
			handler.Async, handler.Concurrent, handler.Result)
	}
	must[*ir.Await](t, must[*ir.ExprStmt](t, handler.Body[0], "the handler's first statement").Expr, "await sleep(10)")
}

// M2 check 4: changing shared state after an await is refused exactly once,
// naming the binding.
func TestSharedStateAfterAwaitRefused(t *testing.T) {
	t.Parallel()

	_, errs := lowerSource(t, sharedAfterAwaitSrc)
	if len(errs) != 1 || !strings.Contains(errs[0].Msg, "`hits`") || errs[0].Kind != Unsupported {
		t.Fatalf("errors = %q, want exactly one limit-of-the-phase refusal naming `hits`", joinMsgs(errs))
	}
}

// A promise settles, so it is shared state (M2 plan, D9): a handler may not
// await one made outside it, directly or through a function, but one it
// makes itself is its own.
func TestPromiseSharedWithHandlerRefused(t *testing.T) {
	t.Parallel()

	_, errs := lowerSource(t, `import { createServer } from "node:http";
`+sleepDecl+`
const ready: Promise<void> = sleep(10);
const port: number = 3000;
function waitReady(): Promise<void> {
  return ready;
}
createServer(async (req, res) => {
  await ready;
  res.end("a");
}).listen(port);
createServer(async (req, res) => {
  await waitReady();
  res.end("b");
}).listen(3001);
createServer(async (req, res) => {
  const mine: Promise<void> = sleep(1);
  await mine;
  res.end("c");
}).listen(3002);
`)
	msgs := joinMsgs(errs)
	if len(errs) != 2 {
		t.Fatalf("errors = %q, want two: the direct use and the call", msgs)
	}
	for _, want := range []string{
		"can't use `ready`, a promise created outside it",
		"can't call `waitReady()`, which uses `ready`, a promise created outside the handler",
	} {
		if !strings.Contains(msgs, want) {
			t.Errorf("errors = %q, want one containing %q", msgs, want)
		}
	}
}

// An async literal takes the slot's signature like any other: in a void slot
// its promise is dropped and so is anything it returns; in a promise slot its
// returns carry the element.
func TestAsyncLiteralSlots(t *testing.T) {
	t.Parallel()

	voidSlot := lowerClean(t, "function run(f: () => void): void {\n  f();\n}\nrun(async () => 5);\n")
	lit := argLit(t, voidSlot, 0)
	if !lit.Async || lit.Result != ir.Void {
		t.Errorf("in a void slot: async=%v result=%v, want async with a void result", lit.Async, lit.Result)
	}
	must[*ir.ExprStmt](t, lit.Body[0], "a value returned into a void slot")
	// A block body's `return v` in a void slot evaluates v and returns nothing.
	block := lowerClean(t, "function run(f: () => void): void {\n  f();\n}\nrun(async () => {\n  return 5;\n});\n")
	lit = argLit(t, block, 0)
	must[*ir.ExprStmt](t, lit.Body[0], "the returned value, evaluated")
	if ret := must[*ir.Return](t, lit.Body[1], "the return"); ret.Value != nil {
		t.Errorf("a return in a void slot carries %#v, want nothing", ret.Value)
	}

	promiseSlot := lowerClean(t, "function run(f: () => Promise<number>): void {\n  f();\n}\nrun(async () => 5);\n")
	lit = argLit(t, promiseSlot, 0)
	if !lit.Async || !lit.Result.Equal(ir.PromiseOf(ir.Number)) {
		t.Errorf("in a promise slot: async=%v result=%v, want async returning Promise<number>", lit.Async, lit.Result)
	}
	must[*ir.NumberLit](t, must[*ir.Return](t, lit.Body[0], "the concise body").Value, "the returned value")

	// A concise async body whose promise holds void has nothing to return.
	concise := lowerClean(t, "const g = async (): Promise<void> => console.log(\"x\");\n")
	lit = must[*ir.FuncLit](t, must[*ir.VarDecl](t, concise.Main[0], "const g").Init, "g")
	must[*ir.ConsoleLog](t, must[*ir.ExprStmt](t, lit.Body[0], "the concise body").Expr, "console.log")
}

// Without an annotation an async function's result is the promise the
// checker inferred.
func TestAsyncFunctionResultIsInferred(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, "async function f() {\n  return 1;\n}\n")
	if f := m.Funcs[0]; !f.Async || !f.Result.Equal(ir.PromiseOf(ir.Number)) {
		t.Errorf("f: async=%v result=%v, want async returning Promise<number>", f.Async, f.Result)
	}
}

func TestTimers(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `function f(): number {
  return 1;
}
function tick(): void {}
const id: number = setTimeout(() => f(), 5);
clearTimeout(id);
setTimeout(tick, 1);
`)
	set := hostCallOf(t, must[*ir.VarDecl](t, m.Main[0], "const id").Init, ir.HostSetTimeout)
	if set.Typ != ir.Number {
		t.Errorf("setTimeout has type %v, want number", set.Typ)
	}
	// The literal's value is dropped: a timer ignores its callback's result.
	cb := must[*ir.FuncLit](t, set.Args[0], "the callback")
	if cb.Result != ir.Void {
		t.Errorf("callback result = %v, want void", cb.Result)
	}
	must[*ir.ExprStmt](t, cb.Body[0], "the callback's dropped value")
	hostCallOf(t, must[*ir.ExprStmt](t, m.Main[1], "clearTimeout(id)").Expr, ir.HostClearTimeout)
	if named := hostCallOf(t, must[*ir.ExprStmt](t, m.Main[2], "setTimeout(tick, 1)").Expr, ir.HostSetTimeout); must[*ir.Ident](t, named.Args[0], "tick").Name != "tick" {
		t.Errorf("setTimeout(tick, 1) passes %+v, want tick", named.Args[0])
	}
}

// setTimeout is recognised by its declaration in lib.dom.d.ts: a program's
// own function of that name is an ordinary call.
func TestOwnSetTimeoutIsAnOrdinaryCall(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, "function setTimeout(f: () => void, ms: number): number {\n  f();\n  return ms;\n}\nsetTimeout(() => {}, 1);\n")
	if call := must[*ir.Call](t, must[*ir.ExprStmt](t, m.Main[0], "the call").Expr, "the call"); call.Callee != "setTimeout" {
		t.Errorf("callee = %q, want the program's setTimeout", call.Callee)
	}
}

// A top-level await makes the module's top-level code an async body (M2 step
// 7). `export {}` is what makes a script a module, so TypeScript allows it,
// and lowers to nothing.
func TestTopLevelAwait(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, sleepDecl+`export {};
console.log("before");
await sleep(10);
const n: number = 1;
console.log(n);
`)
	if !m.Async {
		t.Fatal("a module with a top-level await is not marked Async")
	}
	if len(m.Main) != 4 {
		t.Fatalf("main has %d statements, want 4 (export {} lowers to nothing): %#v", len(m.Main), m.Main)
	}
	if await := must[*ir.Await](t, must[*ir.ExprStmt](t, m.Main[1], "await sleep(10)").Expr, "await sleep(10)"); await.Typ != ir.Void {
		t.Errorf("await sleep(10) has type %v, want void", await.Typ)
	}
	if m.File == "" || !strings.HasSuffix(m.File, "t.ts") {
		t.Errorf("module File = %q, want the source path", m.File)
	}
}

// Awaits inside functions belong to the functions: a module whose only awaits
// are in async functions has ordinary top-level code.
func TestAwaitInsideFunctionsIsNotTopLevel(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, sleepSrc)
	if m.Async {
		t.Error("sleep.ts, whose only await is inside main(), was marked as having a top-level await")
	}
}

func TestAsyncRefusals(t *testing.T) {
	t.Parallel()

	const g = "async function g(): Promise<number> {\n  return 1;\n}\n"
	for _, tt := range []struct{ name, src, want string }{
		// Top-level await is supported since M2 step 7; an await in a plain
		// function is still not (TypeScript rejects it before lowering does).
		{"await in a plain function", g + "function f(): void {\n  const x: number = await g();\n}\n",
			"await is allowed only in an async function or at the top level of a module"},
		{"await on a non-promise", "async function f(): Promise<void> {\n  const x: number = await 5;\n}\n",
			"await needs a promise; awaiting a number is not supported yet"},
		{"returning a promise from async", g + "async function f(): Promise<number> {\n  return g();\n}\n",
			"an async function returning a promise is not supported yet"},
		{"reject", "const p: Promise<void> = new Promise((resolve, reject) => {});\n", "reject is not supported yet; it comes with errors, in M6"},
		{"resolve with a promise", g + "const p: Promise<number> = new Promise((resolve) => {\n  resolve(g());\n});\n",
			"resolving a promise with another promise is not supported yet"},
		{"executor not inline", "function ex(resolve: () => void): void {}\nconst p: Promise<void> = new Promise(ex);\n",
			"new Promise needs its executor written inline"},
		{"async executor", "const p: Promise<void> = new Promise(async (resolve) => {});\n", "an async executor is not supported"},
		{"another constructor", "new Date();\n", "new Date is not supported yet"},
		{"string handler", "setTimeout(\"console.log(1)\", 1);\n", "a string handler for setTimeout is not supported"},
		{"extra timer arguments", "setTimeout(() => {}, 1, 2);\n", "setTimeout's extra arguments are not supported yet"},
		{"no delay", "setTimeout(() => {});\n", "setTimeout without a delay is not supported yet"},
		{"callback taking an argument", "function f(x: number): void {}\nsetTimeout(f, 1);\n",
			"setTimeout's callback must take no arguments, and this one is (number) => void"},
		// A timer would call resolve with no value, resolving Promise<number> with undefined (D2).
		{"resolve of a valued promise as a timer", "const p: Promise<number> = new Promise((resolve) => {\n  setTimeout(resolve, 1);\n});\n",
			"setTimeout's callback must take no arguments, and this one is (number) => void"},
		{"delay that is not a number", "setTimeout(() => {}, i32(1));\n", "setTimeout's delay must be a number, not a i32"},
		{"clearTimeout arity", "clearTimeout();\n", "clearTimeout takes 1 argument, got 0"},
		{".then", g + "g().then((v) => {});\n", "promise methods such as .then are not supported yet; use await"},
		{"Promise statics", "const p: Promise<number> = Promise.resolve(1);\n", "Promise.resolve is not supported yet"},
		{"logging a promise", g + "console.log(g());\n", "console.log cannot print a promise yet"},
		// Every declaration of Promise must be in a bundled lib: a program that
		// augments it has added members with nothing behind them.
		{"augmented Promise", "interface Promise<T> {\n  extra(): void;\n}\n" + g,
			"function g returns a type the backend cannot represent yet"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, tt.src)
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
