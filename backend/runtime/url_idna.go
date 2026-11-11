package runtime

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// A domain's ASCII form, the URL Standard's domain to ASCII
// (https://url.spec.whatwg.org/#concept-domain-to-ascii), without UTS 46's
// tables (S1 plan D6). An ASCII domain with no label starting `xn--` is
// lowercased, which is exactly what the standard gives it. Any other is
// lowercased and punycoded label by label (RFC 3492), and an `xn--` label
// is decoded to check it, where UTS 46 would also map, normalise and check
// each code point against Unicode's tables. What it maps here is a few
// hand-written ranges: the dots that separate labels, the code points it
// ignores and the fullwidth forms of ASCII. Where the rest makes Node
// differ, it is an accepted difference (the IDNA section of the goldens).

// domainToASCII is domain to ASCII with beStrict false, then the checks
// the host parser makes of its result.
func domainToASCII(domain string) (string, bool) {
	result := ""
	if isASCII(domain) && !hasPunycodeLabel(domain) {
		result = strings.ToLower(domain)
	} else {
		var ok bool
		if result, ok = uts46ToASCII(domain); !ok {
			return "", false
		}
	}
	if result == "" || forbiddenHost(result, true) {
		return "", false
	}
	return result, true
}

func hasPunycodeLabel(domain string) bool {
	for label := range strings.SplitSeq(domain, ".") {
		if len(label) >= 4 && strings.EqualFold(label[:4], "xn--") {
			return true
		}
	}
	return false
}

// uts46ToASCII is UTS 46's ToASCII, as far as it goes without the tables:
// mapped, then each label checked and punycoded.
func uts46ToASCII(domain string) (string, bool) {
	var mapped []rune
	for _, r := range domain {
		switch {
		case r == 0x3002 || r == 0xff0e || r == 0xff61:
			r = '.'
		case idnaIgnored(r):
			continue
		case r >= 0xff01 && r <= 0xff5e:
			r -= 0xff01 - '!' // a fullwidth form of ASCII is that ASCII
		}
		mapped = append(mapped, unicode.ToLower(r))
	}
	var b strings.Builder
	start := 0
	for i := 0; i <= len(mapped); i++ {
		if i < len(mapped) && mapped[i] != '.' {
			continue
		}
		label := mapped[start:i]
		if i > start {
			if !idnaLabel(label, &b) {
				return "", false
			}
		}
		if i < len(mapped) {
			b.WriteByte('.')
		}
		start = i + 1
	}
	return b.String(), true
}

// idnaLabel writes label's ASCII form to b: as it is when it is ASCII,
// after checking an `xn--` one decodes to a label it could be the form of,
// and punycoded otherwise.
func idnaLabel(label []rune, b *strings.Builder) bool {
	ascii := true
	for _, r := range label {
		if r >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		s := string(label)
		if strings.HasPrefix(s, "xn--") {
			decoded, ok := punycodeDecode(s[4:])
			if !ok || !idnaValid(decoded, true) {
				return false
			}
		}
		b.WriteString(s)
		return true
	}
	if !idnaValid(label, false) {
		return false
	}
	b.WriteString("xn--")
	b.WriteString(punycodeEncode(label))
	return true
}

// idnaValid is UTS 46's validity criteria for a label, as far as Go's
// standard library can tell them: no code point it disallows by its
// category, nor a mark first, nor `xn--` again; and for a label decoded
// from punycode, non-empty, not all ASCII, and holding nothing mapping
// would have changed.
func idnaValid(label []rune, decoded bool) bool {
	if len(label) == 0 {
		return !decoded
	}
	if unicode.Is(unicode.M, label[0]) {
		return false
	}
	if len(label) >= 4 && string(label[:4]) == "xn--" {
		return false
	}
	ascii := true
	for _, r := range label {
		if r >= utf8.RuneSelf {
			ascii = false
		}
		if idnaDisallowed(r) {
			return false
		}
		if decoded && (unicode.ToLower(r) != r || idnaIgnored(r) || r == 0x3002 || r == 0xff0e || r == 0xff61 || r >= 0xff01 && r <= 0xff5e) {
			return false
		}
	}
	return !decoded || !ascii
}

// idnaIgnored is whether UTS 46 maps r to nothing: the soft hyphen, the
// combining grapheme joiner, the Mongolian free variation selectors, the
// zero width space, word joiner and invisible plus, the variation
// selectors and the byte order mark.
func idnaIgnored(r rune) bool {
	switch {
	case r == 0xad, r == 0x34f, r >= 0x180b && r <= 0x180d, r == 0x180f, r == 0x200b, r == 0x2060, r == 0x2064,
		r >= 0xfe00 && r <= 0xfe0f, r == 0xfeff, r >= 0x1bca0 && r <= 0x1bca3, r >= 0xe0100 && r <= 0xe01ef:
		return true
	}
	return false
}

