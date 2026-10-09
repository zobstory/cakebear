package runtime

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
)

// What is left of Node's event loop.
//
// cakebear runs request handlers on goroutines, genuinely in parallel, rather
// than on an event loop (CLAUDE.md, semantics decision 3). What remains is the
// part a program can observe from main:
//
//   - the process stays alive while a server is listening, and exits when none
//     is (liveHandles);
//   - callbacks such as listen's run after the program's top-level code has
//     finished, on the main goroutine, in the order they were queued;
//   - Ctrl-C and SIGTERM end it with the exit codes a shell reports for Node,
//     after flushing output.
//
// The generated main calls Run last whenever the program uses a host API.

// liveHandles counts listening servers. While it is above zero, Run blocks
// and console output is written through rather than buffered.
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

// ref records a server that started listening, and unref one that stopped.
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

// Run executes queued callbacks on the main goroutine, then blocks while any
// server is listening. With nothing queued and nothing listening it returns at
// once, so a program without a server behaves exactly as it would without it.
func Run() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	run(sigs)
}

func run(sigs <-chan os.Signal) {
	for {
		for _, f := range takeQueue() {
			f()
		}
		if liveHandles.Load() == 0 && queueEmpty() {
			return
		}
		select {
		case <-wake:
		case s := <-sigs:
			Flush()
			exit(signalExitCode(s))
			return
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

// fatal ends the process the way an uncaught error ends a Node program: a
// message on stderr and exit code 1, after flushing the output written so far.
func fatal(format string, args ...any) {
	Flush()
	warn(format, args...)
	exit(1)
}

// warn writes one line to stderr.
func warn(format string, args ...any) {
	errMu.Lock()
	defer errMu.Unlock()
	_, _ = fmt.Fprintf(errOut, format+"\n", args...)
}
