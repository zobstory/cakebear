package runtime

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// thrownBy runs f and gives the *Thrown it panicked with, or nil.
func thrownBy(f func()) (t *Thrown) {
	defer func() {
		if p := recover(); p != nil {
			t = p.(*Thrown)
		}
	}()
	f()
	return nil
}

// TestURLKeyOutputs: s1-node's R38–R43, a line each, as Node 24.18 gave
// them; the goldens hold the rest.
func TestURLKeyOutputs(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ in, base, want string }{
		{"HTTPS://EXAMPLE.COM/Path", "", "https://example.com/Path"},
		{"https://example.com:443", "", "https://example.com/"},
		{"http://example.com/a/../b/./c", "", "http://example.com/b/c"},
		{"http://example.com/café?q=é#é", "", "http://example.com/caf%C3%A9?q=%C3%A9#%C3%A9"},
		{"  http://example.com/trim  ", "", "http://example.com/trim"},
		{"http:example.com/x", "", "http://example.com/x"},
		{"http://example.com\\back\\slash", "", "http://example.com/back/slash"},
		{"http://0x7f.1/", "", "http://127.0.0.1/"},
		{"http://[0:0:0:0:0:0:13.1.68.3]", "", "http://[::d01:4403]/"},
		{"http://exämple.com/", "", "http://xn--exmple-cua.com/"},
		{"file:c:\\x\\y", "", "file:///c:/x/y"},
		{"sc:a  ?q", "", "sc:a %20?q"},
		{"../up", "https://example.com/dir/page.html?old=1#oldhash", "https://example.com/up"},
		{"", "https://example.com/dir/page.html?old=1#oldhash", "https://example.com/dir/page.html?old=1"},
	} {
		var u *URL
		if c.base == "" {
			u = NewURL(c.in, nil)
		} else {
			u = NewURLWithBase(c.in, c.base, nil)
		}
		if got := u.Href(); got != c.want || u.String() != got {
			t.Errorf("new URL(%q, %q) = %q, want %q", c.in, c.base, got, c.want)
		}
	}
	if got := NewURL("blob:https://example.com/uuid", nil).Origin(); got != "https://example.com" {
		t.Errorf("blob origin = %q", got)
	}
	p := NewURLSearchParamsString("b=2&a=1&b=3&c=&d&=e&a=4")
	if got := p.String(); got != "b=2&a=1&b=3&c=&d=&=e&a=4" {
		t.Errorf("toString = %q", got)
	}
	p.Set("b", "new")
	p.Sort()
	if got := p.String(); got != "=e&a=1&a=4&b=new&c=&d=" {
		t.Errorf("after set and sort = %q", got)
	}
	enc := NewURLSearchParamsPairs("k e y", "a b+c&d=e%~!'()*-._é\xed\xa0\x80")
	if got := enc.String(); got != "k+e+y=a+b%2Bc%26d%3De%25%7E%21%27%28%29*-._%C3%A9%EF%BF%BD" {
		t.Errorf("form encoding = %q", got)
	}
}

// TestURLInvalidErrors: the TypeErrors Node throws, property by property
// (R39), and that the site given is the throw's.
func TestURLInvalidErrors(t *testing.T) {
	t.Parallel()
	site := &Site{File: "/x.ts", Line: 3, Col: 9}
	for _, c := range []struct {
		name            string
		f               func()
		str, json, keys string
	}{
		{"new URL", func() { NewURL("not a url", site) }, "TypeError: Invalid URL",
			`{"code":"ERR_INVALID_URL","input":"not a url"}`, "code input"},
		{"new URL with a base", func() { NewURLWithBase("x", "also bad", site) }, "TypeError: Invalid URL",
			`{"code":"ERR_INVALID_URL","input":"x","base":"also bad"}`, "code input base"},
		{"a lone surrogate, as a USVString", func() { NewURL("\xed\xa0\x80x", site) }, "TypeError: Invalid URL",
			`{"code":"ERR_INVALID_URL","input":"�x"}`, "code input"},
		{"the href setter", func() { NewURL("http://a/", nil).SetHref("x\xed\xa0\x80", site) },
			"TypeError [ERR_INVALID_URL]: Invalid URL", `{"code":"ERR_INVALID_URL","input":"x\ud800"}`, "code input"},
	} {
		th := thrownBy(c.f)
		if th == nil || th.Site != site {
			t.Errorf("%s: threw %v, not at the site given", c.name, th)
			continue
		}
		e := th.V.(*Error)
		var keys []string
		for _, p := range e.props {
			keys = append(keys, p.Key)
		}
		if got := ErrorToString(e); got != c.str || e.Kind() != ErrorType {
			t.Errorf("%s: String = %q, want %q", c.name, got, c.str)
		}
		if got := JSONStringify(e, JSONGap{}, nil); got != c.json {
			t.Errorf("%s: JSON = %s, want %s", c.name, got, c.json)
		}
		if got := strings.Join(keys, " "); got != c.keys {
			t.Errorf("%s: keys %q, want %q", c.name, got, c.keys)
		}
	}
}

// TestURLSearchParamsIsLive: one object, whatever sets the query (R41).
func TestURLSearchParamsIsLive(t *testing.T) {
	t.Parallel()
	u := NewURL("http://h/?a=%20b+c", nil)
	p := u.SearchParams()
	if v := p.Get("a"); v.IsNull() || v.Must("a") != " b c" {
		t.Errorf("get a = %v", v)
	}
	p.Append("z", "1")
	u.SetHref("http://n/?m=1", nil)
	u.SetSearch("?q=2")
	if u.SearchParams() != p || p.String() != "q=2" {
		t.Errorf("searchParams after href and search: %p %q", u.SearchParams(), p.String())
	}
	p.Delete("q")
	if u.Href() != "http://n/" {
		t.Errorf("emptied: %q", u.Href())
	}
	if !URLParse("nope").IsNull() || URLCanParse("nope") || !URLCanParseWithBase("x", "http://h/") {
		t.Error("URL.parse or URL.canParse")
	}
	if got := NewURLWithBaseURL("x", NewURL("http://b.test/a/", nil), nil).Href(); got != "http://b.test/a/x" {
		t.Errorf("a URL as the base: %q", got)
	}
}

func TestURLSearchParamsPairsMustBePairs(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("an odd number of strings didn't panic")
		}
	}()
	NewURLSearchParamsPairs("a")
}

// TestURLGoldenSectionsAllRun: every section the index lists is one a test
// runs, so a section added to the generator can't go unchecked.
func TestURLGoldenSectionsAllRun(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/url-goldens-index.json")
	if err != nil {
		t.Fatal(err)
	}
	var idx struct {
		Node     string                     `json:"node"`
		Sections map[string]json.RawMessage `json:"sections"`
	}
	if err := json.Unmarshal(raw, &idx); err != nil {
		t.Fatal(err)
	}
	run := []string{"MISC", "COMPONENTS", "HOSTS", "PATHS", "RELATIVE", "SETTERS", "PARAMS", "FORM", "LIVE", "INSPECT", "JSON"}
	for name := range acceptedURLDifferences {
		run = append(run, name)
	}
	for name := range idx.Sections {
		if !slices.Contains(run, name) {
			t.Errorf("section %s is run by no test", name)
		}
	}
	if len(idx.Sections) != len(run) || idx.Node != "v24.18.0" {
		t.Errorf("the index lists %d sections from Node %s, the tests run %d", len(idx.Sections), idx.Node, len(run))
	}
}
