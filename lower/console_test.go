package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestConsoleMembers (M6 step 6): log, info and debug write where log
// writes, and error and warn to stderr, as Node's do.
func TestConsoleMembers(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `console.log("a");
console.info("b");
console.debug("c");
console.error("d", 1);
console.warn("e");
`)
	for i, want := range []bool{false, false, false, true, true} {
		log := must[*ir.ConsoleLog](t, must[*ir.ExprStmt](t, m.Main[i], "a statement").Expr, "a console call")
		if log.Stderr != want {
			t.Errorf("statement %d: Stderr = %v, want %v", i+1, log.Stderr, want)
		}
	}
}

// TestConsoleRefusals: a member cakebear doesn't lower is refused by name,
// and a program's own console is an ordinary value, never Node's.
func TestConsoleRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"console.trace", "console.trace(\"x\");\n", "console.trace is not supported yet; console.log, info, debug, error and warn are"},
		{"a format", "const s: string = \"%d\";\nconsole.error(s, 1);\n", "a string before console.error's other arguments is a format to Node"},
		{"a promise", "async function f(): Promise<number> {\n  return 1;\n}\nconsole.warn(f());\n", "console.warn cannot print a promise yet"},
		{"the program's own console", "export {};\nconst console: { log: (s: string) => void } = { log: (s: string): void => {} };\nconsole.log(\"x\");\n",
			"only calls to a function by name are supported yet"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, tt.src)
			if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
		})
	}
}
