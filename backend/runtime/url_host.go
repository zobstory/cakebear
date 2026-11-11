package runtime

import (
	"strconv"
	"strings"
)

// The URL Standard's host parser and serializer
// (https://url.spec.whatwg.org/#host-parsing): a domain, an IPv4 address in
// any of the forms the standard reads (hex, octal, fewer than four parts),
// an IPv6 address, or for a URL that isn't special an opaque host. A host
// is kept as it serializes: IPv4 dotted, IPv6 compressed and in brackets.

// parseHost is the host parser. input is the host as written, a USVString;
// special is whether the URL's scheme is special, which makes the host a
// domain, where any other's is opaque. It reports false for a failure.
func parseHost(input string, special bool) (string, bool) {
	if strings.HasPrefix(input, "[") {
		if !strings.HasSuffix(input, "]") || len(input) < 2 {
			return "", false
		}
		addr, ok := parseIPv6(input[1 : len(input)-1])
		if !ok {
			return "", false
		}
		return "[" + serializeIPv6(addr) + "]", true
	}
	if !special {
		if forbiddenHost(input, false) {
			return "", false
		}
		return encodeString(input, setC0), true
	}
	ascii, ok := domainToASCII(utf8DecodeWithoutBOM(percentDecode(input)))
	if !ok {
		return "", false
	}
	if endsInANumber(ascii) {
		addr, ok := parseIPv4(ascii)
		if !ok {
			return "", false
		}
		return serializeIPv4(addr), true
	}
	return ascii, true
}

// forbiddenHost reports whether s holds a forbidden host code point, or
// with domain a forbidden domain code point, which adds the C0 controls,
// `%` and U+007F.
func forbiddenHost(s string, domain bool) bool {
	for i := range len(s) {
		switch c := s[i]; c {
		case 0, '\t', '\n', '\r', ' ', '#', '/', ':', '<', '>', '?', '@', '[', '\\', ']', '^', '|':
			return true
		default:
			if domain && (c < 0x20 || c == '%' || c == 0x7f) {
				return true
			}
		}
	}
	return false
}

// endsInANumber is the standard's ends-in-a-number checker: whether the
// last label, a trailing empty one aside, is all digits or an IPv4 number,
// which makes the host an IPv4 address or a failure.
func endsInANumber(s string) bool {
	parts := strings.Split(s, ".")
	if parts[len(parts)-1] == "" {
		if len(parts) == 1 {
			return false
		}
		parts = parts[:len(parts)-1]
	}
	last := parts[len(parts)-1]
	if last != "" && strings.Trim(last, "0123456789") == "" {
		return true
	}
	_, ok := parseIPv4Number(last)
	return ok
}

// ipv4Overflow is a value no part of an address may reach: a part past it
// fails whatever its place, so parsing stops counting there.
const ipv4Overflow = 1 << 40

// parseIPv4Number is the IPv4 number parser: decimal, hex after 0x or 0X,
// octal after a 0, and 0 for a bare 0x.
func parseIPv4Number(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	radix := uint64(10)
	switch {
	case len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X'):
		s, radix = s[2:], 16
	case len(s) >= 2 && s[0] == '0':
		s, radix = s[1:], 8
	}
	n := uint64(0)
	for i := range len(s) {
		c := s[i]
		var d uint64
		switch {
		case c >= '0' && c <= '9':
			d = uint64(c - '0')
		case radix == 16 && isASCIIHex(c):
			d = uint64(hexValue(c))
		default:
			return 0, false
		}
		if d >= radix {
			return 0, false
		}
		n = min(n*radix+d, ipv4Overflow)
	}
	return n, true
}

