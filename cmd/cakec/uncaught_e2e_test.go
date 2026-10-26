package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	cakebearrt "github.com/zobstory/cakebear/backend/runtime"
)

// An uncaught throw ends a compiled program as it ends Node's (M6 plan D6):
// the output so far, then on stderr a report, then exit code 1. The report
// ends with the error as Node prints it under --stack-trace-limit=0, a
// blank line, and cakebear's version where Node prints its own. Above that
// comes the source excerpt of where it points, once lowering gives the
// emitter its sites (M6 step 3); until then there is none.

// checkUncaught runs bin, expecting it to print stdout, then end with the
// uncaught error whose report Node 24.18 ended with body, under
// --stack-trace-limit=0: Node's stderr from its first blank line on, less
// its version line.
func checkUncaught(t *testing.T, bin, stdout, body string) {
	t.Helper()
	var out, errOut strings.Builder
	cmd := exec.Command(bin)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("%v, want exit 1; stderr:\n%s", err, errOut.String())
	}
	tail := body + "\n\ncakebear " + version + "\n"
	if got := errOut.String(); !strings.HasSuffix(got, tail) || strings.Contains(got, "goroutine") {
		t.Errorf("stderr is\n%s\nwant it to end as Node's does\n%s", got, tail)
	}
	if out.String() != stdout {
		t.Errorf("stdout = %q, want only what ran before the throw, %q", out.String(), stdout)
	}
}

// TestRuntimeVersionIsCakecs: the version an uncaught error's report names
// is the version of the cakec that built the program, which embeds the
// runtime's source, so the two constants must be one.
func TestRuntimeVersionIsCakecs(t *testing.T) {
	t.Parallel()
	if cakebearrt.Version != version {
		t.Errorf("the runtime says cakebear %s, cakec %s", cakebearrt.Version, version)
	}
}
