package lower

import (
	"strings"
	"testing"
)

// S1 step 6 (plan D8): what process.env refuses under cakebear's own
// declaration (types/node-globals.d.ts), which makes a write and process's
// other members type errors. env_e2e_test.go, in cmd/cakec, holds the
// reads to Node and the refusals under @types/node's.
func TestEnvRefusals(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, src, want string }{
		{"process.env whole", "console.log(process.env);\n", "process.env as a whole is not supported yet: read a variable by name"},
		{"a number as the key", "console.log(process.env[1]);\n", "process.env is read by a string here, not a number"},
		{"in with a number", "console.log(1 in process.env);\n", "in process.env takes a string here, not a number"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, tt.src+"export {};\n")
			if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
		})
	}
}

// TestOwnProcessIsAnOrdinaryBinding: process is recognised by where it is
// declared, so a program's own is an ordinary object.
func TestOwnProcessIsAnOrdinaryBinding(t *testing.T) {
	t.Parallel()
	_, errs := lowerSource(t, "interface P { env: number }\nconst process: P = { env: 1 };\nconsole.log(process.env);\nexport {};\n")
	if len(errs) > 0 {
		t.Fatalf("errors = %q, want none", joinMsgs(errs))
	}
}
