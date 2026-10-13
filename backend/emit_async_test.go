package backend

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zobstory/cakebear/ir"
)

var (
	voidPromise = ir.PromiseOf(ir.Void)
	numPromise  = ir.PromiseOf(ir.Number)
	resolveVoid = ir.ResolveType(ir.Void)
)

func logStr(s string) ir.Stmt {
	return &ir.ExprStmt{Span: span, Expr: &ir.ConsoleLog{Arg: &ir.StringLit{Value: s, Span: span}, Span: span}}
}

func ident(name string, t ir.Type) ir.Expr { return &ir.Ident{Name: name, Typ: t, Span: span} }

// sleepFunc is targets-m2/sleep.ts's helper, as lower/ produces it:
//
//	function sleep(ms: number): Promise<void> {
//	  return new Promise((resolve) => { setTimeout(resolve, ms); });
//	}
func sleepFunc() *ir.Func {
	return &ir.Func{
		Name: "sleep", Params: []ir.Param{{Name: "ms", Type: ir.Number, Span: span}}, Result: voidPromise, Span: span,
		Body: []ir.Stmt{&ir.Return{Span: span, Value: &ir.NewPromise{Typ: voidPromise, Span: span, Executor: &ir.FuncLit{
			Params: []ir.Param{{Name: "resolve", Type: resolveVoid, Span: span}},
			Result: ir.Void, Typ: ir.ExecutorType(ir.Void), Span: span,
			Body: []ir.Stmt{&ir.ExprStmt{Span: span, Expr: &ir.HostCall{
				Op: ir.HostSetTimeout, Typ: ir.Number, Span: span,
				Args: []ir.Expr{ident("resolve", resolveVoid), ident("ms", ir.Number)},
			}}},
		}}}},
	}
}

// sleepModule is all of targets-m2/sleep.ts. Its main is an async function
// called main, which mangle keeps clear of Go's.
func sleepModule() *ir.Module {
	main := &ir.Func{Name: "main", Result: voidPromise, Async: true, Span: span, Body: []ir.Stmt{
		logStr("a"),
		&ir.ExprStmt{Span: span, Expr: &ir.Await{Typ: ir.Void, Span: span, Value: &ir.Call{
			Callee: "sleep", Typ: voidPromise, Span: span, Args: []ir.Expr{num(20)},
		}}},
		logStr("b"),
	}}
	return &ir.Module{Name: "sleep", Funcs: []*ir.Func{sleepFunc(), main}, Main: []ir.Stmt{
		&ir.ExprStmt{Span: span, Expr: &ir.Call{Callee: "main", Typ: voidPromise, Span: span}},
		logStr("c"),
	}}
}

// The shape M2's emitter writes (M2 plan, D8): the domain first everywhere,
// an async body run by cakebearrt.Async, await as cakebearrt.Await, and Run
// last so the main domain's reactions and timers run.
func TestEmitAsyncSleepShape(t *testing.T) {
	t.Parallel()

	got := Emit(sleepModule())
	mustParse(t, got)
	for _, want := range []string{
		"func sleep(ts_domain *cakebearrt.Domain, ms float64) *cakebearrt.Promise[struct{}] {\n",
		"\treturn cakebearrt.NewPromiseVoid(ts_domain, func(ts_domain *cakebearrt.Domain, resolve func(*cakebearrt.Domain)) {\n",
		"\t\tcakebearrt.SetTimeout(ts_domain, resolve, ms)\n",
		"func ts_main(ts_domain *cakebearrt.Domain) *cakebearrt.Promise[struct{}] {\n",
		"\treturn cakebearrt.Async(ts_domain, func(ts_co *cakebearrt.Co) struct{} {\n",
		"\t\tcakebearrt.Await(ts_co, sleep(ts_domain, 20.0))\n",
		"\t\treturn struct{}{}\n\t})\n}\n",
		"\tts_domain := cakebearrt.Main()\n",
		"\tts_main(ts_domain)\n",
		"\t" + rtPkg + ".Run()\n}\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted source missing %q:\n%s", want, got)
		}
	}
}

