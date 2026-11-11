package runtime

import (
	"container/heap"
	"math"
	"time"
)

// AbortSignal.timeout(ms) as fetch's signal (S1 plan D2, s1-node R16): a
// signal its own timer aborts with a DOMException named TimeoutError. The
// timer is unref'd, as Node's is, so a signal keeps nothing alive: a
// program that made one and has nothing else to do ends at once, and a
// request's domain needn't wait for it after the response.

// AbortSignal is a signal, of the domain that made it. Lowering keeps it
// from reaching another domain, as it does any host value.
type AbortSignal struct {
	d *Domain
	// reason is what it was aborted with, nil while it hasn't been.
	reason *Error
	// waiting are the fetches and body reads it would end.
	waiting []*abortWait
}

// abortWait is one fetch or body read waiting under a signal: abort ends
// it, unless it has finished on its own (done).
type abortWait struct {
	abort func(*Error)
	done  bool
}

// release is the wait's end on its own: an abort after it does nothing.
func (w *abortWait) release() {
	if w != nil {
		w.done = true
	}
}

// timeoutMessage is the DOMException's message for a timeout (R16).
const timeoutMessage = "The operation was aborted due to timeout"

// newDOMException is a DOMException named name: printed `[DOMException
// [TimeoutError]: …]`, `TimeoutError: …` as a string, `{}` in JSON, and
// an Error to instanceof. Its code (23 for a timeout) is a getter on
// DOMException's prototype, which nothing here reads.
func newDOMException(name, message string) *Error {
	return &Error{kind: ErrorDOMException, name: name, message: message}
}

// AbortSignalTimeout is `AbortSignal.timeout(ms)`, called at site: ms must
// be an integer from 0 to 2^32 - 1, or it throws Node's RangeError there.
func AbortSignalTimeout(d *Domain, ms float64, site *Site) *AbortSignal {
	switch {
	case ms != math.Trunc(ms) || math.IsInf(ms, 0):
		throwCoded(ErrorRange, "ERR_OUT_OF_RANGE", `The value of "delay" is out of range. It must be an integer. Received `+NumberToString(ms), site)
	case ms < 0 || ms > math.MaxUint32:
		throwCoded(ErrorRange, "ERR_OUT_OF_RANGE", `The value of "delay" is out of range. It must be >= 0 && <= 4294967295. Received `+NumberToString(ms), site)
	}
	s := &AbortSignal{d: d}
	setUnrefTimeout(d, func(*Domain) { s.abort(newDOMException("TimeoutError", timeoutMessage)) }, ms)
	return s
}

// abort aborts the signal with reason, ending everything waiting under it,
// in the order they began to wait. A second abort does nothing.
func (s *AbortSignal) abort(reason *Error) {
	if s.reason != nil {
		return
	}
	s.reason = reason
	waiting := s.waiting
	s.waiting = nil
	for _, w := range waiting {
		if !w.done {
			w.done = true
			w.abort(reason)
		}
	}
}

// wait has abort called when the signal aborts, until the wait is
// released. A signal from another domain panics, as a promise does.
func (s *AbortSignal) wait(d *Domain, abort func(*Error)) *abortWait {
	if s.d != d {
		panic(errCrossDomain)
	}
	w := &abortWait{abort: abort}
	live := s.waiting[:0]
	for _, o := range s.waiting {
		if !o.done {
			live = append(live, o)
		}
	}
	s.waiting = append(live, w)
	return w
}

// setUnrefTimeout is a timer that keeps nothing alive: it fires while the
// domain has other work to do, and is dropped with the domain otherwise.
func setUnrefTimeout(d *Domain, cb func(*Domain), ms float64) {
	d.seq++
	t := &timer{when: time.Now().Add(timerDelay(ms)), seq: d.seq, id: math.NaN(), cb: cb, unref: true}
	heap.Push(&d.timers, t)
	d.unrefd++
}
