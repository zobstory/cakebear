package runtime

import (
	"math"
	"testing"
)

// Rejections nothing handles, and a top-level await's (M6 plan D6, D7; R9
// to R14): the program ends with exit code 1 when Node's does, after the
// output Node printed, and reports as Node does, but for the excerpt where
// Node's points into its own code. The programs are testdata/async/'s.

// chainOf logs name and 1 to n, an await of a settled promise apart: a
// microtask chain the drain runs to its end.
func chainOf(d *Domain, name string, n float64, what ...any) *Promise[struct{}] {
	return Async(d, func(co *Co) struct{} {
		for i := 1.0; i <= n; i++ {
			Await(co, tick(d))
			Log(append([]any{name}, append(what, i)...)...)
		}
		return struct{}{}
	})
}

// thrownError is `throw new K(message)` on a line, made at made and thrown
// at thrown, its columns.
func thrownError(file string, line, thrown, made int, text string, kind ErrorKind, message string) *Thrown {
	return &Thrown{V: NewError(kind, message, site(file, line, made, text)), Site: site(file, line, thrown, text)}
}

// unhandledDrain is async/unhandled-drain.ts.
func unhandledDrain(d *Domain) {
	fail := func(d *Domain) *Promise[struct{}] {
		return Async(d, func(*Co) struct{} {
			LogString("fail start")
			panic(thrownError("unhandled-drain.ts", 17, 3, 9, `  throw new Error("from async");`, ErrorBase, "from async"))
		})
	}
	SetTimeout(d, func(*Domain) { LogString("timer, never") }, 0)
	LogString("before call")
	fail(d)
	LogString("after call")
	chainOf(d, "then", 4)
	LogString("sync end")
	Run()
}

// unhandledAfterAwait is async/unhandled-after-await.ts.
func unhandledAfterAwait(d *Domain) {
	g := func(d *Domain) *Promise[struct{}] {
		return Async(d, func(co *Co) struct{} {
			LogString("g start")
			Await(co, tick(d))
			LogString("g resumed")
			panic(thrownError("unhandled-after-await.ts", 11, 3, 9, `  throw new Error("after an await");`, ErrorBase, "after an await"))
		})
	}
	g(d)
	LogString("after call")
	chainOf(d, "then", 4)
	SetTimeout(d, func(*Domain) { LogString("timeout 0") }, 0)
	Run()
}

// unhandledInTimer is async/unhandled-in-timer.ts.
func unhandledInTimer(d *Domain) {
	fails := func(d *Domain) *Promise[struct{}] {
		return Async(d, func(*Co) struct{} {
			panic(thrownError("unhandled-in-timer.ts", 10, 3, 9, `  throw new Error("rejected in timer 1");`, ErrorBase, "rejected in timer 1"))
		})
	}
	after := func(d *Domain) *Promise[struct{}] {
		return Async(d, func(co *Co) struct{} {
			Await(co, tick(d))
			LogString("microtask after the rejection")
			return struct{}{}
		})
	}
	SetTimeout(d, func(d *Domain) {
		LogString("timer 1")
		fails(d)
		after(d)
	}, 0)
	SetTimeout(d, func(*Domain) { LogString("timer 2, same delay") }, 0)
	SetTimeout(d, func(*Domain) { LogString("timer 3, later") }, 20)
	Run()
}

// unhandledAfterTimer is async/unhandled-after-timer.ts.
func unhandledAfterTimer(d *Domain) {
	const text = `const p = new Promise        ((_resolve, reject) => reject(new Error("handled after a timer")));`
	ts_module := Async(d, func(co *Co) struct{} {
		p := NewPromise(d, func(d *Domain, _ func(*Domain, float64), reject func(*Domain, any)) {
			reject(d, NewError(ErrorBase, "handled after a timer", site("unhandled-after-timer.ts", 4, 60, text)))
		})
		LogString("waiting for a timer before awaiting p")
		Await(co, sleepFn(d, 0))
		if ts_t1 := Try(func() { Await(co, p) }); ts_t1 != nil {
			Log("caught:", Unbox[*Error](ts_t1.V, "e").Message())
		}
		LogString("done")
		return struct{}{}
	})
	RunModule(ts_module, "/CASES/unhandled-after-timer.ts")
}

