package runtime

import (
	"bufio"
	"testing"
)

// TestLogErrorKeepsTheProgramsOrder (M6 step 6): console.error and
// console.warn write what stdout holds first, so one stream holding both, as
// a terminal or a pipe does, has the lines in the program's order, as Node
// writes them, each at once.
func TestLogErrorKeepsTheProgramsOrder(t *testing.T) {
	both := &syncBuffer{}
	savedOut, savedErr := out, errOut
	outMu.Lock()
	out = bufio.NewWriter(both)
	outMu.Unlock()
	errMu.Lock()
	errOut = both
	errMu.Unlock()
	t.Cleanup(func() {
		outMu.Lock()
		out = savedOut
		outMu.Unlock()
		errMu.Lock()
		errOut = savedErr
		errMu.Unlock()
	})

	Log("log 1")
	LogError("error 1")
	Log("log 2")
	Log("log 3")
	LogError("error", 2.0, NegZero, NullOf[struct{}]())
	LogError()
	Log("log 4")
	Flush()
	if got, want := both.String(), "log 1\nerror 1\nlog 2\nlog 3\nerror 2 -0 null\n\nlog 4\n"; got != want {
		t.Errorf("one stream holds %q, want %q", got, want)
	}
}