// idnaDisallowed is whether UTS 46 disallows r, by category: controls,
// format characters (the joiners too, which CheckJoiners refuses outside
// a few scripts), private use, the spaces and separators, the replacement
// character, noncharacters, and anything Go's tables don't assign.
func idnaDisallowed(r rune) bool {
	switch {
	case r < utf8.RuneSelf:
		return false
	case r == utf8.RuneError, r >= 0xfdd0 && r <= 0xfdef, r&0xfffe == 0xfffe:
		return true
	case unicode.In(r, unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs, unicode.Zs, unicode.Zl, unicode.Zp):
		return true
	}
	return !unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S)
}

// RFC 3492's parameters for IDNA.
const (
	punyBase        = 36
	punyTMin        = 1
	punyTMax        = 26
	punySkew        = 38
	punyDamp        = 700
	punyInitialBias = 72
	punyInitialN    = 128
	punyMaxInt      = 1<<31 - 1
)

func punyAdapt(delta, points int, first bool) int {
	if first {
		delta /= punyDamp
	} else {
		delta /= 2
	}
	delta += delta / points
	k := 0
	for delta > (punyBase-punyTMin)*punyTMax/2 {
		delta /= punyBase - punyTMin
		k += punyBase
	}
	return k + (punyBase-punyTMin+1)*delta/(delta+punySkew)
}

func punyThreshold(k, bias int) int {
	return min(max(k-bias, punyTMin), punyTMax)
}

func punyDigit(d int) byte {
	if d < 26 {
		return byte('a' + d)
	}
	return byte('0' + d - 26)
}

// punycodeEncode is RFC 3492's encoding of label, without the `xn--`.
func punycodeEncode(label []rune) string {
	var out []byte
	for _, r := range label {
		if r < utf8.RuneSelf {
			out = append(out, byte(r))
		}
	}
	basic := len(out)
	if basic > 0 {
		out = append(out, '-')
	}
	n, delta, bias := punyInitialN, 0, punyInitialBias
	for h := basic; h < len(label); {
		m := int(unicode.MaxRune) + 1
		for _, r := range label {
			if int(r) >= n && int(r) < m {
				m = int(r)
			}
		}
		delta += (m - n) * (h + 1)
		n = m
		for _, r := range label {
			if int(r) < n {
				delta++
			}
			if int(r) != n {
				continue
			}
			q := delta
			for k := punyBase; ; k += punyBase {
				t := punyThreshold(k, bias)
				if q < t {
					break
				}
				out = append(out, punyDigit(t+(q-t)%(punyBase-t)))
				q = (q - t) / (punyBase - t)
			}
			out = append(out, punyDigit(q))
			bias = punyAdapt(delta, h+1, h == basic)
			delta = 0
			h++
		}
		delta++
		n++
	}
	return string(out)
}

// punycodeDecode is RFC 3492's decoding of an ASCII label without its
// `xn--`, failing as the RFC does on a bad digit, an overflow, or a code
// point that isn't one.
func punycodeDecode(s string) ([]rune, bool) {
	var out []rune
	in := 0
	if d := strings.LastIndexByte(s, '-'); d >= 0 {
		for i := range d {
			out = append(out, rune(s[i]))
		}
		in = d + 1
	}
	n, i, bias := punyInitialN, 0, punyInitialBias
	for in < len(s) {
		oldi, w := i, 1
		for k := punyBase; ; k += punyBase {
			if in >= len(s) {
				return nil, false
			}
			c := s[in]
			in++
			digit := -1
			switch {
			case c >= 'a' && c <= 'z':
				digit = int(c - 'a')
			case c >= 'A' && c <= 'Z':
				digit = int(c - 'A')
			case c >= '0' && c <= '9':
				digit = int(c-'0') + 26
			}
			if digit < 0 || digit > (punyMaxInt-i)/w {
				return nil, false
			}
			i += digit * w
			t := punyThreshold(k, bias)
			if digit < t {
				break
			}
			if w > punyMaxInt/(punyBase-t) {
				return nil, false
			}
			w *= punyBase - t
		}
		points := len(out) + 1
		bias = punyAdapt(i-oldi, points, oldi == 0)
		if i/points > punyMaxInt-n {
			return nil, false
		}
		n += i / points
		i %= points
		if n > unicode.MaxRune || n >= 0xd800 && n <= 0xdfff {
			return nil, false
		}
		out = append(out, 0)
		copy(out[i+1:], out[i:])
		out[i] = rune(n)
		i++
	}
	return out, true
}
