package backend

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// checkFailure holds a compiled program's end to what want says it is. A
// want starting "cakebear: " is a panic's message, where Node does
// something an exception can't stand for (carry on with undefined): exit 2,
// with that message among Go's trace. Anything else is String() of the
// Error Node throws, uncaught: exit 1, and on stderr the report Node prints
// under --stack-trace-limit=0, with cakebear's version for Node's (M6 plan
// D6, runtime/uncaught.go). The report points at the test's span, t.ts:1, a
// place with no line to quote, since a module built from IR has no
// ir.Module.Lines: its location, a blank line, the error, and no Go trace.
// Nothing is printed to stdout before either.
func checkFailure(t *testing.T, err error, stdout, stderr, want string) {
	t.Helper()
	var exit *exec.ExitError
	code := -1
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	}
	if strings.HasPrefix(want, "cakebear: ") {
		if code != 2 || stdout != "" || !strings.Contains(stderr, want) {
			t.Errorf("%v, stdout %q, stderr\n%s\nwant exit 2, nothing printed, and %q", err, stdout, stderr, want)
		}
		return
	}
	report := "file://t.ts:1\n\n[" + want + "]\n\ncakebear "
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, report) || strings.Count(stderr, "\n") != 5 {
		t.Errorf("%v, stdout %q, stderr\n%s\nwant exit 1, nothing printed, and the report %q and a version", err, stdout, stderr, report)
	}
}
