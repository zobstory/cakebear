package backend

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/zobstory/cakebear/ir"
)

// siteTable is the package-level array of every place a report may point at.
const siteTable = "ts_sites"

// siteArg is the *cakebearrt.Site argument of a runtime call that may throw
// (runtime/thrown.go), the place an uncaught error's report points at (M6
// plan D6): a throw, a `new Error`, or a call the runtime throws for. Each
// place is one entry of the program's site table, however many calls name
// it. A span with no file is a place the report doesn't know: nil.
func (e *emitter) siteArg(at ir.Span) string {
	if at.File == "" || at.Line < 1 {
		return "nil"
	}
	e.usesRuntime = true
	key := fmt.Sprintf("%s:%d:%d", at.File, at.Line, at.Col)
	n, ok := e.siteIndex[key]
	if !ok {
		if e.siteIndex == nil {
			e.siteIndex = map[string]int{}
		}
		n = len(e.sites)
		e.siteIndex[key] = n
		e.sites = append(e.sites, at)
	}
	return fmt.Sprintf("&%s[%d]", siteTable, n)
}

// siteDecl declares the site table, each place with its line as Node quotes
// it, from the module's stripped lines (ir.Module.Lines), and its column in
// UTF-16 units, as V8 counts it, where the span's is in bytes.
func (e *emitter) siteDecl() string {
	if len(e.sites) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "var %s = [...]%s.Site{\n", siteTable, rtPkg)
	for _, at := range e.sites {
		text, col := "", at.Col
		if lines := e.lines[at.File]; at.Line <= len(lines) {
			text = lines[at.Line-1]
			col = utf16Col(text, at.Col)
		}
		fmt.Fprintf(&b, "\t{File: %s, Line: %d, Col: %d, Text: %s},\n", strconv.Quote(at.File), at.Line, col, strconv.Quote(text))
	}
	b.WriteString("}\n\n")
	return b.String()
}

// utf16Col is the 1-based column col, a count of bytes into line, as a count
// of UTF-16 code units. Past the line's end it is col unchanged.
func utf16Col(line string, col int) int {
	if col < 1 || col-1 > len(line) {
		return col
	}
	units := 1
	for _, r := range line[:col-1] {
		if r >= 0x10000 && r != utf8.RuneError {
			units += 2
		} else {
			units++
		}
	}
	return units
}
