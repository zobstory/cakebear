package runtime

import (
	"math"
	"testing"
)

// TestTruthyNumberIsToBoolean: 0, -0 and NaN are falsy, and every other
// number truthy, as Node 24.18 gives `!!n` for each.
func TestTruthyNumberIsToBoolean(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		n    float64
		want bool
	}{
		{0, false}, {math.Copysign(0, -1), false}, {math.NaN(), false},
		{1, true}, {-1, true}, {0.5, true}, {5e-324, true}, {-5e-324, true},
		{math.Inf(1), true}, {math.Inf(-1), true}, {math.MaxFloat64, true},
	} {
		if got := TruthyNumber(tt.n); got != tt.want {
			t.Errorf("TruthyNumber(%v) = %v, want %v", tt.n, got, tt.want)
		}
	}
}

// TestMaybeTruthy: an empty Maybe is falsy without its value's test being
// asked, and one holding a value is as truthy as the test says.
func TestMaybeTruthy(t *testing.T) {
	t.Parallel()

	asked := 0
	test := func(f float64) bool { asked++; return TruthyNumber(f) }
	for _, tt := range []struct {
		name  string
		m     Maybe[float64]
		want  bool
		asked int
	}{
		{"undefined", Maybe[float64]{}, false, 0},
		{"null", NullOf[float64](), false, 0},
		{"0", Some(0.0), false, 1},
		{"NaN", Some(math.NaN()), false, 1},
		{"5", Some(5.0), true, 1},
	} {
		asked = 0
		if got := tt.m.Truthy(test); got != tt.want || asked != tt.asked {
			t.Errorf("%s: Truthy = %v, asking the test %d times; want %v and %d", tt.name, got, asked, tt.want, tt.asked)
		}
	}
	if !AlwaysTruthy(&struct{}{}) || !AlwaysTruthy[func()](nil) {
		t.Errorf("AlwaysTruthy is false")
	}
}
