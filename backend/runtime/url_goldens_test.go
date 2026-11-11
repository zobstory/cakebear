package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

// The URL goldens (testdata/gen-url-goldens.mjs): what Node 24.18's URL and
// URLSearchParams make of 18,346 cases, which the runtime is held to byte
// for byte, and the IDNA section, whose differences are accepted (S1 plan
// D6) but may not grow.

type urlGolden struct {
	K     string              `json:"k"`
	In    *goldenText         `json:"in"`
	Base  *goldenText         `json:"base"`
	BU    bool                `json:"bu"`
	R     []goldenText        `json:"r"`
	Err   *goldenText         `json:"err"`
	Op    string              `json:"op"`
	V     json.RawMessage     `json:"v"`
	Init  *goldenText         `json:"init"`
	Pairs []goldenText        `json:"pairs"`
	Ops   [][]json.RawMessage `json:"ops"`
	Out   json.RawMessage     `json:"out"`
	Out2  *goldenText         `json:"out2"`
	Ser   *goldenText         `json:"ser"`
	Dec   []goldenText        `json:"dec"`
	Round *goldenText         `json:"round"`
	line  int                 // 1-based
}

type urlCorpus struct {
	sections map[string][]urlGolden
	order    []string
	err      error
}

var loadURLCorpus = sync.OnceValue(func() *urlCorpus {
	c := &urlCorpus{sections: map[string][]urlGolden{}}
	raw, err := os.ReadFile("testdata/url-goldens-index.json")
	if err != nil {
		c.err = err
		return c
	}
	var idx struct {
		Sections map[string]struct {
			First int `json:"first"`
			Last  int `json:"last"`
			Count int `json:"count"`
		} `json:"sections"`
	}
	if c.err = json.Unmarshal(raw, &idx); c.err != nil {
		return c
	}
	f, err := os.Open("testdata/url-goldens.jsonl")
	if err != nil {
		c.err = err
		return c
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for n := 1; sc.Scan(); n++ {
		var g urlGolden
		if c.err = json.Unmarshal(sc.Bytes(), &g); c.err != nil {
			return c
		}
		g.line = n
		for name, s := range idx.Sections {
			if n >= s.First && n <= s.Last {
				c.sections[name] = append(c.sections[name], g)
			}
		}
	}
	for name, s := range idx.Sections {
		if len(c.sections[name]) != s.Count || s.Count == 0 {
			c.err = fmt.Errorf("section %s has %d lines, the index says %d", name, len(c.sections[name]), s.Count)
		}
		c.order = append(c.order, name)
	}
	if c.err == nil {
		c.err = sc.Err()
	}
	return c
})

// urlRecordOf is the eleven getters and searchParams.toString(), as the
// goldens record them.
func urlRecordOf(u *URL) string {
	return strings.Join([]string{u.Href(), u.Origin(), u.Protocol(), u.Username(), u.Password(), u.Host(),
		u.Hostname(), u.Port(), u.Pathname(), u.Search(), u.Hash(), u.SearchParams().String()}, "\x1f")
}

func wantRecord(g urlGolden) string {
	if g.Err != nil {
		return "throws " + string(*g.Err)
	}
	parts := make([]string, len(g.R))
	for i, s := range g.R {
		parts[i] = string(s)
	}
	return strings.Join(parts, "\x1f")
}

// urlThrows runs f, and gives what it threw as Node prints it, or "".
func urlThrows(f func()) (printed string) {
	defer func() {
		if p := recover(); p != nil {
			t, ok := p.(*Thrown)
			if !ok {
				panic(p)
			}
			printed = "throws " + Inspect(t.V)
		}
	}()
	f()
	return ""
}

// runURLSections holds every golden of the sections named to Node's
// output, reporting the first 20 that differ.
func runURLSections(t *testing.T, names ...string) {
	t.Helper()
	c := loadURLCorpus()
	if c.err != nil {
		t.Fatal(c.err)
	}
	for _, name := range names {
		cases := c.sections[name]
		bad := 0
		for _, g := range cases {
			if got, want := g.check(); got != want {
				if bad++; bad <= 20 {
					t.Errorf("%s, line %d (%s %q):\n got  %q\n want %q", name, g.line, g.K, describeInput(g), got, want)
				}
			}
		}
		if bad > 0 {
			t.Errorf("%s: %d of %d differ from Node's", name, bad, len(cases))
		}
	}
}

func describeInput(g urlGolden) string {
	switch {
	case g.In != nil && g.Base != nil:
		return string(*g.In) + " against " + string(*g.Base)
	case g.In != nil:
		return string(*g.In)
	case g.Base != nil:
		return string(*g.Base) + " " + g.Op + " = " + string(g.V)
	}
	return string(g.V)
}

func TestURLParseGoldens(t *testing.T) {
	t.Parallel()
	runURLSections(t, "MISC", "COMPONENTS", "HOSTS", "PATHS", "RELATIVE")
}

func TestURLSetterGoldens(t *testing.T) {
	t.Parallel()
	runURLSections(t, "SETTERS")
}

func TestURLSearchParamsGoldens(t *testing.T) {
	t.Parallel()
	runURLSections(t, "PARAMS", "FORM", "LIVE")
}

func TestURLPrintGoldens(t *testing.T) {
	t.Parallel()
	runURLSections(t, "INSPECT", "JSON")
}

// The accepted differences: how many goldens of each section differ from
// Node's. IDNA's are for want of UTS 46's tables (S1 plan D6): its mapping
// (`ﬁ`, `Ⅻ`, `ⓐ` to ASCII), NFC, the bidi rule, and the validity of what an
// `xn--` label decodes to. INCONSISTENT's are setters after which Node's
// href parses to another URL, its parser's bug (`host = "b:99"` on
// `sc:/.//p` gives `sc://b:99/.//p`, whose host reads `b:99/.`), where the
// runtime gives what the standard does. A change that makes more differ
// fails; one that makes fewer should lower the count.
var acceptedURLDifferences = map[string]int{"IDNA": 202, "INCONSISTENT": 11}

// TestURLAcceptedDifferences lists those differences, run with -v, and
// holds every URL INCONSISTENT leaves to parsing back to itself.
func TestURLAcceptedDifferences(t *testing.T) {
	t.Parallel()
	c := loadURLCorpus()
	if c.err != nil {
		t.Fatal(c.err)
	}
	for name, accepted := range acceptedURLDifferences {
		bad := 0
		for _, g := range c.sections[name] {
			if got, want := g.check(); got != want {
				bad++
				t.Logf("accepted difference, %s line %d (%s %q):\n got  %q\n want %q", name, g.line, g.K, describeInput(g), got, want)
			}
			if name == "INCONSISTENT" {
				u := NewURL(string(*g.Base), nil)
				applySetter(u, g.Op, text(g.V))
				if again := NewURL(u.Href(), nil).Href(); again != u.Href() {
					t.Errorf("line %d: %q parses back to %q", g.line, u.Href(), again)
				}
			}
		}
		t.Logf("%s: %d of %d differ from Node's", name, bad, len(c.sections[name]))
		if bad > accepted {
			t.Errorf("%s: %d differ from Node's, more than the %d accepted", name, bad, accepted)
		}
	}
}
