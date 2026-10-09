package runtime

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer safe to read while the runtime writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// isolate swaps the runtime's process-wide state (stdout, stderr, exit, the
// queue and the handle count) for test doubles, and restores it afterwards.
// A test that calls it must not be parallel: Go runs those only after every
// sequential test has finished, so they never see the doubles.
func isolate(t *testing.T) (stdout, stderr *syncBuffer, exits chan int) {
	t.Helper()
	stdout, stderr, exits = &syncBuffer{}, &syncBuffer{}, make(chan int, 4)

	savedOut, savedErr, savedExit, savedHost := out, errOut, exit, listenHost
	outMu.Lock()
	out = bufio.NewWriter(stdout)
	outMu.Unlock()
	errMu.Lock()
	errOut = stderr
	errMu.Unlock()
	exit = func(code int) { exits <- code }
	listenHost = "127.0.0.1"

	t.Cleanup(func() {
		outMu.Lock()
		out = savedOut
		outMu.Unlock()
		errMu.Lock()
		errOut = savedErr
		errMu.Unlock()
		exit, listenHost = savedExit, savedHost
		_ = takeQueue()
		liveHandles.Store(0)
		select {
		case <-wake:
		default:
		}
	})
	return stdout, stderr, exits
}

// runAsync runs the loop on its own goroutine and returns a channel closed
// when it returns.
func runAsync(sigs <-chan os.Signal) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(sigs)
	}()
	return done
}

func waitFor(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// exitCode waits for the runtime to end the process, failing rather than
// hanging if it never does.
func exitCode(t *testing.T, exits <-chan int) int {
	t.Helper()
	select {
	case code := <-exits:
		return code
	case <-time.After(5 * time.Second):
		t.Fatal("the runtime never ended the process")
		return 0
	}
}

// While a server is listening, Run must not return: returning would end main
// and with it the process.
func TestRunBlocksWhileListening(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	isolate(t)
	ref()
	done := runAsync(nil)
	select {
	case <-done:
		t.Fatal("Run returned while a server was still listening")
	case <-time.After(150 * time.Millisecond):
	}
	unref()
	waitFor(t, done, "Run to return once nothing was listening")
}

// Handlers log from many goroutines at once. Under -race this fails on any
// unsynchronised access to the shared writer; without -race it still fails if
// lines from different goroutines interleave.
func TestConsoleIsSafeFromManyGoroutines(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	stdout, _, _ := isolate(t)

	const goroutines, lines = 100, 50
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Go(func() {
			for i := range lines {
				LogString(fmt.Sprintf("g%03d-line%03d", g, i))
			}
		})
	}
	wg.Wait()
	Flush()

	got := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(got) != goroutines*lines {
		t.Fatalf("got %d lines, want %d", len(got), goroutines*lines)
	}
	seen := map[string]bool{}
	for _, line := range got {
		if len(line) != len("g000-line000") || !strings.HasPrefix(line, "g") || seen[line] {
			t.Fatalf("damaged or repeated line %q", line)
		}
		seen[line] = true
	}
}

// A batch program keeps its buffering; a listening one writes each line as it
// comes, or "listening on …" would sit in the buffer until the process exits.
func TestConsoleWritesThroughOnlyWhileListening(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	stdout, _, _ := isolate(t)

	LogString("before")
	if stdout.String() != "" {
		t.Fatalf("a batch program's output was not buffered: %q", stdout.String())
	}
	ref()
	if stdout.String() != "before\n" {
		t.Fatalf("starting to listen did not flush what was buffered: %q", stdout.String())
	}
	LogString("while listening")
	if !strings.HasSuffix(stdout.String(), "while listening\n") {
		t.Fatalf("a line logged while listening was buffered: %q", stdout.String())
	}
	unref()
	LogString("after")
	if strings.Contains(stdout.String(), "after") {
		t.Fatalf("buffering did not resume once nothing was listening: %q", stdout.String())
	}
	Flush()
	if !strings.HasSuffix(stdout.String(), "after\n") {
		t.Fatalf("Flush lost a line: %q", stdout.String())
	}
}

// A program with no server must not hang: Run returns at once.
func TestRunReturnsWithNothingToDo(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	isolate(t)
	waitFor(t, runAsync(nil), "Run with nothing queued or listening")
}

// Callbacks run in the order they were queued, including ones queued by a
// callback, and all of them on the goroutine that called Run.
func TestRunDrainsTheQueueInOrder(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	isolate(t)

	var order []string
	enqueue(func() { order = append(order, "a") })
	enqueue(func() {
		order = append(order, "b")
		enqueue(func() { order = append(order, "d") })
	})
	enqueue(func() { order = append(order, "c") })
	run(nil)

	if got := strings.Join(order, ""); got != "abcd" {
		t.Errorf("callbacks ran in order %q, want abcd", got)
	}
}

// Ctrl-C and SIGTERM end a listening program with the codes a shell reports
// for Node, after flushing.
func TestRunExitsOnSignals(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	for _, tt := range []struct { //nolint:paralleltest // isolate swaps process-wide runtime state
		sig  os.Signal
		want int
	}{{os.Interrupt, 130}, {syscall.SIGTERM, 143}} {
		t.Run(tt.sig.String(), func(t *testing.T) {
			_, _, exits := isolate(t)
			ref()
			sigs := make(chan os.Signal, 1)
			done := runAsync(sigs)
			sigs <- tt.sig
			waitFor(t, done, "Run to stop on "+tt.sig.String())
			if got := exitCode(t, exits); got != tt.want {
				t.Errorf("exit code = %d, want %d", got, tt.want)
			}
		})
	}
}

// fatal flushes stdout before reporting, so nothing logged before the failure
// is lost.
func TestFatalFlushesThenExitsWithOne(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	stdout, stderr, exits := isolate(t)

	LogString("logged first")
	fatal("Error: %s", "boom")
	if got := exitCode(t, exits); got != 1 {
		t.Errorf("exit code = %d, want 1", got)
	}
	if stdout.String() != "logged first\n" {
		t.Errorf("stdout = %q, want the buffered line flushed", stdout.String())
	}
	if stderr.String() != "Error: boom\n" {
		t.Errorf("stderr = %q", stderr.String())
	}
}
