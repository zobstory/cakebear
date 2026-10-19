package runtime

import (
	"fmt"
	"math"
)

// The string members that may be undefined (M5 plan D9): at, codePointAt
// and s[i], each in code units, as the rest of str*.go is.

// StrAt is `s.at(pos)`: the code unit at ToIntegerOrInfinity(pos), counted
// back from the end when negative, or undefined outside the string.
func StrAt(s string, pos float64) Maybe[string] {
	i := toInteger(pos)
	if i < 0 {
		i += float64(utf16Len(s))
	}
	return unitString(s, i)
}

// StrCodePointAt is `s.codePointAt(pos)`: the code point at code unit pos,
// or undefined outside the string. Unlike at, a negative pos counts from
// nowhere: it is outside. A pair's lead unit gives the pair's code point,
// and its trail unit, like a lone half, its own value.
func StrCodePointAt(s string, pos float64) Maybe[float64] {
	i := toInteger(pos)
	if i < 0 || i >= float64(len(s)) { // past the bytes is past the units
		return Maybe[float64]{}
	}
	n := int(i)
	if isASCII(s[:n+1]) {
		return Some(float64(s[n]))
	}
	first, ok := unitAt(s, n)
	if !ok {
		return Maybe[float64]{}
	}
	if first >= 0xd800 && first <= 0xdbff {
		if second, ok := unitAt(s, n+1); ok && second >= 0xdc00 && second <= 0xdfff {
			return Some(float64(0x10000 + (rune(first)-0xd800)<<10 + rune(second) - 0xdc00))
		}
	}
	return Some(float64(first))
}

// StrIndexOrUndefined is `s[i]` where the read may be undefined. Only an
// integer index inside the string reads a code unit: any other number is
// the name of a property a string doesn't have, `s["1.5"]` or `s["-1"]`.
// -0 is the property "0", as in Node.
func StrIndexOrUndefined(s string, i float64) Maybe[string] {
	if !stringIndex(i) {
		return Maybe[string]{}
	}
	return unitString(s, i)
}

// StrIndex is `s[i]` typed string, which TypeScript reads without checking
// the index, as it does an array's. Outside the string, it panics, saying
// Node reads undefined (M4 plan D8, as Array.Get).
func StrIndex(s string, i float64) string {
	if c := StrIndexOrUndefined(s, i); c.state == stateValue {
		return c.v
	}
	if !stringIndex(i) {
		panic(fmt.Sprintf("cakebear: %s is not a string index (Node reads undefined)", NumberToString(i)))
	}
	panic(fmt.Sprintf("cakebear: index %s is out of range for a string of length %d (Node reads undefined)", NumberToString(i), utf16Len(s)))
}

// stringIndex reports whether s[i] reads a code unit when there is one
// there: i is a finite integer, and not below 0.
func stringIndex(i float64) bool {
	return i >= 0 && i == math.Trunc(i) && !math.IsInf(i, 1) // NaN fails the first
}

// unitString is the code unit at index i, an integer, as a string, or
// undefined where there is none. charAt gives "" exactly there, since a
// code unit is never the empty string.
func unitString(s string, i float64) Maybe[string] {
	if c := StrCharAt(s, i); c != "" {
		return Some(c)
	}
	return Maybe[string]{}
}
