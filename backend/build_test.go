package backend

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestBuildTrimsPaths: a program is built with -trimpath, so the temporary
// directory it was built in appears nowhere in the binary. Without it, Go's
// build cache keyed the runtime package on that directory, a new one every
// build, and stored 2.1 MB a build, the same program or not.
func TestBuildTrimsPaths(t *testing.T) {
	t.Parallel()

	m := &ir.Module{Name: "t", Main: []ir.Stmt{logNum(num(1))}}
	out := filepath.Join(t.TempDir(), "prog")
	if src, err := Build(ir.ProgramOf(m), Options{Output: out}); err != nil {
		t.Fatalf("Build: %v\n%s", err, src)
	}
	info, err := exec.Command("go", "version", "-m", out).Output()
	if err != nil || !strings.Contains(string(info), "-trimpath=true") {
		t.Errorf("the program wasn't built with -trimpath (go version -m: %v):\n%s", err, info)
	}
	bin, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bin, []byte("cakebear-build-")) {
		t.Errorf("the binary holds the path of the directory it was built in")
	}
}
