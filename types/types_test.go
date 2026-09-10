package types

import (
	"math"
	"strings"
	"testing"
)

func TestIsExtension(t *testing.T) {
	t.Parallel()

	for _, name := range All {
		if !IsExtension(name) {
			t.Errorf("IsExtension(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"number", "string", "i8", "", "I32"} {
		if IsExtension(name) {
			t.Errorf("IsExtension(%q) = true, want false", name)
		}
	}
}

func TestValidateIntegerRanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		extension string
		value     float64
		wantErr   string
	}{
		{"i32 max", I32, math.MaxInt32, ""},
		{"i32 min", I32, math.MinInt32, ""},
		{"i32 over", I32, math.MaxInt32 + 1, "outside the range of i32"},
		{"i32 under", I32, math.MinInt32 - 1, "outside the range of i32"},
		{"i32 fractional", I32, 1.5, "not a whole number"},

		{"u32 max", U32, math.MaxUint32, ""},
		{"u32 zero", U32, 0, ""},
		// The most likely real mistake: unsigned types have no negatives.
		{"u32 negative", U32, -1, "outside the range of u32"},

		{"i64 within", I64, 9007199254740991, ""},
		{"u64 zero", U64, 0, ""},

		{"f32 within", F32, 1.5, ""},
		{"f32 over", F32, math.MaxFloat64, "outside the range of f32"},

		{"nan", I32, math.NaN(), "cannot hold NaN"},
		{"infinity", I32, math.Inf(1), "cannot hold Infinity"},
		{"negative infinity", I64, math.Inf(-1), "cannot hold -Infinity"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ValidateLiteral(tt.extension, tt.value, "", false)
			if tt.wantErr == "" {
				if got != "" {
					t.Errorf("ValidateLiteral(%s, %v) = %q, want no error", tt.extension, tt.value, got)
				}
				return
			}
			if !strings.Contains(got, tt.wantErr) {
				t.Errorf("ValidateLiteral(%s, %v) = %q, want it to mention %q", tt.extension, tt.value, got, tt.wantErr)
			}
		})
	}
}

func TestValidateBase64(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"Y2FrZWJlYXI=", "", "YQ==", "YWJjZA=="} {
		if got := ValidateLiteral(Base64, 0, ok, true); got != "" {
			t.Errorf("ValidateLiteral(base64, %q) = %q, want no error", ok, got)
		}
	}
	for _, bad := range []string{"!!!", "not base64!", "Y2FrZWJlYXI"} {
		if got := ValidateLiteral(Base64, 0, bad, true); got == "" {
			t.Errorf("ValidateLiteral(base64, %q) accepted an invalid string", bad)
		}
	}
}

// A conversion given the wrong kind of literal should say so plainly rather
// than silently accepting it.
func TestValidateRejectsWrongLiteralKind(t *testing.T) {
	t.Parallel()

	if got := ValidateLiteral(I32, 0, "5", true); !strings.Contains(got, "needs a number") {
		t.Errorf("i32 with a string literal = %q, want it to ask for a number", got)
	}
	if got := ValidateLiteral(Base64, 5, "", false); !strings.Contains(got, "needs a string") {
		t.Errorf("base64 with a number literal = %q, want it to ask for a string", got)
	}
}

// Diagnostics quote the value the author wrote. Go's %v would render
// 3000000000 as "3e+09", which is not what anyone typed.
func TestDiagnosticsShowNumbersAsWritten(t *testing.T) {
	t.Parallel()

	got := ValidateLiteral(I32, 3000000000, "", false)
	if !strings.Contains(got, "3000000000") {
		t.Errorf("message = %q, want it to contain 3000000000 rather than an exponent", got)
	}
	if strings.Contains(got, "e+") {
		t.Errorf("message = %q, want no exponent notation", got)
	}
}

func TestShowNumber(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		in   float64
		want string
	}{{3000000000, "3000000000"}, {1.5, "1.5"}, {-1, "-1"}, {0, "0"}} {
		if got := showNumber(tt.in); got != tt.want {
			t.Errorf("showNumber(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The declarations are served from a virtual path, so they cannot be shadowed
// by a real file or left stale by a partial install.
func TestDeclarationsAreServedVirtually(t *testing.T) {
	t.Parallel()

	path := DeclarationPath()
	if !strings.HasPrefix(path, scheme) {
		t.Errorf("DeclarationPath() = %q, want it under %q", path, scheme)
	}

	fs := WrapFS(nil)
	if !fs.FileExists(path) {
		t.Error("the overlay does not report its own declarations as existing")
	}
	contents, ok := fs.ReadFile(path)
	if !ok {
		t.Fatal("the overlay could not read its own declarations")
	}
	for _, want := range All {
		if !strings.Contains(contents, "declare function "+want+"(") {
			t.Errorf("declarations are missing a conversion function for %s", want)
		}
	}
	if !strings.Contains(contents, brandProperty) {
		t.Errorf("declarations do not mention the brand property %s", brandProperty)
	}
}
