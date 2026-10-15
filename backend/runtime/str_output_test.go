package runtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// console.log writes a lone surrogate as U+FFFD, as Node's stdout does, and
// two halves that pair as their character (measured on Node 24.18: the bytes
// of console.log("a\uD800b", "\uDC00") are 61 ef bf bd 62 20 ef bf bd 0a).
func TestLogWritesWellFormed(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	stdout, _, _ := isolate(t)
	LogString("a" + "\xed\xa0\x80" + "b")
	Log("a\xed\xa0\x80b", "\xed\xb0\x80")
	Log("x", NewArray("\xed\xa0\x80"))
	LogString(highHalf + lowHalf)
	Flush()
	want := "a\uFFFDb\na\uFFFDb \uFFFD\nx [ '\\ud800' ]\n😀\n"
	if got := stdout.String(); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestWellFormed(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"":                            "",
		"plain":                       "plain",
		"\uD55C\uAE00":                "\uD55C\uAE00", // 0xED that starts a real character
		"\xed\xa0\x80":                "\uFFFD",
		"a\xed\xbf\xbfb":              "a\uFFFDb",
		highHalf + lowHalf:            "😀",
		lowHalf + highHalf:            "\uFFFD\uFFFD",
		highHalf + highHalf + lowHalf: "\uFFFD😀",
	} {
		if got := wellFormed(in); got != want {
			t.Errorf("wellFormed(% x) = % x, want % x", in, got, want)
		}
	}
}

// res.end writes a lone surrogate as U+FFFD, as Node does (measured:
// res.end('a\uD800b\uDC00') sends 61 ef bf bd 62 ef bf bd, Content-Length 8).
func TestResponseBodyIsWellFormed(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	isolate(t)
	url := serveWith(t, func(_ *IncomingMessage, res *ServerResponse) {
		res.SetHeader("x-latin1", "caf\u00E9\u00FF\u0080").End("a\xed\xa0\x80b\xed\xb0\x80")
	})
	r := fetch(http.DefaultClient, "GET", url)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.body != "a\uFFFDb\uFFFD" || r.length != 8 {
		t.Errorf("body % x, length %d; want % x, length 8", r.body, r.length, "a\uFFFDb\uFFFD")
	}
	// Node sends U+0080 to U+00FF in a header as UTF-8, as net/http does.
	if got := r.header.Get("x-latin1"); got != "caf\u00E9\u00FF\u0080" {
		t.Errorf("x-latin1 = % x", got)
	}
}

// setHeader throws in Node for a name that isn't a token and for a value
// with a character it can't send; each message here was measured on Node
// 24.18. A header already sent is reported first.
func TestSetHeaderValidatesLikeNode(t *testing.T) {
	t.Parallel()

	fresh := func() *ServerResponse { return &ServerResponse{w: httptest.NewRecorder(), status: 200} }
	for _, c := range []struct {
		name, header, value string
		want                string // "" means it must not panic
	}{
		{"space in name", "x y", "v", `TypeError [ERR_INVALID_HTTP_TOKEN]: Header name must be a valid HTTP token ["x y"]`},
		{"bad name and value", "x y", "\xed\xa0\x80", `TypeError [ERR_INVALID_HTTP_TOKEN]: Header name must be a valid HTTP token ["x y"]`},
		{"empty name", "", "v", `TypeError [ERR_INVALID_HTTP_TOKEN]: Header name must be a valid HTTP token [""]`},
		{"lone surrogate in name", "x-\xed\xa0\x80", "v", "TypeError [ERR_INVALID_HTTP_TOKEN]: Header name must be a valid HTTP token [\"x-\uFFFD\"]"},
		{"colon in name", "x:y", "v", `TypeError [ERR_INVALID_HTTP_TOKEN]: Header name must be a valid HTTP token ["x:y"]`},
		{"lone surrogate in value", "x-lone", "a\xed\xa0\x80b", `TypeError [ERR_INVALID_CHAR]: Invalid character in header content ["x-lone"]`},
		{"past U+00FF", "x-100", "\u0100", `TypeError [ERR_INVALID_CHAR]: Invalid character in header content ["x-100"]`},
		{"a pair", "x-pair", "😀", `TypeError [ERR_INVALID_CHAR]: Invalid character in header content ["x-pair"]`},
		{"control", "x-1f", "\x1f", `TypeError [ERR_INVALID_CHAR]: Invalid character in header content ["x-1f"]`},
		{"newline", "x-nl", "a\nb", `TypeError [ERR_INVALID_CHAR]: Invalid character in header content ["x-nl"]`},
		{"DEL", "x-del", "a\x7fb", `TypeError [ERR_INVALID_CHAR]: Invalid character in header content ["x-del"]`},
		{"every token character", "!#$%&'*+-.^_`|~09azAZ", "v", ""},
		{"empty value", "x-empty", "", ""},
		{"tab and spaces", "x-tab", " a\tb ", ""},
		{"U+00FF", "x-ff", "\u00FF\u0080", ""},
	} {
		got := panicked(func() { fresh().SetHeader(c.header, c.value) })
		if got != c.want {
			t.Errorf("%s: panic = %q, want %q", c.name, got, c.want)
		}
	}
	late := fresh()
	late.WriteHead(200)
	if got := panicked(func() { late.SetHeader("x y", "\xed\xa0\x80") }); !strings.Contains(got, "ERR_HTTP_HEADERS_SENT") {
		t.Errorf("after the head, panic = %q, want ERR_HTTP_HEADERS_SENT first", got)
	}
}
