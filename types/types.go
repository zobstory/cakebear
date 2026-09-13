// Package types implements cakebear's own type extensions.
//
// The extensions are branded types declared in cakebear.d.ts and injected into
// every program: an intersection of a primitive with a phantom property. That
// choice is what keeps the golden rule intact -- TypeScript's checker enforces
// them with no edit inside internal/checker, so upstream merges stay
// mechanical, and retiring an extension when TypeScript ships an equivalent is
// deleting a declaration rather than unpicking a patch.
//
// Recognising a brand and validating a literal live here; lowering them to
// native Go types lives in lower/ and backend/.
package types

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/internal/vfs"
)

//go:embed cakebear.d.ts
var declarations string

// scheme mirrors how upstream serves its bundled lib files. The declarations
// never exist on disk, so they cannot be edited, shadowed by a file of the same
// name, or left stale by a partial install.
const scheme = "cakebear:///"

// DeclarationPath is the virtual path of cakebear's declarations. cakec adds it
// as a root file of every program.
func DeclarationPath() string { return scheme + "cakebear.d.ts" }

// brandProperty is the phantom property carrying an extension's name. Nothing
// can have it at runtime; it exists only for the checker to compare.
const brandProperty = "__cakebearBrand"

// Extension names, matching the brands in cakebear.d.ts.
const (
	I32    = "i32"
	I64    = "i64"
	U32    = "u32"
	U64    = "u64"
	F32    = "f32"
	Base64 = "base64"
)

// All lists every extension, in declaration order.
var All = []string{I32, I64, U32, U64, F32, Base64}

// IsExtension reports whether name is one of cakebear's extension types.
func IsExtension(name string) bool {
	for _, e := range All {
		if e == name {
			return true
		}
	}
	return false
}

// BrandOf returns the extension name carried by a checked type, or "" when the
// type is not one of cakebear's.
//
// It reads the phantom property through the checker's public API rather than
// matching on the annotation's spelling, so an alias (`type Id = i32`) resolves
// correctly and a local variable shadowing the name does not.
func BrandOf(c *checker.Checker, t *checker.Type) string {
	if c == nil || t == nil {
		return ""
	}
	prop := c.GetPropertyOfType(t, brandProperty)
	if prop == nil {
		return ""
	}
	// The brand's type is a string literal; TypeToString renders it quoted.
	name := strings.Trim(c.TypeToString(c.GetTypeOfSymbol(prop)), `"`)
	if !IsExtension(name) {
		return ""
	}
	return name
}

// ValidateLiteral checks a literal argument to a conversion at compile time.
//
// This is where the extensions earn their place. A brand alone only stops you
// passing the wrong type around; catching `i32(3000000000)` and
// `base64("!!!")` before the program runs is the part a plain `number` or
// `string` could never do.
//
// It returns an empty string when the value is acceptable.
func ValidateLiteral(extension string, num float64, str string, isString bool) string {
	if extension == Base64 {
		if !isString {
			return "base64 needs a string"
		}
		if _, err := base64.StdEncoding.DecodeString(str); err != nil {
			return fmt.Sprintf("%q is not valid base64", str)
		}
		return ""
	}

	if isString {
		return extension + " needs a number"
	}

	if math.IsNaN(num) || math.IsInf(num, 0) {
		return fmt.Sprintf("%s cannot hold %s", extension, describeFloat(num))
	}

	switch extension {
	case F32:
		// float32 has a much smaller range than float64; anything beyond it
		// would silently become an infinity.
		if math.Abs(num) > math.MaxFloat32 {
			return showNumber(num) + " is outside the range of f32"
		}
		return ""
	case I32:
		return checkInteger(extension, num, math.MinInt32, math.MaxInt32)
	case I64:
		return checkInteger(extension, num, math.MinInt64, math.MaxInt64)
	case U32:
		return checkInteger(extension, num, 0, math.MaxUint32)
	case U64:
		return checkInteger(extension, num, 0, math.MaxUint64)
	default:
		return ""
	}
}

func checkInteger(extension string, v, low, high float64) string {
	if v != math.Trunc(v) {
		return fmt.Sprintf("%s is not a whole number, so it cannot be %s", showNumber(v), extension)
	}
	if v < low || v > high {
		return fmt.Sprintf("%s is outside the range of %s", showNumber(v), extension)
	}
	return ""
}

// showNumber renders a value the way the author wrote it rather than the way Go
// prints a float64: someone who typed 3000000000 should not be told about
// "3e+09".
func showNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func describeFloat(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "Infinity"
	default:
		return "-Infinity"
	}
}

// Overlay serves cakebear's declarations from a virtual path alongside a real
// filesystem, the same way upstream serves its bundled lib files.
//
// Embedding vfs.FS forwards every method we do not care about, so this keeps
// working when upstream adds one -- an explicit implementation of the whole
// interface would break on the next sync, which the golden rule cannot prevent
// because the breakage would be in our file.
type Overlay struct {
	vfs.FS
}

// WrapFS returns fs with cakebear's declarations overlaid.
func WrapFS(fs vfs.FS) vfs.FS { return &Overlay{FS: fs} }

func (o *Overlay) FileExists(path string) bool {
	if path == DeclarationPath() {
		return true
	}
	return o.FS.FileExists(path)
}

func (o *Overlay) ReadFile(path string) (string, bool) {
	if path == DeclarationPath() {
		return declarations, true
	}
	return o.FS.ReadFile(path)
}
