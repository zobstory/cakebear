package runtime

import "unsafe"

// StrAcc is what the backend keeps beside a string binding the program
// appends to (M5 plan D13): `s += x` is `s = acc.Append(s, x)`. JavaScript
// engines make a loop of appends linear with ropes; a Go string is
// immutable, so StrConcat copies the whole string on every append, and a 10k
// append loop building a 1.2 MB page took 331 ms where Append takes 2.15 ms
// (spikes/m5-append/REPORT.md).
//
// The bytes of a string it handed out are never written again: an append
// writes only past the end of the newest one, so every string it made, and
// any prefix of one held elsewhere (a const copied from the binding and read
// by a spawn, say), keeps its value. Only the binding's own domain appends: a
// let never crosses domains (the race rule), and within a domain only the
// token holder runs, so StrAcc needs no atomics.
type StrAcc struct {
	buf []byte // len(buf) is the bytes the newest string covers
	tip *byte  // that string's first byte, or nil when there is none
	asc bool   // buf is all ASCII, so a code unit is a byte
}

// accMin is the shortest result kept in an accumulator. A shorter one is made
// by StrConcat, as before, so a binding appended to once or twice costs
// nothing extra.
const accMin = 256

// Append is s + parts..., as StrConcat makes it, extending s in place when
// it is still the newest string this accumulator made. Otherwise it starts
// again from a copy: another write to the binding (`s = ""`) leaves the
// accumulator behind, and its next append begins afresh.
func (a *StrAcc) Append(s string, parts ...string) string {
	total := len(s)
	for _, p := range parts {
		total += len(p)
	}
	if total < accMin || total > stringLimit || seamJoins(s, parts) {
		// A split surrogate pair to join and V8's length limit are
		// StrConcat's business, as for any other string.
		a.tip = nil
		var pieces [8]string // on the stack for up to 7 parts
		return StrConcat(append(append(pieces[:0], s), parts...)...)
	}
	if !a.newest(s) {
		a.buf = make([]byte, 0, 2*total)
		a.buf = append(a.buf, s...)
		a.asc = isASCII(s)
	}
	if total > cap(a.buf) {
		grown := make([]byte, len(a.buf), 2*total)
		copy(grown, a.buf)
		a.buf = grown
	}
	for _, p := range parts {
		if a.asc && !isASCII(p) {
			a.asc = false
		}
		a.buf = append(a.buf, p...)
	}
	a.tip = unsafe.SliceData(a.buf)
	return unsafe.String(a.tip, len(a.buf))
}

// Length is `s.length` for a binding with an accumulator: the byte count when
// s is the newest string and all of it is ASCII, without the scan StrLength
// makes. A loop reading `xml.length` as it appends took 2.45 s with the scan,
// and 2.1 ms with this.
func (a *StrAcc) Length(s string) float64 {
	if a.asc && a.newest(s) {
		return float64(len(s))
	}
	return StrLength(s)
}

// newest reports whether s is the string this accumulator made last.
func (a *StrAcc) newest(s string) bool {
	return a.tip != nil && unsafe.StringData(s) == a.tip && len(s) == len(a.buf)
}

// seamJoins is joinsHalves for s followed by parts, without making a slice.
func seamJoins(s string, parts []string) bool {
	high := s != "" && endsHigh(s)
	for _, p := range parts {
		if p != "" {
			if high && startsLow(p) {
				return true
			}
			high = endsHigh(p)
		}
	}
	return false
}
