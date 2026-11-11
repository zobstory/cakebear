package runtime

import "strings"

// How console.log prints a URL and a URLSearchParams (R24, R25): each by
// its own [util.inspect.custom] in Node's lib/internal/url.js, neither as a
// plain object is printed.

// urlValue is URL's custom inspect. It is handed the depth left, Node's
// default less how deep the URL is, and below 0 returns the URL itself,
// which has no own properties to print: `URL {}`. Otherwise it inspects
// afresh an object of its getters, searchParams among them, at Node's
// default depth and from no indentation, so it is laid out as at the top
// level wherever it is; formatValue then indents its lines to where it is.
func (c *inspector) urlValue(u *URL, recurse int) string {
	if inspectDepth-recurse < 0 {
		return "URL {}"
	}
	values := [...]any{u.Href(), u.Origin(), u.Protocol(), u.Username(), u.Password(), u.Host(),
		u.Hostname(), u.Port(), u.Pathname(), u.Search(), u.params, u.Hash()}
	keys := [...]string{"href", "origin", "protocol", "username", "password", "host",
		"hostname", "port", "pathname", "search", "searchParams", "hash"}
	var inner inspector
	output := make([]string, len(values))
	for i, v := range values {
		inner.indent += 2
		output[i] = keys[i] + ": " + inner.value(v, 1)
		inner.indent -= 2
	}
	s := "URL " + inner.reduceToSingleString(output, "", [2]string{"{", "}"}, false, nil)
	return c.indentLines(s)
}

// paramsValue is URLSearchParams' custom inspect: `[Object]` past the
// depth, and otherwise its pairs as a Map's are, `'a' => '1'`, each string
// inspected afresh. They are on one line unless they are longer joined
// than the break length, wherever it is printed, and then a line each,
// with the `}` on the last one's.
func (c *inspector) paramsValue(p *URLSearchParams, recurse int) string {
	if inspectDepth-recurse < 0 {
		return "[Object]"
	}
	if len(p.list) == 0 {
		return "URLSearchParams {}"
	}
	var inner inspector
	output := make([]string, len(p.list))
	length := -2
	for i, q := range p.list {
		output[i] = inner.str(q.name) + " => " + inner.str(q.value)
		length += utf16Len(output[i]) + 2
	}
	if length > breakLength {
		return c.indentLines("URLSearchParams {\n  " + strings.Join(output, ",\n  ") + " }")
	}
	return c.indentLines("URLSearchParams { " + strings.Join(output, ", ") + " }")
}

// indentLines is what formatValue does to a custom inspect's string: each
// line after the first indented to where it is printed.
func (c *inspector) indentLines(s string) string {
	if c.indent == 0 {
		return s
	}
	return strings.ReplaceAll(s, "\n", "\n"+strings.Repeat(" ", c.indent))
}
