package runtime

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// What is left of Node's event loop, for the main program's domain.
//
// cakebear runs each request in its own domain, in parallel with main and
// with each other (domain.go). Run drives the main domain once the program's
// top-level code has finished, in the order an ES module sees under Node
// (measured on Node 24.18; server programs are ES modules because they
// import node:http):
//
//   - the microtasks the top-level code queued;
//   - callbacks such as listen's, in the order they were queued, each
//     followed by the microtasks it queued. Node runs these through
//     process.nextTick, which an ES module drains after its microtasks;
//   - timers, as they come due, each followed by its microtasks.
//
// The process stays alive while a server is listening, a request's work is
// still running after its response (liveHandles), or main has a timer
// pending, and it exits when none of those holds. Ctrl-C and SIGTERM end it
// with the exit codes a shell reports for Node, after flushing output.
//
// The generated main calls Run last whenever the program uses a host API.

// liveHandles counts listening servers and requests still running after their
// response. While it is above zero, Run blocks and console output is written
// through rather than buffered.
var liveHandles atomic.Int64

var (
	queueMu sync.Mutex
	queue   []func()
	// wake is signalled whenever the queue or liveHandles changes, so Run can
	// re-check its state without polling. One slot is enough: Run re-reads
	// everything each time it wakes.
	wake = make(chan struct{}, 1)
)

// exit and errOut are variables so tests can see what a run would do to the
// process without ending the test binary.
var (
	exit   = os.Exit
	errMu  sync.Mutex
	errOut io.Writer = os.Stderr
)

func notify() {
	select {
	case wake <- struct{}{}:
	default:
	}
}

// enqueue schedules f to run on the main goroutine, after top-level code.
func enqueue(f func()) {
	queueMu.Lock()
	queue = append(queue, f)
	queueMu.Unlock()
	notify()
}

func takeQueue() []func() {
	queueMu.Lock()
	defer queueMu.Unlock()
	q := queue
	queue = nil
	return q
}

func queueEmpty() bool {
	queueMu.Lock()
	defer queueMu.Unlock()
	return len(queue) == 0
}

// ref records a server that started listening, or a request whose work runs
// on after its response, and unref one that stopped.
func ref() {
	liveHandles.Add(1)
	// Anything logged before the server came up is due now, not at exit.
	Flush()
	notify()
}

func unref() {
	liveHandles.Add(-1)
	notify()
}

// Run drives the main domain on the main goroutine until nothing is left: no
// queued callback, no pending timer, nothing listening. With none of those it
// returns at once, so a program that uses none behaves exactly as it would
// without it.
func Run() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	run(sigs)
}

func run(sigs <-chan os.Signal) {
	d := mainDomain
	d.drain()
	for {
		for _, f := range takeQueue() {
			d.runTask(f)
		}
		d.fireDue()
		if liveHandles.Load() == 0 && queueEmpty() && !d.pending() {
			return
		}
		// About to wait, so whatever was logged is due now: a program
		// sleeping on a timer shows its output before the wait, not at exit.
		Flush()
		var due <-chan time.Time
		var clock *time.Timer
		if when, ok := d.nextDue(); ok {
			clock = time.NewTimer(time.Until(when))
			due = clock.C
		}
		select {
		case <-wake:
		case <-due:
		case s := <-sigs:
			Flush()
			exit(signalExitCode(s))
			return
		}
		if clock != nil {
			clock.Stop()
		}
	}
}

// signalExitCode is what a shell reports for a process ended by s: 128 plus
// the signal's number, so 130 for Ctrl-C and 143 for SIGTERM, as for Node.
func signalExitCode(s os.Signal) int {
	if n, ok := s.(syscall.Signal); ok {
		return 128 + int(n)
	}
	return 1
}

// fatalf ends the process the way an uncaught error ends a Node program: a
// message on stderr and exit code 1, after flushing the output written so far.
func fatalf(format string, args ...any) {
	Flush()
	warnf(format, args...)
	exit(1)
}

// warnf writes one line to stderr.
func warnf(format string, args ...any) {
	errMu.Lock()
	defer errMu.Unlock()
	_, _ = fmt.Fprintf(errOut, format+"\n", args...)
}
