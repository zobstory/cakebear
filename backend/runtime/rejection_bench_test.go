package runtime

import "testing"

// One await of a settled promise: the token to the driver and back through
// a microtask, and since M6 step 4 the call parked and unparked. The m6-go
// spike measured the registry at +10.6 ns an await (+4%); the step's
// measurement against the runtime before it is in its report.
func BenchmarkAwait(b *testing.B) {
	d := newDomain()
	done := Async(d, func(*Co) float64 { return 1 })
	b.ResetTimer()
	d.runTask(func() {
		Async(d, func(co *Co) struct{} {
			for range b.N {
				Await(co, done)
			}
			return struct{}{}
		})
	})
}

// The same inside a try, as `try { await p } catch {}` runs.
func BenchmarkAwaitInTry(b *testing.B) {
	d := newDomain()
	done := Async(d, func(*Co) float64 { return 1 })
	b.ResetTimer()
	d.runTask(func() {
		Async(d, func(co *Co) struct{} {
			for range b.N {
				if t := Try(func() { Await(co, done) }); t != nil {
					b.Fatal("threw")
				}
			}
			return struct{}{}
		})
	})
}

// An await of a rejected promise, caught around the await: the rejection
// thrown again in the awaiter and caught one frame up.
func BenchmarkAwaitRejectedCaught(b *testing.B) {
	d := newDomain()
	var failed *Promise[float64]
	d.runTask(func() {
		failed = Async(d, func(*Co) float64 { panic(&Thrown{V: "x"}) })
		Async(d, func(co *Co) struct{} {
			Try(func() { Await(co, failed) }) // handles it, so the drain's check passes
			return struct{}{}
		})
	})
	b.ResetTimer()
	d.runTask(func() {
		Async(d, func(co *Co) struct{} {
			for range b.N {
				if t := Try(func() { Await(co, failed) }); t == nil {
					b.Fatal("not thrown")
				}
			}
			return struct{}{}
		})
	})
}
