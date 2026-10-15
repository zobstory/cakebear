package runtime

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// A port of Node's util.inspect (lib/internal/util/inspect.js, v24.18.0), for
// what the subset can build: numbers, strings, booleans, and the objects and
// arrays made of them (M4 plan §2c, rules R1–R13, D12). It follows inspect.js
// function by function, with its defaults: depth 2, 100 array items, 10,000
// string characters, a break length of 80 and compact 3. Colours are off, as
// they are when stdout isn't a terminal.
//
// Node measures in UTF-16 code units, and a Go string is UTF-8, so every
// length here is utf16Len, never len. Grouping measures display width
// (width.go) instead.

const (
	inspectDepth    = 2
	maxArrayLength  = 100
	maxStringLength = 10000
	breakLength     = 80
	compact         = 3
)

// Log is console.log of several values (R1): strings as they are, anything
// else inspected, joined by a space.
func Log(args ...any) { writeLine(Format(args...)) }

// Format is util.format for arguments whose first, if it is a string, holds
// no `%`. Lowering refuses a first argument that might (D12), so no
// substitution is ever needed; a `%` here would be a bug, so it panics.
func Format(args ...any) string {
	if len(args) == 0 {
		return "" // console.log(): an empty line
	}
	if first, ok := args[0].(string); ok && len(args) > 1 && strings.Contains(first, "%") {
		panic("cakebear: Format was given a format string")
	}
	var b strings.Builder
	for i, arg := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		if s, ok := arg.(string); ok {
			b.WriteString(s)
		} else {
			b.WriteString(Inspect(arg))
		}
	}
	return b.String()
}

// Inspect is util.inspect(v) with Node's defaults.
func Inspect(v any) string {
	var c inspector
	return c.value(v, 0)
}

// inspector is inspect.js's ctx.
type inspector struct {
	indent   int   // ctx.indentationLvl
	seen     []any // the containers being formatted, outermost first
	circular map[any]int
}

// array is what inspect needs of an *Array[T], whatever T is.
type array interface {
	inspectLen() int
	inspectAt(i int) any
	numeric() bool
}

// value is formatValue.
func (c *inspector) value(v any, recurse int) string {
	switch x := v.(type) {
	case float64:
		return numberForDisplay(x)
	case float32:
		return numberForDisplay(float64(x))
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case bool:
		return BoolToString(x)
	case string:
		return c.str(x)
	case Object, array:
		return c.container(x, recurse)
	}
	panic(fmt.Sprintf("cakebear: inspect has no way to print a %T", v))
}

// container is the rest of formatValue, and formatRaw, for an object or an
// array.
func (c *inspector) container(v any, recurse int) string {
	for _, s := range c.seen {
		if s == v {
			return fmt.Sprintf("[Circular *%d]", c.circularIndex(v))
		}
	}
	arr, isArray := v.(array)
	obj, _ := v.(Object)
	if isArray && arr.inspectLen() == 0 {
		return "[]" // R7: even past the depth limit
	}
	if !isArray && len(obj.Layout().keys) == 0 {
		return "{}"
	}
	if recurse > inspectDepth {
		if isArray {
			return "[Array]" // R8
		}
		return "[Object]"
	}
	recurse++
	c.seen = append(c.seen, v)

	var output []string
	if isArray {
		output = c.arrayEntries(arr, recurse)
	} else {
		output = c.objectEntries(obj, recurse)
	}
	base := ""
	if i, ok := c.circular[v]; ok {
		base = fmt.Sprintf("<ref *%d>", i)
	}
	c.seen = c.seen[:len(c.seen)-1]

	braces := [2]string{"{", "}"}
	if isArray {
		braces = [2]string{"[", "]"}
	}
	return c.reduceToSingleString(output, base, braces, isArray, arr)
}

func (c *inspector) circularIndex(v any) int {
	if c.circular == nil {
		c.circular = make(map[any]int)
	}
	i, ok := c.circular[v]
	if !ok {
		i = len(c.circular) + 1
		c.circular[v] = i
	}
	return i
}

// arrayEntries is formatArray: at most 100 elements, then a count of the
// rest (R11).
func (c *inspector) arrayEntries(arr array, recurse int) []string {
	n := min(arr.inspectLen(), maxArrayLength)
	output := make([]string, 0, n+1)
	for i := range n {
		c.indent += 2
		output = append(output, c.value(arr.inspectAt(i), recurse))
		c.indent -= 2
	}
	if remaining := arr.inspectLen() - n; remaining > 0 {
		output = append(output, fmt.Sprintf("... %d more item%s", remaining, plural(remaining)))
	}
	return output
}

