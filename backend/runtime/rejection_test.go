package runtime

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

// Rejection through async functions, await, executors and spawn (M6 plan
// D7, step 4), held to what Node 24.18 did with the programs in
// testdata/async/ (testdata/gen-async-goldens.mjs). Each program here is the
// case's, written as the emitter writes it: a module with a top-level await
// is an async body that RunModule runs, any other ends in Run, and one with
// no await or timer in neither, as the generated main does.

func loadAsync(t *testing.T) map[string]uncaughtCase {
	t.Helper()
	data, err := os.ReadFile("testdata/async-goldens.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases map[string]uncaughtCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return file.Cases
}

// runAsMain runs program as the generated main does, under Exit, and
// returns how it ended. program ends in Run or RunModule itself, as the
// generated main does, or in neither.
func runAsMain(t *testing.T, program func(d *Domain)) (stdout, stderr string, exitCode int) {
	t.Helper()
	out, errOut, exits := isolate(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer Exit()
		program(Main())
	}()
	waitFor(t, done, "the program to end")
	select {
	case exitCode = <-exits:
	default:
	}
	return out.String(), errOut.String(), exitCode
}

// matchAsync holds program to Node's run of the case named, 20 runs: stdout
// byte for byte, the exit code, and the report, as cakebearsReport makes it
// from Node's with excerpt for a case Node has none for.
func matchAsync(t *testing.T, name string, program func(*Domain), excerpt string) {
	t.Helper()
	c, ok := loadAsync(t)[name]
	if !ok {
		t.Fatalf("no golden for %s", name)
	}
	want := ""
	if c.Rule != "ordering" {
		want = cakebearsReport(t, c, excerpt)
	}
	for run := range 20 { //nolint:paralleltest // isolate swaps process-wide runtime state
		t.Run(strconv.Itoa(run), func(t *testing.T) {
			stdout, stderr, code := runAsMain(t, program)
			if stdout != c.Limit0.Stdout || code != c.Limit0.Exit {
				t.Fatalf("printed\n%s(exit %d)\nwant, as Node\n%s(exit %d)", stdout, code, c.Limit0.Stdout, c.Limit0.Exit)
			}
			if stderr != want {
				t.Fatalf("reported\n%s\nwant\n%s", stderr, want)
			}
		})
	}
}

// tick is `new Promise((resolve) => resolve())`.
func tick(d *Domain) *Promise[struct{}] {
	return NewPromiseVoid(d, func(d *Domain, resolve func(*Domain), _ func(*Domain, any)) { resolve(d) })
}

// orderMixed is async/order-mixed.ts.
func orderMixed(d *Domain) {
	ok := func(d *Domain, v string) *Promise[string] { return Async(d, func(*Co) string { return v }) }
	bad := func(d *Domain, v string) *Promise[string] {
		return Async(d, func(*Co) string { panic(&Thrown{V: v}) })
	}
	relay := func(d *Domain, p *Promise[string]) *Promise[string] {
		return Async(d, func(co *Co) string { return Await(co, p) })
	}
	relayCatch := func(d *Domain, p *Promise[string]) *Promise[string] {
		return Async(d, func(co *Co) string {
			var ts_r string
			ts_k1, ts_t1 := TryK(func() int {
				ts_r = Await(co, p)
				return 1
			})
			if ts_t1 != nil {
				e := ts_t1.V
				return "recovered " + UnknownToString(e)
			}
			if ts_k1 == 1 {
				return ts_r
			}
			panic("cakebear: reached the end of a function TypeScript proved cannot end")
		})
	}
	w := func(d *Domain, name string, p *Promise[string]) *Promise[struct{}] {
		return Async(d, func(co *Co) struct{} {
			if ts_t1 := Try(func() {
				LogString(name + " " + Await(co, p))
			}); ts_t1 != nil {
				e := ts_t1.V
				LogString(name + " caught " + UnknownToString(e))
			}
			return struct{}{}
		})
	}
	early := func(d *Domain) *Promise[string] {
		return Async(d, func(*Co) string {
			LogString("early body")
			panic(&Thrown{V: "early"})
		})
	}
	w(d, "a", ok(d, "1"))
	w(d, "b", bad(d, "2"))
	w(d, "c", relay(d, bad(d, "3")))
	w(d, "d", relay(d, ok(d, "4")))
	w(d, "e", relayCatch(d, bad(d, "5")))
	w(d, "f", relay(d, relay(d, bad(d, "6"))))
	w(d, "g", early(d))
	LogString("caller continues")
	SetTimeout(d, func(*Domain) { LogString("timer") }, 0)
	p1 := NewPromise(d, func(d *Domain, resolve func(*Domain, string), reject func(*Domain, any)) {
		reject(d, "r1")
		resolve(d, "1")
	})
	p2 := NewPromise(d, func(*Domain, func(*Domain, string), func(*Domain, any)) {
		panic(&Thrown{V: "t2"})
	})
	p3 := NewPromise(d, func(d *Domain, resolve func(*Domain, string), _ func(*Domain, any)) {
		resolve(d, "3")
		panic(&Thrown{V: "ignored"})
	})
	w(d, "p1", p1)
	w(d, "p2", p2)
	w(d, "p3", p3)
	LogString("sync done")
	Run()
}

func TestRejectionOrderMatchesNode(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	matchAsync(t, "order-mixed", orderMixed, "")
}

// orderAwait is async/order-await.ts, m6-node's c3-02.
func orderAwait(d *Domain) {
	rejectNow := func(d *Domain, message string) *Promise[float64] {
		return NewPromise(d, func(d *Domain, _ func(*Domain, float64), reject func(*Domain, any)) {
			reject(d, NewError(ErrorBase, message, nil))
		})
	}
	resolveAfter := func(d *Domain, ms, value float64) *Promise[float64] {
		return NewPromise(d, func(d *Domain, resolve func(*Domain, float64), _ func(*Domain, any)) {
			SetTimeout(d, func(d *Domain) { resolve(d, value) }, ms)
		})
	}
	rejectAfter := func(d *Domain, ms float64, message string) *Promise[float64] {
		return NewPromise(d, func(d *Domain, _ func(*Domain, float64), reject func(*Domain, any)) {
			SetTimeout(d, func(d *Domain) { reject(d, NewError(ErrorBase, message, nil)) }, ms)
		})
	}
	ok := func(d *Domain, name string) *Promise[float64] {
		return Async(d, func(co *Co) float64 {
			Log(name, "start")
			Await(co, tick(d))
			Log(name, "after await 1")
			Await(co, tick(d))
			Log(name, "after await 2")
			return 1
		})
	}
	bad := func(d *Domain, name string) *Promise[float64] {
		return Async(d, func(co *Co) float64 {
			Log(name, "start")
			Await(co, tick(d))
			Log(name, "after await 1")
			panic(&Thrown{V: NewError(ErrorBase, name, nil)})
		})
	}
	badSync := func(d *Domain, name string) *Promise[float64] {
		return Async(d, func(*Co) float64 {
			Log(name, "start")
			panic(&Thrown{V: NewError(ErrorBase, name, nil)})
		})
	}
	run := func(d *Domain, name string, p *Promise[float64]) *Promise[struct{}] {
		return Async(d, func(co *Co) struct{} {
			ts_t1 := Try(func() {
				if ts_t2 := Try(func() {
					v := Await(co, p)
					Log(name, "resolved", v)
				}); ts_t2 != nil {
					e := ts_t2.V
					Log(name, "rejected", Unbox[*Error](e, "e").Message())
					Await(co, tick(d))
					Log(name, "after an await in catch")
				}
			})
			Log(name, "finally")
			Rethrow(ts_t1)
			Log(name, "after try")
			return struct{}{}
		})
	}
	ts_module := Async(d, func(co *Co) struct{} {
		LogString("sync start")
		SetTimeout(d, func(*Domain) { LogString("timeout 0") }, 0)
		runs := NewArray(
			run(d, "A", ok(d, "a")),
			run(d, "B", bad(d, "b")),
			run(d, "C", badSync(d, "c")),
			run(d, "D", rejectNow(d, "d")),
			run(d, "E", resolveAfter(d, 5, 5)),
			run(d, "F", rejectAfter(d, 5, "f")),
			run(d, "G", rejectAfter(d, 0, "g")),
		)
		LogString("sync end")
		for i := 0; i < runs.Len(); i++ {
			Await(co, runs.Get(float64(i)))
		}
		LogString("all done")
		return struct{}{}
	})
	RunModule(ts_module, "/CASES/order-await.ts")
}

func TestRejectionHopsMatchNode(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	matchAsync(t, "order-await", orderAwait, "")
}

// settleOnce is async/settle-once.ts, m6-node's c3-04 and c3-05.
func settleOnce(d *Domain) {
	message := func(e any) string { return Unbox[*Error](e, "e").Message() }
	ts_module := Async(d, func(co *Co) struct{} {
		p := NewPromise(d, func(d *Domain, resolve func(*Domain, float64), reject func(*Domain, any)) {
			resolve(d, 1)
			reject(d, NewError(ErrorBase, "ignored", nil))
			resolve(d, 2)
		})
		Log("resolve, reject, resolve:", Await(co, p))
		q := NewPromise(d, func(d *Domain, resolve func(*Domain, float64), reject func(*Domain, any)) {
			reject(d, NewError(ErrorBase, "first", nil))
			resolve(d, 3)
			reject(d, NewError(ErrorBase, "second", nil))
		})
		if ts_t1 := Try(func() { Await(co, q) }); ts_t1 != nil {
			Log("reject, resolve, reject:", message(ts_t1.V))
		}
		r := NewPromise(d, func(d *Domain, resolve func(*Domain, float64), reject func(*Domain, any)) {
			SetTimeout(d, func(d *Domain) { resolve(d, 4) }, 1)
			SetTimeout(d, func(d *Domain) { reject(d, NewError(ErrorBase, "too late", nil)) }, 5)
		})
		Log("timer resolve, then timer reject:", Await(co, r))
		Await(co, sleepFn(d, 10))
		LogString("before new Promise")
		t := NewPromise(d, func(*Domain, func(*Domain, float64), func(*Domain, any)) {
			LogString("executor runs synchronously")
			panic(&Thrown{V: NewError(ErrorBase, "executor threw", nil)})
		})
		LogString("after new Promise")
		if ts_t2 := Try(func() { Await(co, t) }); ts_t2 != nil {
			Log("caught:", message(ts_t2.V))
		}
		u := NewPromise(d, func(d *Domain, resolve func(*Domain, float64), _ func(*Domain, any)) {
			resolve(d, 1)
			panic(&Thrown{V: NewError(ErrorBase, "thrown after resolve", nil)})
		})
		Log("resolve then throw:", Await(co, u))
		v := NewPromise(d, func(d *Domain, _ func(*Domain, float64), reject func(*Domain, any)) {
			reject(d, NewError(ErrorBase, "rejected first", nil))
			panic(&Thrown{V: NewError(ErrorBase, "thrown after reject", nil)})
		})
		if ts_t3 := Try(func() { Await(co, v) }); ts_t3 != nil {
			Log("reject then throw:", message(ts_t3.V))
		}
		s := NewPromise(d, func(*Domain, func(*Domain, float64), func(*Domain, any)) {
			panic(&Thrown{V: "a string"})
		})
		if ts_t4 := Try(func() { Await(co, s) }); ts_t4 != nil {
			e := ts_t4.V
			Log("executor threw a string:", TypeOf(e).String(), e)
		}
		LogString("done")
		return struct{}{}
	})
	RunModule(ts_module, "/CASES/settle-once.ts")
}

func TestPromisesSettleOnceAsNode(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	matchAsync(t, "settle-once", settleOnce, "")
}

// rejectedValues is async/rejected-values.ts.
func rejectedValues(d *Domain) {
	box := newTestObject([]string{"a"}, []any{1.0})
	list := NewArray[any](1.0, "two", Null)
	err := NewError(ErrorType, "m", nil)
	rejectNow := func(d *Domain, reason any) *Promise[float64] {
		return NewPromise(d, func(d *Domain, _ func(*Domain, float64), reject func(*Domain, any)) { reject(d, reason) })
	}
	throwIt := func(d *Domain, reason any) *Promise[float64] {
		return Async(d, func(*Co) float64 { panic(&Thrown{V: reason}) })
	}
	reasons := NewArray[any](err, "m", 5.0, NegZero, true, box, list, nil, Null)
	ts_module := Async(d, func(co *Co) struct{} {
		for i := 0; i < reasons.Len(); i++ {
			reason := reasons.Get(float64(i))
			if ts_t1 := Try(func() { Await(co, rejectNow(d, reason)) }); ts_t1 != nil {
				e := ts_t1.V
				Log("rejected", TypeOf(e).String(), e == reason, e)
			}
			if ts_t2 := Try(func() { Await(co, throwIt(d, reason)) }); ts_t2 != nil {
				e := ts_t2.V
				Log("thrown", TypeOf(e).String(), e == reason, e)
			}
		}
		late := NewPromise(d, func(d *Domain, _ func(*Domain, float64), reject func(*Domain, any)) {
			reject(d, NewError(ErrorBase, "handled a few microtasks late", nil))
		})
		Await(co, tick(d))
		Await(co, tick(d))
		Await(co, tick(d))
		if ts_t3 := Try(func() { Await(co, late) }); ts_t3 != nil {
			Log("caught:", Unbox[*Error](ts_t3.V, "e").Message())
		}
		LogString("done")
		return struct{}{}
	})
	RunModule(ts_module, "/CASES/rejected-values.ts")
}

func TestCatchGetsTheRejectedValue(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	matchAsync(t, "rejected-values", rejectedValues, "")
}

// spawnRejects is async/spawn-rejects.ts.
func spawnRejects(d *Domain) {
	ts_module := Async(d, func(co *Co) struct{} {
		if ts_t1 := Try(func() {
			n := Await(co, Spawn(d, func(*Domain) float64 {
				panic(&Thrown{V: NewError(ErrorRange, "in a spawn", nil)})
			}))
			Log("never", n)
		}); ts_t1 != nil {
			Log("spawn", UnknownToString(ts_t1.V))
		}
		if ts_t2 := Try(func() {
			Await(co, SpawnAsync(d, func(s *Domain) *Promise[float64] {
				return Async(s, func(co *Co) float64 {
					Await(co, sleepFn(s, 1))
					panic(&Thrown{V: NewError(ErrorBase, "in an async spawn", nil)})
				})
			}))
		}); ts_t2 != nil {
			Log("async spawn", UnknownToString(ts_t2.V))
		}
		if ts_t3 := Try(func() {
			Await(co, SpawnAsync(d, func(s *Domain) *Promise[float64] {
				return Async(s, func(co *Co) float64 {
					Await(co, sleepFn(s, 1))
					panic(&Thrown{V: newTestObject([]string{"code", "list"}, []any{7.0, NewArray(1.0, 2.0)})})
				})
			}))
		}); ts_t3 != nil {
			Log("an object", ts_t3.V)
		}
		if ts_t4 := Try(func() {
			Await(co, SpawnAsync(d, func(s *Domain) *Promise[float64] {
				return NewPromise(s, func(s *Domain, _ func(*Domain, float64), reject func(*Domain, any)) {
					SetTimeout(s, func(s *Domain) { reject(s, "later") }, 1)
				})
			}))
		}); ts_t4 != nil {
			Log("a rejection", ts_t4.V)
		}
		LogString("end")
		return struct{}{}
	})
	RunModule(ts_module, "/CASES/spawn-rejects.ts")
}

func TestSpawnRejectionsReachTheCaller(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	matchAsync(t, "spawn-rejects", spawnRejects, "")
}
