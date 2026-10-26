package runtime

import (
	"errors"
	"strings"
	"testing"
)

// TestCaretLinesMatchNode: the line under the source line, as Node 24.18
// wrote it (scratch measurements, m6-runtime): a tab for a tab, a space for
// anything else, a code unit's worth each, then a caret; and Node's quirk,
// which reads the line's bytes with a column in code units, so a tab after
// a character past ASCII moves to where its byte is.
func TestCaretLinesMatchNode(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		text string
		col  int
		want string
	}{
		{`throw new Error("m");`, 7, "      ^"},
		{`throw new Error("m");`, 1, "^"},
		{"\tthrow new Error(\"tab\");", 8, "\t      ^"},
		{`const s = "日本😀"; throw new Error(s);`, 25, strings.Repeat(" ", 24) + "^"},
		{"/*日*/\tthrow new Error(\"q\");", 13, "       \t    ^"},
		{"\tif (true) throw new RangeError(\"from inner\");", 18, "\t                ^"},
	} {
		if got := caretLine(c.text, c.col); got != c.want {
			t.Errorf("caretLine(%q, %d) = %q, want %q", c.text, c.col, got, c.want)
		}
	}
}

// TestFileURLsMatchNode: a site's file as url.pathToFileURL(p).href writes
// it, Node 24.18's, which is how Node names a module in a report.
func TestFileURLsMatchNode(t *testing.T) {
	t.Parallel()

	for _, c := range [][2]string{
		{"/CASES/c.ts", "file:///CASES/c.ts"},
		{"/a b/c#d.ts", "file:///a%20b/c%23d.ts"},
		{"/x/~y/é.ts", "file:///x/%7Ey/%C3%A9.ts"},
		{"/q?r/[s].ts", "file:///q%3Fr/%5Bs%5D.ts"},
		{"/t\tu/v%w.ts", "file:///t%09u/v%25w.ts"},
		{"/a/./b/../c.ts", "file:///a/c.ts"},
		{"/a//b.ts", "file:///a/b.ts"},
		{"/a\xed\xa0\x80b", "file:///a%EF%BF%BDb"}, // a lone surrogate
		{"/a!$&'()*+,-.:;=@_z", "file:///a!$&'()*+,-.:;=@_z"},
		{"/\"<>\\^`{|}\x7f", "file:///%22%3C%3E%5C%5E%60%7B%7C%7D%7F"},
	} {
		if got := fileURL(c[0]); got != c[1] {
			t.Errorf("fileURL(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

// TestReportsPointWhereNodeDoes: which site a report shows (R6). An Error
// is shown where it was made at the top level and in a microtask, and where
// it was thrown in a timer, the listen callback and a handler; a value that
// isn't an Error, and an Error that knows no site of its own, where it was
// thrown. With no site at all the report has no excerpt.
func TestReportsPointWhereNodeDoes(t *testing.T) {
	t.Parallel()

	made, thrown := site("x.ts", 1, 1, "made"), site("x.ts", 2, 1, "thrown")
	version := "\n\ncakebear " + Version + "\n"
	for _, c := range []struct {
		name string
		t    *Thrown
		p    phase
		want string
	}{
		{"top level", &Thrown{V: NewError(ErrorBase, "m", made), Site: thrown}, inModule, "file:///CASES/x.ts:1\nmade\n^\n\n[Error: m]" + version},
		{"microtask", &Thrown{V: NewError(ErrorBase, "m", made), Site: thrown}, inMicrotask, "file:///CASES/x.ts:1\nmade\n^\n\n[Error: m]" + version},
		{"timer", &Thrown{V: NewError(ErrorBase, "m", made), Site: thrown}, inTimer, "file:///CASES/x.ts:2\nthrown\n^\n\n[Error: m]" + version},
		{"callback", &Thrown{V: NewError(ErrorBase, "m", made), Site: thrown}, inCallback, "file:///CASES/x.ts:2\nthrown\n^\n\n[Error: m]" + version},
		{"handler", &Thrown{V: NewError(ErrorBase, "m", made), Site: thrown}, inHandler, "file:///CASES/x.ts:2\nthrown\n^\n\n[Error: m]" + version},
		{"made nowhere", &Thrown{V: NewError(ErrorBase, "m", nil), Site: thrown}, inModule, "file:///CASES/x.ts:2\nthrown\n^\n\n[Error: m]" + version},
		{"thrown nowhere", &Thrown{V: NewError(ErrorBase, "m", made)}, inTimer, "file:///CASES/x.ts:1\nmade\n^\n\n[Error: m]" + version},
		{"no site", &Thrown{V: NewError(ErrorType, "t", nil)}, inModule, "[TypeError: t]" + version},
		// A site with no line has its location alone; one with no file is
		// no site at all.
		{"no text", &Thrown{V: NewError(ErrorBase, "m", &Site{File: "/CASES/x.ts", Line: 3, Col: 7})}, inModule,
			"file:///CASES/x.ts:3\n\n[Error: m]" + version},
		{"no text, a string", &Thrown{V: "m", Site: &Site{File: "/CASES/x.ts", Line: 3, Col: 7}}, inModule,
			"\nfile:///CASES/x.ts:3\nm" + version},
		{"no file", &Thrown{V: NewError(ErrorBase, "m", &Site{Line: 3}), Site: thrown}, inModule,
			"file:///CASES/x.ts:2\nthrown\n^\n\n[Error: m]" + version},
		{"a string", &Thrown{V: "m", Site: thrown}, inModule, "\nfile:///CASES/x.ts:2\nthrown\n^\nm" + version},
		{"undefined, no site", &Thrown{V: nil}, inTimer, "undefined" + version},
	} {
		if got := uncaughtReport(c.t, c.p); got != c.want {
			t.Errorf("%s: reported\n%q\nwant\n%q", c.name, got, c.want)
		}
	}
}

// TestExitIsFlushOrTheReport: with nothing panicking, Exit only flushes the
// output; a throw is reported, exit code 1, after the output; and a Go
// panic goes on, the same value, after the output, with no report and no
// exit code of Exit's own.
func TestExitIsFlushOrTheReport(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	stdout, stderr, exits := isolate(t)
	func() {
		defer Exit()
		LogString("fine")
	}()
	if stdout.String() != "fine\n" || stderr.String() != "" || len(exits) != 0 {
		t.Errorf("a program that ended printed %q, reported %q, exited %d times", stdout.String(), stderr.String(), len(exits))
	}

	bug := errors.New("a cakebear bug")
	var got any
	func() {
		defer func() { got = recover() }()
		defer Exit()
		LogString("before the bug")
		panic(bug)
	}()
	if err, _ := got.(error); !errors.Is(err, bug) || stdout.String() != "fine\nbefore the bug\n" || stderr.String() != "" || len(exits) != 0 {
		t.Errorf("a Go panic came out as %v, with %q printed, %q reported and %d exits", got, stdout.String(), stderr.String(), len(exits))
	}
}
