package runtime

import (
	"fmt"
	"slices"
)

// URLSearchParams is WHATWG's URLSearchParams as Node 24.18 has it (R41–
// R43): a list of name and value pairs, in order, duplicates kept, read and
// written in the application/x-www-form-urlencoded format (url_encode.go).
// One that is a URL's searchParams writes every change back to the URL's
// query. Every string a method takes is a USVString, so a lone surrogate
// in one is U+FFFD.
type URLSearchParams struct {
	list []paramPair
	url  *URL // the URL whose searchParams these are, or nil
}

// NewURLSearchParams is `new URLSearchParams()`.
func NewURLSearchParams() *URLSearchParams { return &URLSearchParams{} }

// NewURLSearchParamsString is `new URLSearchParams(init)`: init parsed, a
// leading `?` dropped.
func NewURLSearchParamsString(init string) *URLSearchParams {
	init = usv(init)
	if init != "" && init[0] == '?' {
		init = init[1:]
	}
	return &URLSearchParams{list: parseForm(init)}
}

// NewURLSearchParamsPairs is `new URLSearchParams({ … })` or of an array of
// pairs: name, value, name, value, in order.
func NewURLSearchParamsPairs(pairs ...string) *URLSearchParams {
	if len(pairs)%2 != 0 {
		panic(fmt.Sprintf("cakebear: NewURLSearchParamsPairs was given %d strings, not pairs", len(pairs)))
	}
	p := &URLSearchParams{list: make([]paramPair, 0, len(pairs)/2)}
	for i := 0; i < len(pairs); i += 2 {
		p.list = append(p.list, paramPair{usv(pairs[i]), usv(pairs[i+1])})
	}
	return p
}

func (p *URLSearchParams) update() {
	if p.url != nil {
		p.url.updateQuery(p.list)
	}
}

// Append is `p.append(name, value)`: the pair, last.
func (p *URLSearchParams) Append(name, value string) {
	p.list = append(p.list, paramPair{usv(name), usv(value)})
	p.update()
}

// Delete is `p.delete(name)`: every pair of that name.
func (p *URLSearchParams) Delete(name string) {
	name = usv(name)
	p.list = slices.DeleteFunc(p.list, func(q paramPair) bool { return q.name == name })
	p.update()
}

// DeleteValue is `p.delete(name, value)`: every pair of that name and
// value.
func (p *URLSearchParams) DeleteValue(name, value string) {
	name, value = usv(name), usv(value)
	p.list = slices.DeleteFunc(p.list, func(q paramPair) bool { return q.name == name && q.value == value })
	p.update()
}

// Get is `p.get(name)`: the first value of that name, or null.
func (p *URLSearchParams) Get(name string) Maybe[string] {
	name = usv(name)
	for _, q := range p.list {
		if q.name == name {
			return Some(q.value)
		}
	}
	return NullOf[string]()
}

// GetAll is `p.getAll(name)`: every value of that name, in order.
func (p *URLSearchParams) GetAll(name string) *Array[string] {
	name = usv(name)
	values := []string{}
	for _, q := range p.list {
		if q.name == name {
			values = append(values, q.value)
		}
	}
	return NewArray(values...)
}

// Has is `p.has(name)`.
func (p *URLSearchParams) Has(name string) bool {
	name = usv(name)
	return slices.ContainsFunc(p.list, func(q paramPair) bool { return q.name == name })
}

// HasValue is `p.has(name, value)`.
func (p *URLSearchParams) HasValue(name, value string) bool {
	name, value = usv(name), usv(value)
	return slices.ContainsFunc(p.list, func(q paramPair) bool { return q.name == name && q.value == value })
}

// Set is `p.set(name, value)`: the first pair of that name takes the value
// where it is, and the rest go; with none, the pair goes last.
func (p *URLSearchParams) Set(name, value string) {
	name, value = usv(name), usv(value)
	if i := slices.IndexFunc(p.list, func(q paramPair) bool { return q.name == name }); i >= 0 {
		p.list[i].value = value
		rest := slices.DeleteFunc(p.list[i+1:], func(q paramPair) bool { return q.name == name })
		p.list = p.list[:i+1+len(rest)]
	} else {
		p.list = append(p.list, paramPair{name, value})
	}
	p.update()
}

// Sort is `p.sort()`: stable, by name, in UTF-16 code units (R42), which
// orders a character past U+FFFF before one from U+E000 where Go's string
// order doesn't.
func (p *URLSearchParams) Sort() {
	slices.SortStableFunc(p.list, func(a, b paramPair) int { return StrCompare(a.name, b.name) })
	p.update()
}

// Size is `p.size`.
func (p *URLSearchParams) Size() float64 { return float64(len(p.list)) }

// Len is the number of pairs now, for forEach, which the emitter writes as
// a loop over the live list.
func (p *URLSearchParams) Len() int { return len(p.list) }

// Entry is the i-th pair.
func (p *URLSearchParams) Entry(i int) (name, value string) {
	return p.list[i].name, p.list[i].value
}

// String is `p.toString()`, `String(p)` and a template's `${p}`: the list
// in the form encoding.
func (p *URLSearchParams) String() string { return serializeForm(p.list) }
