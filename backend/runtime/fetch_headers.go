package runtime

import (
	"sort"
	"strings"
)

// Headers is fetch's Headers (S1 plan D2, R2, R3, R6): name and value pairs,
// a name compared without case.
//
// A program's own keeps its names as first written, in the order written,
// which is what a request sends (R6). A response's comes from Go's client,
// which keeps no order across names: iterating sorts them anyway (R3), and
// printing one, where Node keeps the wire's order (R22), is refused.
type Headers struct {
	entries []headerEntry
	// setCookie keeps the set-cookie values apart, for getSetCookie; get
	// joins them as it joins any repeat (R2).
	setCookie []string
	// immutable is set on a response's headers: a write throws, as under
	// Node's immutable guard.
	immutable bool
}

type headerEntry struct {
	name  string // as first written
	lower string
	value string // repeats joined with ", " as they are added (R2)
}

// NewHeaders is `new Headers()`, or of pairs in order: an object literal's
// properties, or an array of pairs. A bad name or value throws as Node's
// constructor does, through append (measured on Node 24.18).
func NewHeaders(pairs [][2]string, site *Site) *Headers {
	h := &Headers{}
	for _, p := range pairs {
		h.Append(p[0], p[1], site)
	}
	return h
}

// headerToken reports whether name is an HTTP token, RFC 9110's tchar.
func headerToken(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// normalizeValue is the Fetch Standard's: leading and trailing HTTP
// whitespace removed (R6: "  padded  " is sent as "padded").
func normalizeValue(v string) string { return strings.Trim(v, " \t\r\n") }

// validValue reports whether a normalised value holds no NUL, CR or LF.
func validValue(v string) bool { return !strings.ContainsAny(v, "\x00\r\n") }

func (h *Headers) check(op, name, value string, hasValue bool, site *Site) {
	if !headerToken(name) {
		throwError(ErrorType, "Headers."+op+": "+quoteJS(name)+" is an invalid header name.", site)
	}
	if hasValue && !validValue(value) {
		throwError(ErrorType, "Headers."+op+": "+quoteJS(value)+" is an invalid header value.", site)
	}
	if h.immutable && op != "get" && op != "has" {
		throwError(ErrorType, "Headers."+op+": immutable", site)
	}
}

// quoteJS is a string as Node's messages quote one, in double quotes, as
// it is: a newline in it stays a newline (R7).
func quoteJS(s string) string { return `"` + s + `"` }

func (h *Headers) find(lower string) int {
	for i, e := range h.entries {
		if e.lower == lower {
			return i
		}
	}
	return -1
}

// Append is `headers.append(name, value)`.
func (h *Headers) Append(name, value string, site *Site) {
	value = normalizeValue(value)
	h.check("append", name, value, true, site)
	lower := strings.ToLower(name)
	if lower == "set-cookie" {
		h.setCookie = append(h.setCookie, value)
	}
	if i := h.find(lower); i >= 0 {
		h.entries[i].value += ", " + value
		return
	}
	h.entries = append(h.entries, headerEntry{name: name, lower: lower, value: value})
}

// Set is `headers.set(name, value)`: the value replaces the name's, where
// the name first was.
func (h *Headers) Set(name, value string, site *Site) {
	value = normalizeValue(value)
	h.check("set", name, value, true, site)
	lower := strings.ToLower(name)
	if lower == "set-cookie" {
		h.setCookie = []string{value}
	}
	if i := h.find(lower); i >= 0 {
		h.entries[i].value = value
		return
	}
	h.entries = append(h.entries, headerEntry{name: name, lower: lower, value: value})
}

// Delete is `headers.delete(name)`.
func (h *Headers) Delete(name string, site *Site) {
	h.check("delete", name, "", false, site)
	lower := strings.ToLower(name)
	if lower == "set-cookie" {
		h.setCookie = nil
	}
	if i := h.find(lower); i >= 0 {
		h.entries = append(h.entries[:i], h.entries[i+1:]...)
	}
}

// Get is `headers.get(name)`: the value, repeats joined, or null (R2).
func (h *Headers) Get(name string, site *Site) Maybe[string] {
	h.check("get", name, "", false, site)
	if i := h.find(strings.ToLower(name)); i >= 0 {
		return Some(h.entries[i].value)
	}
	return NullOf[string]()
}

// Has is `headers.has(name)`.
func (h *Headers) Has(name string, site *Site) bool {
	h.check("has", name, "", false, site)
	return h.find(strings.ToLower(name)) >= 0
}

// GetSetCookie is `headers.getSetCookie()`, each value apart (R2).
func (h *Headers) GetSetCookie() *Array[string] {
	return NewArray(append([]string(nil), h.setCookie...)...)
}

// sorted is what iterating gives: lowercase names in order (R3).
func (h *Headers) sorted() []headerEntry {
	out := make([]headerEntry, len(h.entries))
	copy(out, h.entries)
	sort.SliceStable(out, func(i, j int) bool { return out[i].lower < out[j].lower })
	return out
}