// M2 check 1, at the backend: sleep.ts built and run prints what Node prints.
func TestBuildAsyncSleepMatchesNode(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "sleep")
	if src, err := Build(sleepModule(), Options{Output: out}); err != nil {
		t.Fatalf("Build: %v\n%s", err, src)
	}
	start := time.Now()
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("running the binary: %v", err)
	}
	if string(got) != "a\nc\nb\n" {
		t.Errorf("printed %q, want %q, as Node prints", got, "a\nc\nb\n")
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("ran for %v; the 20 ms sleep cannot have happened", elapsed)
	}
}

// A program whose only pending work is an await still runs it: the code after
// the await is a microtask, and only Run drains them. No timer is involved,
// so nothing else would make the emitter call Run.
func TestBuildAwaitWithoutATimerRuns(t *testing.T) {
	t.Parallel()

	one := &ir.Func{Name: "one", Result: numPromise, Async: true, Span: span, Body: []ir.Stmt{&ir.Return{Span: span, Value: num(1)}}}
	show := &ir.Func{Name: "show", Result: voidPromise, Async: true, Span: span, Body: []ir.Stmt{
		logStr("before"),
		logNum(&ir.Await{Typ: ir.Number, Span: span, Value: &ir.Call{Callee: "one", Typ: numPromise, Span: span}}),
	}}
	m := &ir.Module{Name: "t", Funcs: []*ir.Func{one, show}, Main: []ir.Stmt{
		&ir.ExprStmt{Span: span, Expr: &ir.Call{Callee: "show", Typ: voidPromise, Span: span}},
		logStr("sync done"),
	}}
	out := filepath.Join(t.TempDir(), "await")
	if src, err := Build(m, Options{Output: out}); err != nil {
		t.Fatalf("Build: %v\n%s", err, src)
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("running the binary: %v", err)
	}
	if want := "before\nsync done\n1\n"; string(got) != want {
		t.Errorf("printed %q, want %q, as Node prints", got, want)
	}
}

// An async function whose promise holds a value returns it from the Async
// body, with no trailing struct{}{}; one that resolves a number uses
// NewPromise and a resolve that takes the value.
func TestEmitAsyncValues(t *testing.T) {
	t.Parallel()

	g := &ir.Func{Name: "g", Result: numPromise, Async: true, Span: span, Body: []ir.Stmt{
		&ir.Return{Span: span, Value: &ir.Binary{
			Op: ir.OpAdd, Typ: ir.Number, Span: span,
			Left:  &ir.Await{Typ: ir.Number, Span: span, Value: &ir.Call{Callee: "h", Typ: numPromise, Span: span}},
			Right: num(1),
		}},
	}}
	resolveNum := ir.ResolveType(ir.Number)
	h := &ir.Func{Name: "h", Result: numPromise, Span: span, Body: []ir.Stmt{
		&ir.Return{Span: span, Value: &ir.NewPromise{Typ: numPromise, Span: span, Executor: &ir.FuncLit{
			Params: []ir.Param{{Name: "resolve", Type: resolveNum, Span: span}},
			Result: ir.Void, Typ: ir.ExecutorType(ir.Number), Span: span,
			Body: []ir.Stmt{&ir.ExprStmt{Span: span, Expr: &ir.Call{
				Callee: "resolve", Typ: ir.Void, Span: span, Args: []ir.Expr{num(41)},
			}}},
		}}},
	}}
	got := Emit(&ir.Module{Name: "t", Funcs: []*ir.Func{g, h}})
	mustParse(t, got)
	for _, want := range []string{
		"\treturn cakebearrt.Async(ts_domain, func(ts_co *cakebearrt.Co) float64 {\n",
		"\t\treturn (cakebearrt.Await(ts_co, h(ts_domain)) + 1.0)\n\t})\n}\n",
		"\treturn cakebearrt.NewPromise(ts_domain, func(ts_domain *cakebearrt.Domain, resolve func(*cakebearrt.Domain, float64)) {\n",
		"\t\tresolve(ts_domain, 41.0)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted source missing %q:\n%s", want, got)
		}
	}
}