// unhandledSlowFirst is async/unhandled-slow-first.ts.
func unhandledSlowFirst(d *Domain) {
	const text = `  return new Promise((_resolve, reject) => setTimeout(() => reject(new Error(message)), ms));`
	rejectAfter := func(d *Domain, ms float64, message string) *Promise[float64] {
		return NewPromise(d, func(d *Domain, _ func(*Domain, float64), reject func(*Domain, any)) {
			SetTimeout(d, func(d *Domain) { reject(d, NewError(ErrorBase, message, site("unhandled-slow-first.ts", 6, 68, text))) }, ms)
		})
	}
	resolveAfter := func(d *Domain, ms, value float64) *Promise[float64] {
		return NewPromise(d, func(d *Domain, resolve func(*Domain, float64), _ func(*Domain, any)) {
			SetTimeout(d, func(d *Domain) { resolve(d, value) }, ms)
		})
	}
	main := func(d *Domain) *Promise[struct{}] {
		return Async(d, func(co *Co) struct{} {
			fast := rejectAfter(d, 1, "fast one failed")
			slow := resolveAfter(d, 20, 2)
			if ts_t1 := Try(func() {
				Log("slow:", Await(co, slow))
				Log("fast:", Await(co, fast))
			}); ts_t1 != nil {
				Log("caught:", Unbox[*Error](ts_t1.V, "e").Message())
			}
			return struct{}{}
		})
	}
	ts_module := Async(d, func(co *Co) struct{} {
		Await(co, main(d))
		LogString("done")
		return struct{}{}
	})
	RunModule(ts_module, "/CASES/unhandled-slow-first.ts")
}

// unhandledNoLoop is async/unhandled-no-loop.ts. With no await and no
// timer, the generated main calls no Run.
func unhandledNoLoop(d *Domain) {
	f := func(d *Domain) *Promise[struct{}] {
		return Async(d, func(*Co) struct{} {
			panic(thrownError("unhandled-no-loop.ts", 6, 3, 9, `  throw new RangeError("nothing awaits this");`, ErrorRange, "nothing awaits this"))
		})
	}
	f(d)
	LogString("after the call")
}

func TestUnhandledRejectionsEndMainAsNode(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	for name, program := range map[string]func(*Domain){
		"unhandled-drain":       unhandledDrain,
		"unhandled-after-await": unhandledAfterAwait,
		"unhandled-in-timer":    unhandledInTimer,
		"unhandled-after-timer": unhandledAfterTimer,
		"unhandled-slow-first":  unhandledSlowFirst,
		"unhandled-no-loop":     unhandledNoLoop,
	} { //nolint:paralleltest // isolate swaps process-wide runtime state
		t.Run(name, func(t *testing.T) { matchAsync(t, name, program, "") })
	}
}

// unhandledValue is async/unhandled-value.ts with VARIANT set to one that
// rejects with reason, or throws it from an async function.
func unhandledValue(reason any, thrown bool) func(*Domain) {
	return func(d *Domain) {
		rejectWith := func(d *Domain, reason any) *Promise[float64] {
			return NewPromise(d, func(d *Domain, _ func(*Domain, float64), reject func(*Domain, any)) { reject(d, reason) })
		}
		throwIt := func(d *Domain, reason any) *Promise[float64] {
			return Async(d, func(*Co) float64 {
				panic(&Thrown{V: reason, Site: site("unhandled-value.ts", 11, 3, "  throw reason;")})
			})
		}
		LogString("sync")
		if thrown {
			throwIt(d, reason)
		} else {
			rejectWith(d, reason)
		}
	}
}

// TestUnhandledValuesReportAsNode: R13's report, the reason named as Node
// names it, with an excerpt only where a throw rejected the promise.
func TestUnhandledValuesReportAsNode(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	for variant, reason := range map[string]any{
		"string":    "m",
		"empty":     "",
		"number":    5.0,
		"negzero":   NegZero,
		"nan":       math.NaN(),
		"boolean":   false,
		"undefined": nil,
		"null":      Null,
		"object":    newTestObject([]string{"a", "b"}, []any{1.0, NewArray(2.0)}),
		"array":     NewArray[any](1.0, "two"),
		"multiline": "a \"quoted\"\nsecond line",
		"surrogate": "\xed\xa0\x80 lone", // "\uD800 lone", as the scanner stores it
	} { //nolint:paralleltest // isolate swaps process-wide runtime state
		t.Run(variant, func(t *testing.T) { matchAsync(t, "unhandled-value/"+variant, unhandledValue(reason, false), "") })
	}
	t.Run("thrown", func(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
		excerpt := "file:///CASES/unhandled-value.ts:11\n  throw reason;\n  ^\n"
		matchAsync(t, "unhandled-value/thrown", unhandledValue("thrown from an async function", true), excerpt)
	})
}

