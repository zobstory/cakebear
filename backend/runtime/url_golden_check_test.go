package runtime

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Running one URL golden (url_goldens_test.go): each kind of line made
// again through the runtime, and what Node recorded, as two strings to
// compare.

func text(raw json.RawMessage) string {
	var g goldenText
	if err := json.Unmarshal(raw, &g); err != nil {
		panic(err)
	}
	return string(g)
}

// check runs golden g, giving what the runtime made of it and what Node
// did.
func (g urlGolden) check() (got, want string) {
	switch g.K {
	case "p":
		return g.checkParse()
	case "set":
		u := NewURL(string(*g.Base), nil)
		v := text(g.V)
		if got = urlThrows(func() { applySetter(u, g.Op, v) }); got == "" {
			got = urlRecordOf(u)
		}
		return got, wantRecord(g)
	case "usp":
		return g.checkParams()
	case "live":
		return g.checkLive()
	case "form":
		p := NewURLSearchParamsString(string(*g.In))
		var dec []string
		for i := range p.Len() {
			name, value := p.Entry(i)
			dec = append(dec, name, value)
		}
		got = NewURLSearchParamsPairs(string(*g.In), string(*g.In)).String() + "\x1e" + strings.Join(dec, "\x1f") + "\x1e" + p.String()
		want = string(*g.Ser) + "\x1e" + joinText(g.Dec) + "\x1e" + string(*g.Round)
		return got, want
	case "insp":
		return Inspect(buildURLValue(g.V)), text(g.Out)
	case "json":
		v := buildURLValue(g.V)
		return JSONStringify(v, JSONGap{}, nil) + "\x1e" + JSONStringify(v, JSONGapNumber(2), nil), text(g.Out) + "\x1e" + string(*g.Out2)
	}
	panic("no golden kind " + g.K)
}

func joinText(ts []goldenText) string {
	parts := make([]string, len(ts))
	for i, s := range ts {
		parts[i] = string(s)
	}
	return strings.Join(parts, "\x1f")
}

func (g urlGolden) checkParse() (got, want string) {
	in := string(*g.In)
	var u *URL
	var can bool
	var parsed Maybe[*URL]
	switch {
	case g.Base == nil:
		got = urlThrows(func() { u = NewURL(in, nil) })
		can, parsed = URLCanParse(in), URLParse(in)
	case g.BU:
		base := NewURL(string(*g.Base), nil)
		got = urlThrows(func() { u = NewURLWithBaseURL(in, base, nil) })
		can, parsed = URLCanParseWithBase(in, base.Href()), URLParseWithBase(in, base.Href())
	default:
		got = urlThrows(func() { u = NewURLWithBase(in, string(*g.Base), nil) })
		can, parsed = URLCanParseWithBase(in, string(*g.Base)), URLParseWithBase(in, string(*g.Base))
	}
	if got == "" {
		got = urlRecordOf(u)
		if parsed.IsNull() || parsed.Must("parsed").Href() != u.Href() {
			got += " (URL.parse differs)"
		}
	} else if !parsed.IsNull() {
		got += " (URL.parse doesn't fail)"
	}
	if can != (u != nil) {
		got += " (URL.canParse differs)"
	}
	return got, wantRecord(g)
}

func applySetter(u *URL, op, v string) {
	switch op {
	case "href":
		u.SetHref(v, nil)
	case "protocol":
		u.SetProtocol(v)
	case "username":
		u.SetUsername(v)
	case "password":
		u.SetPassword(v)
	case "host":
		u.SetHost(v)
	case "hostname":
		u.SetHostname(v)
	case "port":
		u.SetPort(v)
	case "pathname":
		u.SetPathname(v)
	case "search":
		u.SetSearch(v)
	case "hash":
		u.SetHash(v)
	default:
		panic("no setter " + op)
	}
}

// applyParams runs a URLSearchParams method, and gives its result as
// canonResult writes Node's.
func applyParams(p *URLSearchParams, op []string) string {
	a := op[1:]
	switch op[0] {
	case "append":
		p.Append(a[0], a[1])
	case "delete":
		if len(a) > 1 {
			p.DeleteValue(a[0], a[1])
		} else {
			p.Delete(a[0])
		}
	case "set":
		p.Set(a[0], a[1])
	case "sort":
		p.Sort()
	case "get":
		if m := p.Get(a[0]); !m.IsNull() {
			return "s:" + strconv.Quote(m.Must("value"))
		}
		return "null"
	case "getAll":
		all := p.GetAll(a[0])
		values := make([]string, all.Len())
		for i := range values {
			values[i] = strconv.Quote(all.At(i))
		}
		return "a:" + strings.Join(values, ",")
	case "has":
		if len(a) > 1 {
			return "b:" + strconv.FormatBool(p.HasValue(a[0], a[1]))
		}
		return "b:" + strconv.FormatBool(p.Has(a[0]))
	case "size":
		return "n:" + NumberToString(p.Size())
	case "toString":
		return "s:" + strconv.Quote(p.String())
	case "forEach":
		var values []string
		for i := range p.Len() {
			name, value := p.Entry(i)
			values = append(values, strconv.Quote(name), strconv.Quote(value))
		}
		return "a:" + strings.Join(values, ",")
	default:
		panic("no method " + op[0])
	}
	return "void"
}