// An async literal standing in for a void callback, such as a request
// handler, starts its body and drops the promise: no return before the call,
// and a bare return in the body returns struct{}{}.
func TestEmitAsyncLiteralInAVoidSlot(t *testing.T) {
	t.Parallel()

	handler := &ir.FuncLit{
		Async: true, Result: ir.Void, Typ: ir.FuncType(nil, ir.Void), Span: span,
		Body: []ir.Stmt{&ir.If{
			Cond: &ir.BoolLit{Value: true, Span: span}, Span: span,
			Then: []ir.Stmt{&ir.Return{Span: span}},
		}},
	}
	got := Emit(&ir.Module{Name: "t", Main: []ir.Stmt{&ir.VarDecl{
		Name: "f", Type: handler.Typ, Init: handler, Span: span,
	}}})
	mustParse(t, got)
	for _, want := range []string{
		"\tf = func(ts_domain *cakebearrt.Domain) {\n\t\tcakebearrt.Async(ts_domain, func(ts_co *cakebearrt.Co) struct{} {\n",
		"\t\t\t\treturn struct{}{}\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted source missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "return cakebearrt.Async") {
		t.Errorf("a literal in a void slot returned its promise:\n%s", got)
	}
}

// A plain function literal nested in an async body returns as plain Go: the
// async body's struct{}{} returns stop at the literal.
func TestEmitPlainLiteralInsideAsyncBody(t *testing.T) {
	t.Parallel()

	inner := &ir.FuncLit{Result: ir.Void, Typ: ir.FuncType(nil, ir.Void), Span: span, Body: []ir.Stmt{&ir.Return{Span: span}}}
	f := &ir.Func{Name: "f", Result: voidPromise, Async: true, Span: span, Body: []ir.Stmt{
		&ir.VarDecl{Name: "inner", Type: inner.Typ, Init: inner, Span: span},
	}}
	got := Emit(&ir.Module{Name: "t", Funcs: []*ir.Func{f}})
	mustParse(t, got)
	if !strings.Contains(got, "\t\tinner = func(ts_domain *cakebearrt.Domain) {\n\t\t\treturn\n\t\t}\n") {
		t.Errorf("the nested literal's return should be bare:\n%s", got)
	}
}

func TestEmitClearTimeout(t *testing.T) {
	t.Parallel()

	got := Emit(&ir.Module{Name: "t", Main: []ir.Stmt{&ir.ExprStmt{Span: span, Expr: &ir.HostCall{
		Op: ir.HostClearTimeout, Typ: ir.Void, Span: span, Args: []ir.Expr{num(3)},
	}}}})
	mustParse(t, got)
	for _, want := range []string{"\tcakebearrt.ClearTimeout(ts_domain, 3.0)\n", "\t" + rtPkg + ".Run()\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted source missing %q:\n%s", want, got)
		}
	}
}

// main declares a domain only when its own code uses one: a function literal
// has its own ts_domain parameter, so the calls inside it don't count. Go
// rejects an unused local, and parsing alone would not notice; a server whose
// only calls were in its handler failed to build this way.
func TestMainDeclaresADomainOnlyForItsOwnUse(t *testing.T) {
	t.Parallel()

	handler := &ir.FuncLit{Result: ir.Void, Typ: ir.FuncType(nil, ir.Void), Span: span, Body: []ir.Stmt{
		&ir.ExprStmt{Span: span, Expr: &ir.Call{Callee: "g", Typ: ir.Void, Span: span}},
	}}
	only := Emit(&ir.Module{Name: "t", Main: []ir.Stmt{&ir.VarDecl{Name: "f", Type: handler.Typ, Init: handler, Span: span}}})
	if strings.Contains(only, domainVar+" := ") {
		t.Errorf("main declared a domain only its literal uses:\n%s", only)
	}
	direct := Emit(&ir.Module{Name: "t", Main: []ir.Stmt{&ir.ExprStmt{Span: span, Expr: &ir.Call{Callee: "g", Typ: ir.Void, Span: span}}}})
	if !strings.Contains(direct, "\t"+domainVar+" := "+rtPkg+".Main()\n") {
		t.Errorf("main calls g but declares no domain:\n%s", direct)
	}
}

// ts_domain and ts_co are free for the emitter: no user identifier mangles
// onto either.
func TestMangleLeavesTheEmittersNamesFree(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"domain", "co", "ts_domain", "ts_co", "Domain", "cakebearrt"} {
		if got := mangle(name); got == domainVar || got == coVar {
			t.Errorf("mangle(%q) = %q, which the emitter uses itself", name, got)
		}
	}
}
