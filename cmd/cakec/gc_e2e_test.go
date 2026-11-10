package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// garbageProgram makes about 40 MB of short-lived strings, enough for several
// collections at any GOGC but off.
const garbageProgram = `let total: number = 0;
for (let i = 0; i < 20000; i++) {
  const s: string = "x".repeat(1000) + String(i);
  total += s.length;
}
console.log(total);
`

// firstGoal matches the heap goal of the first collection in a gctrace line,
// which is Go's 4 MB minimum heap scaled by GOGC/100.
var firstGoal = regexp.MustCompile(`(?m)^gc 1 .* (\d+) MB goal`)

// TestCompiledProgramsCollectAtGOGC200: a compiled program runs with GOGC=200
// unless its environment sets GOGC, which stands, off included
// (runtime/gc.go). The first collection's heap goal says which is in force: 8
// MB at 200, 4 at Go's default and 2 at 50.
func TestCompiledProgramsCollectAtGOGC200(t *testing.T) {
	t.Parallel()

	bin := buildProgram(t, "garbage", garbageProgram)
	var base []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GOGC=") && !strings.HasPrefix(kv, "GODEBUG=") {
			base = append(base, kv)
		}
	}
	for _, tt := range []struct {
		gogc string // "" leaves it unset
		goal string // "" for no collection at all
	}{
		{"", "8"},
		{"100", "4"},
		{"50", "2"},
		{"off", ""},
	} {
		t.Run("GOGC="+tt.gogc, func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command(bin)
			cmd.Env = append(append([]string{}, base...), "GODEBUG=gctrace=1")
			if tt.gogc != "" {
				cmd.Env = append(cmd.Env, "GOGC="+tt.gogc)
			}
			var stderr strings.Builder
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil || string(out) != "20088890\n" {
				t.Fatalf("printed %q (%v), want %q\n%s", out, err, "20088890\n", stderr.String())
			}
			m := firstGoal.FindStringSubmatch(stderr.String())
			switch {
			case tt.goal == "" && m != nil:
				t.Errorf("GOGC=off collected: %s", m[0])
			case tt.goal != "" && m == nil:
				t.Errorf("no first collection in the trace:\n%s", stderr.String())
			case tt.goal != "" && m[1] != tt.goal:
				t.Errorf("the first collection's goal is %s MB, want %s MB: %s", m[1], tt.goal, m[0])
			}
		})
	}
}
