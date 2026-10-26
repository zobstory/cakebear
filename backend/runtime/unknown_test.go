package runtime

import (
	"math"
	"testing"
)

// TestUnknownsAreJavaScriptValues: what Node 24.18 gave for Boolean(v),
// typeof v, v === undefined, v === null and v == null, for a value of each
// kind an unknown holds (M6 plan D5).
func TestUnknownsAreJavaScriptValues(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name                       string
		v                          any
		truthy                     bool
		tag                        TypeTag
		undefined, null, nullLoose bool
	}{
		{"undefined", nil, false, TagUndefined, true, false, true},
		{"null", Null, false, TagObject, false, true, true},
		{"0", 0.0, false, TagNumber, false, false, false},
		{"-0", NegZero, false, TagNumber, false, false, false},
		{"NaN", math.NaN(), false, TagNumber, false, false, false},
		{"1", 1.0, true, TagNumber, false, false, false},
		{`""`, "", false, TagString, false, false, false},
		{`"a"`, "a", true, TagString, false, false, false},
		{"false", false, false, TagBoolean, false, false, false},
		{"true", true, true, TagBoolean, false, false, false},
		{"an Error", NewError(ErrorBase, "", nil), true, TagObject, false, false, false},
		{"an array", NewArray[float64](), true, TagObject, false, false, false},
		{"an object", newTestObject(nil, nil), true, TagObject, false, false, false},
	} {
		if got := TruthyUnknown(c.v); got != c.truthy {
			t.Errorf("Boolean(%s) = %v", c.name, got)
		}
		if got := TypeOf(c.v); got != c.tag {
			t.Errorf("typeof %s = %s, want %s", c.name, got, c.tag)
		}
		if IsUndefinedUnknown(c.v) != c.undefined || IsNullUnknown(c.v) != c.null || IsEmptyUnknown(c.v) != c.nullLoose {
			t.Errorf("%s: === undefined %v, === null %v, == null %v", c.name, IsUndefinedUnknown(c.v), IsNullUnknown(c.v), IsEmptyUnknown(c.v))
		}
	}
}

// TestUnknownEqualityIsStrict: Go's == on two unknowns is ===, as Node gave
// it: NaN unequal to itself, -0 equal to 0, objects and errors by identity,
// strings by value, null unequal to undefined, and 1 to "1".
func TestUnknownEqualityIsStrict(t *testing.T) {
	t.Parallel()

	o, e := any(newTestObject(nil, nil)), any(NewError(ErrorBase, "x", nil))
	for _, c := range []struct {
		name string
		a, b any
		want bool
	}{
		{"NaN === NaN", math.NaN(), math.NaN(), false},
		{"-0 === 0", NegZero, 0.0, true},
		{"o === o", o, o, true},
		{"{} === {}", newTestObject(nil, nil), newTestObject(nil, nil), false},
		{"e === e", e, e, true},
		{"two errors", NewError(ErrorBase, "x", nil), NewError(ErrorBase, "x", nil), false},
		{`"a" === "a"`, "a", "a", true},
		{"null === undefined", Null, nil, false},
		{`1 === "1"`, 1.0, "1", false},
	} {
		if got := c.a == c.b; got != c.want {
			t.Errorf("%s = %v", c.name, got)
		}
	}
}

// TestBoxAndUnbox: a maybe boxes as the value its state is, so an unknown
// never holds one, and an unbox gives back what was boxed, or, where the
// narrowing was unsound, panics saying what it held, since Node carries on.
func TestBoxAndUnbox(t *testing.T) {
	t.Parallel()

	if BoxMaybe(Maybe[float64]{}) != nil || BoxMaybe(NullOf[string]()) != Null || BoxMaybe(Some(2.0)) != any(2.0) {
		t.Error("BoxMaybe doesn't give the value each state is")
	}
	arr := NewArray(1.0)
	if Unbox[float64](any(3.0), "u") != 3 || Unbox[string](any("s"), "u") != "s" || Unbox[*Array[float64]](any(arr), "u") != arr {
		t.Error("Unbox doesn't give back what was boxed")
	}
	if m := UnboxMaybe[*Array[float64]](Null, "u"); !m.IsNull() {
		t.Errorf("UnboxMaybe of null = %v", m)
	}
	if m := UnboxMaybe[*Array[float64]](nil, "u"); !m.IsUndefined() {
		t.Errorf("UnboxMaybe of undefined = %v", m)
	}
	if m := UnboxMaybe[*Array[float64]](any(arr), "u"); m.Must("m") != arr {
		t.Error("UnboxMaybe of a value isn't that value")
	}
	const narrowed = " where TypeScript had narrowed it to "
	for _, c := range []struct {
		name string
		do   func()
		want string
	}{
		{"a string as a number", func() { Unbox[float64](any("s"), "e") }, "cakebear: e is a string" + narrowed + "a number (Node carries on with it)"},
		{"undefined as an Error", func() { Unbox[*Error](nil, "e") }, "cakebear: e is undefined" + narrowed + "an Error (Node carries on with it)"},
		{"null as a boolean", func() { Unbox[bool](Null, "e") }, "cakebear: e is null" + narrowed + "a boolean (Node carries on with it)"},
		{"an array of another type", func() { Unbox[*Array[string]](any(arr), "e.list") },
			"cakebear: e.list is an array" + narrowed + "an array of another shape (Node carries on with it)"},
		{"an Error as an object", func() { UnboxMaybe[*testObject](any(NewError(ErrorBase, "", nil)), "e") },
			"cakebear: e is an Error" + narrowed + "an object (Node carries on with it)"},
	} {
		if got := panicked(c.do); got != c.want {
			t.Errorf("%s: panicked %q, want %q", c.name, got, c.want)
		}
	}
}

// TestUnknownToStringOfEmpties: String(undefined) and String(null) are
// their names at the top level, and nothing in an array's join.
func TestUnknownToStringOfEmpties(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		v    any
		want string
	}{
		{nil, "undefined"}, {Null, "null"}, {NewArray[any](nil, Null, 1.0), ",,1"}, {NewArray(Some(1.0), Maybe[float64]{}), "1,"},
		{NewArray(NewError(ErrorType, "t", nil)), "TypeError: t"},
	} {
		if got := UnknownToString(c.v); got != c.want {
			t.Errorf("String(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}
