package runtime

import (
	"path"
	"strconv"
	"strings"
)

// Uncaught errors (M6 plan D6): a throw nothing caught ends the program as
// it ends Node's, with the output written so far (R3), a report on stderr
// and exit code 1. The report is Node's, where what it says comes from the
// program:
//
//	file:///Users/…/x.ts:3           where it points, as a file: URL
//	throw new Error("m");            the source line, its types blanked
//	      ^                          a caret under the column
//	                                 a blank line
//	[Error: m]                       the error as console.log prints it
//	                                 a blank line
//	cakebear 0.0.0-dev               in place of Node.js v24.18.0
//
// A value that isn't an Error has a blank line first and none after the
// caret, and is printed as console.log prints it, a string as it is (R7).
// Where Node prints a place in its own code instead, a non-Error at an ES
// module's top level or one of node:http's errors, the report points at the
// program's own site, and Node's advice to run node --trace-uncaught is
// left out everywhere: the program isn't run by node. Where the site isn't
// known, the report has no excerpt.

// Version is cakebear's release, which the report names in place of Node's
// version. It is the version of the cakec that built the program, which
// embeds this source: TestRuntimeVersionIsCakecs holds the two equal.
const Version = "0.0.0-dev"

// phase is what a domain's driver is running, which decides where the
// report of an uncaught Error points (R6).
type phase uint8

const (
	inModule    phase = iota // top-level code, and what it calls
	inMicrotask              // a promise reaction: code after an await
	inTimer                  // a timer's callback
	inCallback               // a callback queued for main: listen's
	inHandler                // a request handler
)

// pointsAtCreation reports whether an uncaught Error's report points at
// where the error was made, rather than at the throw. Node does where it
// takes the place from the error's stack: an ES module's top level, a
// microtask, a rejection and a top-level await (R6, c1-04, c2-03). It
// points at the throw in a timer, a request handler and the listen
// callback.
func (p phase) pointsAtCreation() bool { return p == inModule || p == inMicrotask }

// Exit is deferred first by the generated main, in place of Flush. A throw
// that reached main, from its top-level code, a timer, a microtask or the
// listen callback, or a rejection nothing handled there (rejection.go), ends
// the process as it ends Node's: the output so far, the report, and exit
// code 1. Any other panic goes on up from here, after the output, so the
// crash still shows where it began, marked [recovered, repanicked].
//
// A program that ended normally has its microtasks drained once more, which
// is a check for rejections: a program with no await or timer never calls
// Run, yet an async call's throw, or a reject, may have left one (R9).
func Exit() {
	r := recover()
	if r == nil {
		r = settle(mainDomain)
	}
	Flush()
	if r == nil {
		return
	}
	report, ok := failureReport(r, mainDomain.running)
	if !ok {
		panic(r)
	}
	writeErr(report)
	exit(1)
}

// settle drains d and gives back what that panicked with, if anything.
func settle(d *Domain) (r any) {
	defer func() { r = recover() }()
	d.drain()
	return nil
}

// uncaughtReport is the report of t, a throw nothing caught while p ran.
func uncaughtReport(t *Thrown, p phase) string {
	if e, ok := t.V.(*Error); ok {
		site := known(t.Site)
		if made := known(e.site); made != nil && (site == nil || p.pointsAtCreation()) {
			site = made
		}
		return errorReport(e, site)
	}
	var b strings.Builder
	if site := known(t.Site); site != nil {
		b.WriteByte('\n')
		writeExcerpt(&b, site)
	}
	b.WriteString(Format(t.V))
	b.WriteString(versionLines)
	return b.String()
}

// errorReport is the report of an Error, pointing at site, if it is known.
func errorReport(e *Error, site *Site) string {
	var b strings.Builder
	if site != nil {
		writeExcerpt(&b, site)
		b.WriteByte('\n')
	}
	b.WriteString(Inspect(e))
	b.WriteString(versionLines)
	return b.String()
}

// versionLines end a report, in place of Node's `Node.js v24.18.0`.
const versionLines = "\n\ncakebear " + Version + "\n"

// known is site, or nil for one that names no file: a place nobody knows.
func known(site *Site) *Site {
	if site == nil || site.File == "" {
		return nil
	}
	return site
}

// writeExcerpt writes where site is, its line, and a caret under it. A
// site with no Text, which the emitter passes until lowering gives it the
// line, has its location alone.
func writeExcerpt(b *strings.Builder, site *Site) {
	b.WriteString(fileURL(site.File))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(site.Line))
	b.WriteByte('\n')
	if site.Text == "" {
		return
	}
	b.WriteString(site.Text)
	b.WriteByte('\n')
	b.WriteString(caretLine(site.Text, site.Col))
	b.WriteByte('\n')
}

// caretLine is the line Node writes under the source line (node_errors.cc,
// GetErrorSource): for each code unit before the column, a tab where the
// line has a tab and a space elsewhere, then one caret. Node indexes the
// line's UTF-8 bytes with the UTF-16 column, so a tab after a character
// beyond ASCII is copied to where its byte is, not its character (measured:
// `/*日*/\tthrow` puts it 7 places in, after 7 spaces, where the line has it
// 5 characters in), and so does this.
func caretLine(text string, col int) string {
	var b strings.Builder
	for i := range max(col-1, 0) {
		if i >= len(text) {
			break
		}
		if text[i] == '\t' {
			b.WriteByte('\t')
		} else {
			b.WriteByte(' ')
		}
	}
	b.WriteByte('^')
	return b.String()
}

// fileURL is url.pathToFileURL(p).href, as Node names a module in a report:
// the path cleaned, and every byte but an ASCII letter, a digit and
// !$&'()*+,-./:;=@_ written as %XX, in upper case, a lone surrogate as
// U+FFFD's (measured on Node 24.18: ~ is escaped too).
func fileURL(p string) string {
	p = wellFormed(path.Clean(p))
	var b strings.Builder
	b.WriteString("file://")
	for i := range len(p) {
		c := p[i]
		if c < 0x80 && urlPathKept[c] {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(uriHex[c>>4])
		b.WriteByte(uriHex[c&0xf])
	}
	return b.String()
}

// urlPathKept is the ASCII pathToFileURL leaves as it is.
var urlPathKept = func() (kept [0x80]bool) {
	for _, c := range "!$&'()*+,-./0123456789:;=@ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz" {
		kept[c] = true
	}
	return kept
}()

// writeErr writes s to stderr whole, in UTF-8 as Node writes it: a lone
// surrogate is U+FFFD.
func writeErr(s string) {
	errMu.Lock()
	defer errMu.Unlock()
	_, _ = errOut.Write([]byte(wellFormed(s)))
}
