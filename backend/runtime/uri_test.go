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
		if got := EncodeURIComponent(c.in); got != c.want {
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
		if got := DecodeURIComponent(c.in); got != c.want {
			t.Errorf("decodeURIComponent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestURIPanics: each panic, verbatim, where Node throws URIError: URI
// malformed.
func TestURIPanics(t *testing.T) {
	t.Parallel()

	const node = " (Node throws URIError: URI malformed)"
	for _, c := range []struct {
		name string
		call func()
		want string
	}{
		{"a lone high half", func() { EncodeURIComponent("a\xed\xa0\x80") }, "cakebear: encodeURIComponent cannot encode the lone surrogate U+D800" + node},
		{"a lone low half", func() { EncodeURIComponent("\xed\xb8\x80") }, "cakebear: encodeURIComponent cannot encode the lone surrogate U+DE00" + node},
		{"halves the wrong way round", func() { EncodeURIComponent("\xed\xb8\x80\xed\xa0\xbd") }, "cakebear: encodeURIComponent cannot encode the lone surrogate U+DE00" + node},
		{"two high halves", func() { EncodeURIComponent("\xed\xa0\xbd\xed\xa0\xbd") }, "cakebear: encodeURIComponent cannot encode the lone surrogate U+D83D" + node},
		{"a % at the end", func() { DecodeURIComponent("100%") }, `cakebear: decodeURIComponent cannot decode "%"` + node},
		{"one hex digit", func() { DecodeURIComponent("a%4") }, `cakebear: decodeURIComponent cannot decode "%4"` + node},
		{"not hex", func() { DecodeURIComponent("%ZZtop") }, `cakebear: decodeURIComponent cannot decode "%ZZ"` + node},
		{"not hex, cut in a character", func() { DecodeURIComponent("%4é") }, `cakebear: decodeURIComponent cannot decode "%4é"` + node},
		{"a continuation byte first", func() { DecodeURIComponent("%80") }, `cakebear: decodeURIComponent cannot decode "%80"` + node},
		{"a sequence cut short", func() { DecodeURIComponent("%E2%82") }, `cakebear: decodeURIComponent cannot decode "%E2%82"` + node},
		{"a sequence broken off", func() { DecodeURIComponent("%E2%82A") }, `cakebear: decodeURIComponent cannot decode "%E2%82A"` + node},
		{"an overlong form", func() { DecodeURIComponent("%C0%80") }, `cakebear: decodeURIComponent cannot decode "%C0%80"` + node},
		{"a surrogate", func() { DecodeURIComponent("%ED%A0%80") }, `cakebear: decodeURIComponent cannot decode "%ED%A0%80"` + node},
		{"past U+10FFFF", func() { DecodeURIComponent("%F4%90%80%80") }, `cakebear: decodeURIComponent cannot decode "%F4%90%80%80"` + node},
		{"a five-byte lead", func() { DecodeURIComponent("%F8%88%80%80%80") }, `cakebear: decodeURIComponent cannot decode "%F8"` + node},
	} {
		func() {
			defer func() {
				if got := recover(); got != c.want {
					t.Errorf("%s: panicked with %v, want %q", c.name, got, c.want)
				}
			}()
			c.call()
		}()
	}
}