func canonResult(raw json.RawMessage) string {
	switch s := string(raw); {
	case s == "null":
		return "null"
	case s == `{"v":1}`:
		return "void"
	case s == "true" || s == "false":
		return "b:" + s
	case s[0] == '[':
		var ts []goldenText
		if err := json.Unmarshal(raw, &ts); err != nil {
			panic(err)
		}
		values := make([]string, len(ts))
		for i, t := range ts {
			values[i] = strconv.Quote(string(t))
		}
		return "a:" + strings.Join(values, ",")
	case s[0] == '"' || s[0] == '{':
		return "s:" + strconv.Quote(text(raw))
	default:
		return "n:" + s
	}
}

func opStrings(op []json.RawMessage) []string {
	out := make([]string, len(op))
	for i, raw := range op {
		out[i] = text(raw)
	}
	return out
}

func (g urlGolden) checkParams() (got, want string) {
	var p *URLSearchParams
	if g.Init != nil {
		p = NewURLSearchParamsString(string(*g.Init))
	} else {
		pairs := make([]string, len(g.Pairs))
		for i, s := range g.Pairs {
			pairs[i] = string(s)
		}
		p = NewURLSearchParamsPairs(pairs...)
	}
	var out [][2]json.RawMessage
	if err := json.Unmarshal(g.Out, &out); err != nil {
		panic(err)
	}
	var gots, wants []string
	for i, raw := range g.Ops {
		gots = append(gots, applyParams(p, opStrings(raw))+" -> "+p.String())
		wants = append(wants, canonResult(out[i][0])+" -> "+text(out[i][1]))
	}
	return strings.Join(gots, "\n"), strings.Join(wants, "\n")
}

func (g urlGolden) checkLive() (got, want string) {
	u := NewURL(string(*g.Base), nil)
	p := u.SearchParams()
	var out [][4]json.RawMessage
	if err := json.Unmarshal(g.Out, &out); err != nil {
		panic(err)
	}
	var gots, wants []string
	for i, raw := range g.Ops {
		op := opStrings(raw)
		if op[0] == "u" {
			applySetter(u, op[1], op[2])
		} else {
			applyParams(p, op[1:])
		}
		if u.SearchParams() != p {
			gots = append(gots, "searchParams is a new object")
		}
		gots = append(gots, strings.Join([]string{u.Href(), u.Search(), p.String(), NumberToString(p.Size())}, "\x1f"))
		wants = append(wants, strings.Join([]string{text(out[i][0]), text(out[i][1]), text(out[i][2]), string(out[i][3])}, "\x1f"))
	}
	return strings.Join(gots, "\n"), strings.Join(wants, "\n")
}

// buildURLValue makes the value a golden describes, objects and arrays as
// the backend generates them.
func buildURLValue(raw json.RawMessage) any {
	var d struct {
		URL   *goldenText          `json:"url"`
		Base  *goldenText          `json:"base"`
		Parse *goldenText          `json:"parse"`
		USP   *goldenText          `json:"usp"`
		USPP  []goldenText         `json:"uspp"`
		S     *goldenText          `json:"s"`
		N     *float64             `json:"n"`
		O     [][2]json.RawMessage `json:"o"`
		A     []json.RawMessage    `json:"a"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		panic(err)
	}
	switch {
	case d.URL != nil && d.Base != nil:
		return NewURLWithBase(string(*d.URL), string(*d.Base), nil)
	case d.URL != nil:
		return NewURL(string(*d.URL), nil)
	case d.Parse != nil:
		return URLParse(string(*d.Parse))
	case d.USP != nil:
		return NewURLSearchParamsString(string(*d.USP))
	case d.USPP != nil:
		pairs := make([]string, len(d.USPP))
		for i, s := range d.USPP {
			pairs[i] = string(s)
		}
		return NewURLSearchParamsPairs(pairs...)
	case d.S != nil:
		return string(*d.S)
	case d.N != nil:
		return *d.N
	case d.O != nil:
		keys := make([]string, len(d.O))
		values := make([]any, len(d.O))
		for i, kv := range d.O {
			keys[i] = text(kv[0])
			values[i] = buildURLValue(kv[1])
		}
		return newTestObject(keys, values)
	case d.A != nil:
		arr := NewArray[any]()
		for _, v := range d.A {
			arr.Push(buildURLValue(v))
		}
		return arr
	}
	panic("bad description " + string(raw))
}
