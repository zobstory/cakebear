package runtime

import "math"

// Mod is JavaScript's % on numbers: the remainder of truncated division,
// with the dividend's sign. math.Mod is exact, as V8's fmod is, and the two
// agree to the bit, -0 and the infinities included (TestModMatchesNode). An
// f32's remainder is exact in a float64, so converting it back loses nothing.
func Mod[T float32 | float64](a, b T) T {
	return T(math.Mod(float64(a), float64(b)))
}

// IntQuo is / on one of the integer extension types, inside its conversion
// (`i32(a / b)`): Go's quotient, truncated toward zero, as the conversion
// truncates the number's. A zero divisor panics, saying why, where Go's own
// message would only say "integer divide by zero".
func IntQuo[T int32 | int64 | uint32 | uint64](a, b T, typ string) T {
	if b == 0 {
		panic(byZero(typ, "/", "Infinity, or NaN for 0 / 0"))
	}
	return a / b
}

// IntRem is % on one of the integer extension types, inside its conversion:
// Go's remainder, with the dividend's sign, as JavaScript's.
func IntRem[T int32 | int64 | uint32 | uint64](a, b T, typ string) T {
	if b == 0 {
		panic(byZero(typ, "%", "NaN"))
	}
	return a % b
}

func byZero(typ, op, number string) string {
	return "cakebear: " + typ + "(a " + op + " b) with b zero: as a number it is " + number + ", which " + typ + " can't hold"
}