// objectEntries is formatProperty for each key, in the object's own order
// (R6).
func (c *inspector) objectEntries(obj Object, recurse int) []string {
	layout := obj.Layout()
	output := make([]string, len(layout.keys))
	for i, field := range layout.fields {
		c.indent += 2
		value := c.value(obj.Field(field), recurse)
		c.indent -= 2
		output[i] = layout.printed[i] + ": " + value
	}
	return output
}

func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}

// reduceToSingleString puts the entries on one line if they fit (R9),
// grouping a long array's first (R10), and otherwise one per line. A grouped
// array is never on one line.
//
// inspect.js also requires `ctx.currentDepth - recurseTimes < ctx.compact`,
// the depth of the container formatted last less this one's. At Node's
// default depth (2) and compact (3) that always holds, the first being at
// most 3 and the second at least 1, so it is left out.
func (c *inspector) reduceToSingleString(output []string, base string, braces [2]string, isArray bool, arr array) string {
	entries := len(output)
	if isArray && entries > 6 {
		output = c.groupArrayElements(output, arr)
	}
	if entries == len(output) {
		start := len(output) + c.indent + utf16Len(braces[0]) + utf16Len(base) + 10
		if c.isBelowBreakLength(output, start, base) {
			joined := strings.Join(output, ", ")
			if !strings.Contains(joined, "\n") {
				return prefixed(base) + braces[0] + " " + joined + " " + braces[1]
			}
		}
	}
	indentation := "\n" + strings.Repeat(" ", c.indent)
	return prefixed(base) + braces[0] + indentation + "  " +
		strings.Join(output, ","+indentation+"  ") + indentation + braces[1]
}

func prefixed(base string) string {
	if base == "" {
		return ""
	}
	return base + " "
}

func (c *inspector) isBelowBreakLength(output []string, start int, base string) bool {
	total := len(output) + start
	if total+len(output) > breakLength {
		return false
	}
	for _, entry := range output {
		total += utf16Len(entry)
		if total > breakLength {
			return false
		}
	}
	return base == "" || !strings.Contains(base, "\n")
}

// groupArrayElements lines short entries up in columns (R10), numbers
// aligned right and anything else left.
func (c *inspector) groupArrayElements(output []string, arr array) []string {
	totalLength, maxLength := 0, 0
	outputLength := len(output)
	if maxArrayLength < len(output) {
		outputLength-- // the "... n more items" entry isn't measured
	}
	const separatorSpace = 2
	dataLen := make([]int, outputLength)
	for i := range outputLength {
		n := displayWidth(output[i])
		dataLen[i] = n
		totalLength += n + separatorSpace
		maxLength = max(maxLength, n)
	}
	actualMax := maxLength + separatorSpace
	if actualMax*3+c.indent >= breakLength ||
		!(float64(totalLength)/float64(actualMax) > 5 || maxLength <= 6) {
		return output
	}
	averageBias := math.Sqrt(float64(actualMax) - float64(totalLength)/float64(len(output)))
	biasedMax := math.Max(float64(actualMax)-3-averageBias, 1)
	columns := min(
		int(jsRound(math.Sqrt(2.5*biasedMax*float64(outputLength))/biasedMax)),
		(breakLength-c.indent)/actualMax,
		compact*4,
		15,
	)
	if columns <= 1 {
		return output
	}
	maxLineLength := make([]int, columns)
	for i := range columns {
		lineMax := 0
		for j := i; j < outputLength; j += columns {
			lineMax = max(lineMax, dataLen[j])
		}
		maxLineLength[i] = lineMax + separatorSpace
	}
	right := arr != nil && arr.numeric()
	var grouped []string
	for i := 0; i < outputLength; i += columns {
		last := min(i+columns, outputLength)
		var b strings.Builder
		j := i
		for ; j < last-1; j++ {
			padding := maxLineLength[j-i] + utf16Len(output[j]) - dataLen[j]
			b.WriteString(pad(output[j]+", ", padding, right))
		}
		if right {
			padding := maxLineLength[j-i] + utf16Len(output[j]) - dataLen[j] - separatorSpace
			b.WriteString(pad(output[j], padding, true))
		} else {
			b.WriteString(output[j])
		}
		grouped = append(grouped, b.String())
	}
	if maxArrayLength < len(output) {
		grouped = append(grouped, output[outputLength])
	}
	return grouped
}

// pad is padStart (left of s) or padEnd, to width UTF-16 units.
func pad(s string, width int, start bool) string {
	n := width - utf16Len(s)
	if n <= 0 {
		return s
	}
	if start {
		return strings.Repeat(" ", n) + s
	}
	return s + strings.Repeat(" ", n)
}

// jsRound is Math.round: halves round up, toward +Infinity.
func jsRound(f float64) float64 { return math.Floor(f + 0.5) }

// utf16Len is a string's length as JavaScript counts it.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r >= 0x10000 {
			n++
		}
	}
	return n
}
