package runtime

// The parked registry (M6 plan D7; m6-go spike §4): the async calls waiting
// in an Await, so a domain that will never run again can end them.
//
// A parked call is a goroutine blocked until a microtask hands it the
// domain's token. A domain stops running when its request fails, when its
// driver runs out of work while calls still wait on promises nothing can
// settle (M2 left one goroutine behind for each), and when a spawned domain
// is done. Without the registry each such call was a goroutine left waiting
// for ever: 100 of them for 100 requests that threw with one call parked.
// abort hands each one the token once more, marked aborted, and Await then
// exits its goroutine with runtime.Goexit, which runs only the runtime's own
// deferred calls (Try's Catch, which sees no panic, and Async's handoff):
// the generated code defers nothing but Exit, in main. So none of the
// program runs, no finally included, which is what Node does with a call
// that never resumes.
//
// Only the token holder touches the registry: Await parks its call before
// it hands the token back, and the reaction that resumes it unparks it
// first. A slice with each call's index kept in its Co makes both O(1) and
// hashes nothing.

func (d *Domain) park(co *Co) {
	co.parkedAt = len(d.parked)
	d.parked = append(d.parked, co)
}

func (d *Domain) unpark(co *Co) {
	last := len(d.parked) - 1
	moved := d.parked[last]
	d.parked[co.parkedAt], moved.parkedAt = moved, co.parkedAt
	d.parked[last] = nil
	d.parked = d.parked[:last]
}

// abort ends every call parked in d, which nothing will drive again. The
// caller holds d's token, and gets it back from each call as it exits.
func (d *Domain) abort() {
	for len(d.parked) > 0 {
		last := len(d.parked) - 1
		co := d.parked[last]
		d.parked[last] = nil
		d.parked = d.parked[:last]
		co.aborted = true
		co.resume <- struct{}{}
		<-co.yield
	}
}

// busy reports whether anything in d may still run after the code running
// now: pending work, a microtask already queued, or a parked call, which a
// later microtask may resume. A spawn's result or rejection reaches its
// caller as it is only when d is not busy (spawn.go): otherwise d could
// change it in parallel with the caller.
func (d *Domain) busy() bool {
	return d.pending() || d.microHead < len(d.micro) || len(d.parked) > 0
}