// parseIPv4 is the IPv4 parser: up to four parts, a trailing empty one
// dropped, each but the last at most 255, and the last filling the bytes
// the others leave.
func parseIPv4(s string) (uint32, bool) {
	parts := strings.Split(s, ".")
	if parts[len(parts)-1] == "" && len(parts) > 1 {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 4 {
		return 0, false
	}
	numbers := make([]uint64, len(parts))
	for i, part := range parts {
		n, ok := parseIPv4Number(part)
		if !ok {
			return 0, false
		}
		numbers[i] = n
	}
	last := len(numbers) - 1
	for _, n := range numbers[:last] {
		if n > 255 {
			return 0, false
		}
	}
	if numbers[last] >= 1<<(8*(5-len(numbers))) {
		return 0, false
	}
	addr := numbers[last]
	for i, n := range numbers[:last] {
		addr += n << (8 * (3 - i))
	}
	return uint32(addr), true
}

func serializeIPv4(addr uint32) string {
	b := make([]byte, 0, 15)
	for i := 3; i >= 0; i-- {
		b = strconv.AppendUint(b, uint64(addr>>(8*i)&0xff), 10)
		if i > 0 {
			b = append(b, '.')
		}
	}
	return string(b)
}

// parseIPv6 is the IPv6 parser: eight pieces of up to four hex digits, one
// run of them compressed to `::`, and the last two perhaps an IPv4 address
// in dotted decimal.
func parseIPv6(s string) ([8]uint16, bool) {
	var addr [8]uint16
	at := func(i int) int {
		if i < len(s) {
			return int(s[i])
		}
		return -1 // EOF; a byte from 0x80 up fails as a code point would
	}
	isDigit := func(c int) bool { return c >= '0' && c <= '9' }
	piece, compress, p := 0, -1, 0
	if at(p) == ':' {
		if at(p+1) != ':' {
			return addr, false
		}
		p += 2
		piece++
		compress = piece
	}
	for at(p) != -1 {
		if piece == 8 {
			return addr, false
		}
		if at(p) == ':' {
			if compress != -1 {
				return addr, false
			}
			p++
			piece++
			compress = piece
			continue
		}
		value, length := 0, 0
		for length < 4 && at(p) != -1 && isASCIIHex(byte(at(p))) {
			value = value*16 + int(hexValue(byte(at(p))))
			p++
			length++
		}
		if at(p) == '.' {
			if length == 0 || piece > 6 {
				return addr, false
			}
			p -= length
			seen := 0
			for at(p) != -1 {
				if seen > 0 {
					if at(p) != '.' || seen >= 4 {
						return addr, false
					}
					p++
				}
				if !isDigit(at(p)) {
					return addr, false
				}
				part := -1
				for isDigit(at(p)) {
					d := at(p) - '0'
					switch part {
					case -1:
						part = d
					case 0:
						return addr, false
					default:
						part = part*10 + d
					}
					if part > 255 {
						return addr, false
					}
					p++
				}
				addr[piece] = addr[piece]<<8 | uint16(part)
				seen++
				if seen == 2 || seen == 4 {
					piece++
				}
			}
			if seen != 4 {
				return addr, false
			}
			break
		}
		if at(p) == ':' {
			p++
			if at(p) == -1 {
				return addr, false
			}
		} else if at(p) != -1 {
			return addr, false
		}
		addr[piece] = uint16(value)
		piece++
	}
	if compress != -1 {
		swaps := piece - compress
		for piece = 7; piece != 0 && swaps > 0; piece, swaps = piece-1, swaps-1 {
			addr[piece], addr[compress+swaps-1] = addr[compress+swaps-1], addr[piece]
		}
	} else if piece != 8 {
		return addr, false
	}
	return addr, true
}

// serializeIPv6 writes each piece in lower-case hex without leading zeros,
// and the first of the longest runs of two or more zero pieces as `::`.
func serializeIPv6(addr [8]uint16) string {
	compress, best := -1, 1
	for i := 0; i < 8; {
		if addr[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && addr[j] == 0 {
			j++
		}
		if j-i > best {
			compress, best = i, j-i
		}
		i = j
	}
	b := make([]byte, 0, 39)
	for i := 0; i < 8; i++ {
		if i == compress {
			if i == 0 {
				b = append(b, ':')
			}
			b = append(b, ':')
			i += best - 1
			continue
		}
		b = strconv.AppendUint(b, uint64(addr[i]), 16)
		if i != 7 {
			b = append(b, ':')
		}
	}
	return string(b)
}
