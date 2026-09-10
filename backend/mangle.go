package backend

import "strings"

// genModule is the module path of the throwaway module cakec generates. It
// never leaves the temp directory and is never published, so the name only has
// to be a valid module path that cannot collide with a real one.
const genModule = "cakebear.local/out"

// rtDir is where the embedded runtime is written inside the generated module.
const rtDir = "cakebearrt"

// goKeywords cannot appear as identifiers in Go. TypeScript happily allows all
// of them as variable names, so any of these arriving from user source has to
// be renamed rather than emitted.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// goPredeclared are not keywords but are predeclared in Go's universe scope.
// Shadowing most of them is legal, but a user variable named `string` or `len`
// makes the emitted code either wrong or unreadable, so they are renamed too.
var goPredeclared = map[string]bool{
	"any": true, "bool": true, "byte": true, "complex64": true, "complex128": true,
	"error": true, "float32": true, "float64": true, "int": true, "int8": true,
	"int16": true, "int32": true, "int64": true, "rune": true, "string": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "true": true, "false": true, "iota": true, "nil": true,
	"append": true, "cap": true, "clear": true, "close": true, "complex": true,
	"copy": true, "delete": true, "imag": true, "len": true, "make": true,
	"max": true, "min": true, "new": true, "panic": true, "print": true,
	"println": true, "real": true, "recover": true, "main": true,
}

// mangle turns a TypeScript identifier into a Go one.
//
// Names pass through untouched wherever that is safe, because generated source
// that a person can read against the original is worth a great deal when
// debugging the emitter. Go and TypeScript agree on identifier syntax more than
// they disagree -- both permit Unicode letters, so `café` and `日本` need no
// help -- and the collisions that remain are a short, closed list.
//
// The prefix is applied to the whole name rather than a character being
// substituted, so mangling can never map two distinct names onto one: `type`
// becomes `ts_type`, and a user identifier already called `ts_type` becomes
// `ts_ts_type`.
func mangle(name string) string {
	if name == "" {
		return "_"
	}
	if goKeywords[name] || goPredeclared[name] || name == rtPkg || strings.HasPrefix(name, "ts_") {
		return "ts_" + name
	}
	// A leading underscore is legal in both languages, but `_` alone is Go's
	// blank identifier and cannot be read back.
	if name == "_" {
		return "ts_blank"
	}
	return name
}