// hopsOf is async/'s hops(name): 8 awaits, logging after each.
func hopsOf(d *Domain, name string) *Promise[struct{}] { return chainOf(d, name, 8, "hop") }

// tlaThrow is async/tla-throw.ts.
func tlaThrow(d *Domain) {
	ts_module := Async(d, func(co *Co) struct{} {
		LogString("before")
		Await(co, tick(d))
		hopsOf(d, "a")
		SetTimeout(d, func(*Domain) { LogString("timer, never") }, 0)
		LogString("throwing")
		panic(thrownError("tla-throw.ts", 20, 1, 7, `throw new Error("after a top-level await");`, ErrorBase, "after a top-level await"))
	})
	RunModule(ts_module, "/CASES/tla-throw.ts")
}

// tlaSyncThrow is async/tla-sync-throw.ts.
func tlaSyncThrow(d *Domain) {
	ts_module := Async(d, func(co *Co) struct{} {
		n := 1.0
		hopsOf(d, "a")
		LogString("throwing before any await")
		if n > 0 {
			panic(thrownError("tla-sync-throw.ts", 16, 12, 18, `if (n > 0) throw new RangeError("before the first await");`, ErrorRange, "before the first await"))
		}
		Await(co, tick(d))
		return struct{}{}
	})
	RunModule(ts_module, "/CASES/tla-sync-throw.ts")
}

// tlaRejected is async/tla-rejected.ts.
func tlaRejected(d *Domain) {
	ts_module := Async(d, func(co *Co) struct{} {
		hopsOf(d, "a")
		p := NewPromiseVoid(d, func(d *Domain, _ func(*Domain), reject func(*Domain, any)) { reject(d, "a string reason") })
		LogString("awaiting a rejected promise")
		Await(co, p)
		LogString("never")
		return struct{}{}
	})
	RunModule(ts_module, "/CASES/tla-rejected.ts")
}

// tlaRejectedError is async/tla-rejected-error.ts.
func tlaRejectedError(d *Domain) {
	const text = `await new Promise      ((_resolve, reject) => reject(new Error("awaited rejection")));`
	ts_module := Async(d, func(co *Co) struct{} {
		LogString("before await")
		SetTimeout(d, func(*Domain) { LogString("timer") }, 0)
		Await(co, NewPromiseVoid(d, func(d *Domain, _ func(*Domain), reject func(*Domain, any)) {
			reject(d, NewError(ErrorBase, "awaited rejection", site("tla-rejected-error.ts", 7, 54, text)))
		}))
		LogString("not reached")
		return struct{}{}
	})
	RunModule(ts_module, "/CASES/tla-rejected-error.ts")
}

// TestTopLevelAwaitRejectionsAreUncaught: a module's rejection is an
// uncaught throw, not R13's report (R14), five microtasks after it, as
// Node's loader reports it.
func TestTopLevelAwaitRejectionsAreUncaught(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	for name, program := range map[string]func(*Domain){
		"tla-throw":          tlaThrow,
		"tla-sync-throw":     tlaSyncThrow,
		"tla-rejected":       tlaRejected,
		"tla-rejected-error": tlaRejectedError,
	} { //nolint:paralleltest // isolate swaps process-wide runtime state
		t.Run(name, func(t *testing.T) { matchAsync(t, name, program, "") })
	}
}

// spawnUncaught is async/spawn-uncaught.ts.
func spawnUncaught(d *Domain) {
	ts_module := Async(d, func(co *Co) struct{} {
		if ts_t1 := Try(func() {
			Await(co, SpawnAsync(d, func(s *Domain) *Promise[float64] {
				return Async(s, func(co *Co) float64 {
					SetTimeout(s, func(*Domain) {
						panic(thrownError("spawn-uncaught.ts", 13, 7, 13, `      throw new Error("in a spawned timer");`, ErrorBase, "in a spawned timer"))
					}, 1)
					Await(co, sleepFn(s, 50))
					return 1
				})
			}))
			LogString("never")
		}); ts_t1 != nil {
			Log("never caught", UnknownToString(ts_t1.V))
		}
		return struct{}{}
	})
	RunModule(ts_module, "/CASES/spawn-uncaught.ts")
}

// TestSpawnedFailureIsUncaughtInTheCaller: a throw nothing in the spawned
// domain catches, before its result, is uncaught in main, as under Node.
func TestSpawnedFailureIsUncaughtInTheCaller(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	matchAsync(t, "spawn-uncaught", spawnUncaught, "")
}
