package runtime

import "testing"

// TestURIKeyOutputs: what each function leaves alone, what it escapes, and
// how, recorded from Node 24.18.
func TestURIKeyOutputs(t *testing.T) {
	t.Parallel()

	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"AZaz09-_.!~*'()", "AZaz09-_.!~*'()"},
		{"a b+c=d&e/f?g#h%", "a%20b%2Bc%3Dd%26e%2Ff%3Fg%23h%25"},
		{"\x00\x7f", "%00%7F"},
		{"café", "caf%C3%A9"},
		{"€", "%E2%82%AC"},
		{"😀", "%F0%9F%98%80"},
		{"\U0010ffff", "%F4%8F%BF%BF"},
		{"\ufeff", "%EF%BB%BF"},
		// Two halves of a pair, as concatenation can leave them, are the
		// code point they make.
		{"\xed\xa0\xbd\xed\xb8\x80", "%F0%9F%98%80"},
		{"x\xed\xaf\xbf\xed\xbf\xbfy", "x%F4%8F%BF%BFy"},
	} {
		if got := EncodeURIComponent(c.in, nil); got != c.want {
			t.Errorf("encodeURIComponent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"a+b", "a+b"},
		{"%25%2F%3f%23", "%/?#"},
		{"caf%c3%A9", "café"},
		{"%F0%9F%98%80", "😀"},
		{"%EF%BF%BD", "�"},
		{"%00", "\x00"},
		// A lone surrogate isn't an escape, so it is kept.
		{"\xed\xa0\x80%41", "\xed\xa0\x80A"},
		{"日本%E8%AA%9E", "日本語"},
	} {
		if got := DecodeURIComponent(c.in, nil); got != c.want {
			t.Errorf("decodeURIComponent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestURIThrows: each throws the URIError Node throws, URI malformed, which
// a try can catch (M6 plan D9).
func TestURIThrows(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		call func()
		want string
	}{
		{"a lone high half", func() { EncodeURIComponent("a\xed\xa0\x80", nil) }, "URIError: URI malformed"},
		{"a lone low half", func() { EncodeURIComponent("\xed\xb8\x80", nil) }, "URIError: URI malformed"},
		{"halves the wrong way round", func() { EncodeURIComponent("\xed\xb8\x80\xed\xa0\xbd", nil) }, "URIError: URI malformed"},
		{"two high halves", func() { EncodeURIComponent("\xed\xa0\xbd\xed\xa0\xbd", nil) }, "URIError: URI malformed"},
		{"a % at the end", func() { DecodeURIComponent("100%", nil) }, "URIError: URI malformed"},
		{"one hex digit", func() { DecodeURIComponent("a%4", nil) }, "URIError: URI malformed"},
		{"not hex", func() { DecodeURIComponent("%ZZtop", nil) }, "URIError: URI malformed"},
		{"not hex, cut in a character", func() { DecodeURIComponent("%4é", nil) }, "URIError: URI malformed"},
		{"a continuation byte first", func() { DecodeURIComponent("%80", nil) }, "URIError: URI malformed"},
		{"a sequence cut short", func() { DecodeURIComponent("%E2%82", nil) }, "URIError: URI malformed"},
		{"a sequence broken off", func() { DecodeURIComponent("%E2%82A", nil) }, "URIError: URI malformed"},
		{"an overlong form", func() { DecodeURIComponent("%C0%80", nil) }, "URIError: URI malformed"},
		{"a surrogate", func() { DecodeURIComponent("%ED%A0%80", nil) }, "URIError: URI malformed"},
		{"past U+10FFFF", func() { DecodeURIComponent("%F4%90%80%80", nil) }, "URIError: URI malformed"},
		{"a five-byte lead", func() { DecodeURIComponent("%F8%88%80%80%80", nil) }, "URIError: URI malformed"},
	} {
		if got := throws(c.call); got != c.want {
			t.Errorf("%s: %s, want a throw of %s", c.name, got, c.want)
		}
	}
}
