package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// A call to a library function cakebear doesn't implement is a limit of the
// current phase, reported like every other. It used to reach go build and fail
// there with "undefined: encodeURI", pointing at generated code.
func TestLibraryGlobalIsRefusedNotMiscompiled(t *testing.T) {
	t.Parallel()

	path := writeTS(t, "global.ts", "const s: string = encodeURI(\"a b\");\nconsole.log(s);\n")
	var stderr bytes.Buffer
	code := runBuild(buildOptions{files: []string{path}, output: filepath.Join(filepath.Dir(path), "global")}, &stderr)
	out := stderr.String()
	if code != exitErrors {
		t.Errorf("exit code = %d, want %d\n%s", code, exitErrors, out)
	}
	if want := "global.ts:1:19: cannot compile yet: encodeURI is a library function cakebear does not implement yet"; !strings.Contains(out, want) {
		t.Errorf("output missing %q:\n%s", want, out)
	}
	if strings.Contains(out, "go build failed") {
		t.Errorf("reached go build:\n%s", out)
	}
}
