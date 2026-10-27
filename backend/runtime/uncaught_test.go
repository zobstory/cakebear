package runtime

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The report of an uncaught error (M6 plan D6), held to what Node 24.18
// printed for the programs in testdata/uncaught/
// (testdata/gen-uncaught-goldens.mjs). Each program here is the case's,
// written as the emitter writes it: its sites name the case's file under
// /CASES, as the goldens write it, and its lines and columns.

// uncaughtRun is how Node ran a case: what it printed, how it ended, and
// what a request to it got.
type uncaughtRun struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Client string `json:"client"`
	Exit   int    `json:"exit"`
}

type uncaughtCase struct {
	Rule   string      `json:"rule"`
	Limit0 uncaughtRun `json:"limit0"`
	Frames uncaughtRun `json:"frames"`
}

func loadUncaught(t *testing.T) map[string]uncaughtCase {
	t.Helper()
	data, err := os.ReadFile("testdata/uncaught-goldens.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases map[string]uncaughtCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return file.Cases
}

// nodeHint is Node's advice for a thrown value that isn't an Error, which a
// binary that node doesn't run leaves out.
const nodeHint = "(Use `node --trace-uncaught ...` to show where the exception was thrown)\n"

// cakebearsReport is the report cakebear makes of c, by its rule
// (gen-uncaught-goldens.mjs, gen-async-goldens.mjs). A throw-site or
// rejection-value case has none from Node: want is its excerpt, or "" for
// none.
func cakebearsReport(t *testing.T, c uncaughtCase, want string) string {
	t.Helper()
	const nodeVersion = "\nNode.js v24.18.0\n"
	limit0 := c.Limit0.Stderr
	if !strings.HasSuffix(limit0, nodeVersion) {
		t.Fatalf("Node's report doesn't end with its version:\n%s", limit0)
	}
	limit0 = strings.TrimSuffix(limit0, nodeVersion) + "\ncakebear " + Version + "\n"
	limit0 = strings.Replace(limit0, nodeHint, "", 1)
	body := limit0[strings.Index(limit0, "\n\n")+2:] // from the error on
	switch c.Rule {
	case "limit0":
		return limit0
	case "header-from-frames":
		lines := strings.SplitAfterN(c.Frames.Stderr, "\n", 4)
		return strings.Join(lines[:3], "") + "\n" + body
	case "rejection-value":
		// R13's UnhandledPromiseRejection is an Error, reported as one.
		if want == "" {
			return body
		}
		return want + "\n" + body
	}
	// A non-Error has no blank line between the caret and the value, and
	// none before the value when there is no excerpt.
	value := limit0[strings.Index(limit0, "    ^\n")+6:]
	if want == "" {
		return value
	}
	return "\n" + want + value
}

// site is a place in a case.
func site(file string, line, col int, text string) *Site {
	return &Site{File: "/CASES/" + file, Line: line, Col: col, Text: text}
}

// throwAt is `throw v` at a site.
func throwAt(v any, at *Site) { panic(&Thrown{V: v, Site: at}) }

// runMain runs program as the generated main runs the program's top-level
// code, under Exit, then the loop, and returns how it ended.
func runMain(t *testing.T, program func(d *Domain)) (stdout, stderr string, exitCode int) {
	t.Helper()
	out, errOut, exits := isolate(t)
	func() {
		defer Exit()
		program(Main())
		Run()
	}()
	select {
	case exitCode = <-exits:
	default:
		exitCode = 0
	}
	return out.String(), errOut.String(), exitCode
}

// uncaughtPrograms are the cases that aren't servers.
var uncaughtPrograms = map[string]func(*Domain){
	"top-error": func(*Domain) {
		LogString("before")
		text := `throw new Error("m");`
		throwAt(NewError(ErrorBase, "m", site("top-error.ts", 4, 7, text)), site("top-error.ts", 4, 1, text))
	},
	"top-made-elsewhere": func(*Domain) {
		e := NewError(ErrorType, "made here", site("top-made-elsewhere.ts", 3, 18, `const e        = new TypeError("made here");`))
		LogString("before")
		throwAt(e, site("top-made-elsewhere.ts", 5, 1, "throw e;"))
	},
	"top-function": func(*Domain) {
		const text = "\tif (true) throw new RangeError(\"from inner\");"
		inner := func() {
			throwAt(NewError(ErrorRange, "from inner", site("top-function.ts", 4, 18, text)), site("top-function.ts", 4, 12, text))
		}
		outer := func() { inner() }
		outer()
		LogString("not reached")
	},
	"top-string": func(*Domain) {
		LogString("before")
		throwAt("m", site("top-string.ts", 4, 28, `const why         = "m";   throw why;`))
	},
	"top-wide": func(*Domain) {
		const text = "/*日*/\tthrow new Error(\"日本😀\");"
		throwAt(NewError(ErrorBase, "日本😀", site("top-wide.ts", 3, 13, text)), site("top-wide.ts", 3, 7, text))
	},
	"top-builtin": func(*Domain) {
		n := -1.0
		LogString("before")
		LogString(StrRepeat("ab", n, site("top-builtin.ts", 5, 18, `console.log("ab".repeat(n));`)))
	},
	"timer-error": func(d *Domain) {
		e := NewError(ErrorBase, "made at the top", site("timer-error.ts", 3, 18, `const e        = new Error("made at the top");`))
		LogString("sync")
		SetTimeout(d, func(*Domain) {
			LogString("timer 1")
			throwAt(e, site("timer-error.ts", 7, 3, "  throw e;"))
		}, 0)
		SetTimeout(d, func(*Domain) { LogString("timer 2, never") }, 0)
	},
	"timer-values": func(d *Domain) {
		box := newTestObject([]string{"a", "list"}, []any{1.0, NewArray[any](nil, Null, "s")})
		SetTimeout(d, func(*Domain) { throwAt(box, site("timer-values.ts", 6, 3, "  throw box;")) }, 0)
	},
	"timer-string": func(d *Domain) {
		SetTimeout(d, func(*Domain) { throwAt("from a timer", site("timer-string.ts", 4, 3, `  throw "from a timer";`)) }, 0)
	},
	"timer-builtin": func(d *Domain) {
		a := newTestObject([]string{"next"}, []any{nil})
		a.values[0] = NewArray[any](a)
		SetTimeout(d, func(*Domain) {
			LogString(JSONStringify(a, JSONGap{}, site("timer-builtin.ts", 7, 20, "  console.log(JSON.stringify(a));")))
		}, 0)
	},
}

// The excerpts of the throw-site cases, where Node points into its own code.
var throwSites = map[string]string{
	"top-string": "file:///CASES/top-string.ts:4\nconst why         = \"m\";   throw why;\n" + strings.Repeat(" ", 27) + "^\n",
}

// TestUncaughtReportsMatchNode: an uncaught throw ends the program with
// exit code 1, after the output so far, and the report Node prints, where
// it points as R6 says for where the throw went uncaught: at the error's
// creation at an ES module's top level, and at the throw in a timer.
func TestUncaughtReportsMatchNode(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	cases := loadUncaught(t)
	for name, program := range uncaughtPrograms { //nolint:paralleltest // isolate swaps process-wide runtime state
		t.Run(name, func(t *testing.T) {
			c, ok := cases[name]
			if !ok {
				t.Fatalf("no golden for %s", name)
			}
			stdout, stderr, code := runMain(t, program)
			if want := cakebearsReport(t, c, throwSites[name]); stderr != want {
				t.Errorf("stderr is\n%s\nwant, as Node's report\n%s", stderr, want)
			}
			if stdout != c.Limit0.Stdout || code != c.Limit0.Exit || code != 1 {
				t.Errorf("printed %q and exited %d, want %q and %d, as Node", stdout, code, c.Limit0.Stdout, c.Limit0.Exit)
			}
		})
	}
}

// TestListenCallbackThrowEndsTheProgram: a throw in the listen callback
// ends the program, as in Node, its report pointing at the throw.
func TestListenCallbackThrowEndsTheProgram(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	c := loadUncaught(t)["listen-callback"]
	var s *Server
	stdout, stderr, code := runMain(t, func(*Domain) {
		s = CreateServer(func(_ *Domain, _ *IncomingMessage, res *ServerResponse) { res.End("ok") })
		s.Listen(0, nil, func(*Domain) {
			LogString("listening")
			const text = `  throw new Error("from the listen callback");`
			throwAt(NewError(ErrorBase, "from the listen callback", site("listen-callback.ts", 6, 9, text)), site("listen-callback.ts", 6, 3, text))
		})
	})
	if s.srv != nil {
		_ = s.close()
	}
	if want := cakebearsReport(t, c, ""); stderr != want || stdout != c.Limit0.Stdout || code != 1 {
		t.Errorf("printed %q, exited %d, and reported\n%s\nwant %q, 1 and\n%s", stdout, code, stderr, c.Limit0.Stdout, want)
	}
}
