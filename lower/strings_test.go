package lower

import (
	"slices"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// TestTemplateLiterals (S2): a template is its text and its substitutions,
// each a primitive converted by ToString, or a string as it is.
func TestTemplateLiterals(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, "const n: number = 1;\nconst s: string = \"x\";\nconst b: boolean = true;\nconst w: i32 = i32(2);\n"+
		"const t1: string = `a${n}b${s}${b}${w}`;\nconst t2: string = `plain`;\nconst t3: string = `${s}`;\n")
	c, ok := m.Main[4].(*ir.VarDecl).Init.(*ir.Concat)
	if !ok {
		t.Fatalf("t1 is %T, want a Concat", m.Main[4].(*ir.VarDecl).Init)
	}
	var got []string
	for _, p := range c.Parts {
		switch p := p.(type) {
		case *ir.StringLit:
			got = append(got, "text "+p.Value)
		case *ir.BuiltinCall:
			got = append(got, p.Fn.String()+" of a "+p.Args[0].ExprType().String())
		case *ir.Ident:
			got = append(got, "the string "+p.Name)
		default:
			got = append(got, "a "+describeKind(ast.KindUnknown))
		}
	}
	want := []string{"text a", "String of a number", "text b", "the string s", "String of a boolean", "String of a i32"}
	if !slices.Equal(got, want) {
		t.Errorf("t1's parts are %q, want %q", got, want)
	}
	if lit, ok := m.Main[5].(*ir.VarDecl).Init.(*ir.StringLit); !ok || lit.Value != "plain" {
		t.Errorf("t2 is %v, want the string literal \"plain\"", m.Main[5].(*ir.VarDecl).Init)
	}
	if c3, ok := m.Main[6].(*ir.VarDecl).Init.(*ir.Concat); !ok || len(c3.Parts) != 1 {
		t.Errorf("t3 is %v, want a Concat of s alone", m.Main[6].(*ir.VarDecl).Init)
	}
}

// TestStringBuiltins: each method, conversion, parser and URI function
// lowers to its builtin, a method's receiver first, with the arguments the
// call gives and no more, so the runtime can tell an absent one from NaN.
func TestStringBuiltins(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		expr string
		fn   ir.Builtin
		args int
	}{
		{"s.length", ir.StrLength, 1},
		{"s.charAt(1)", ir.StrCharAt, 2},
		{"s.charCodeAt(0)", ir.StrCharCodeAt, 2},
		{"s.includes(\"a\")", ir.StrIncludes, 2},
		{"s.includes(\"a\", 2)", ir.StrIncludes, 3},
		{"s.startsWith(\"a\")", ir.StrStartsWith, 2},
		{"s.endsWith(\"a\", 3)", ir.StrEndsWith, 3},
		{"s.indexOf(\"a\")", ir.StrIndexOf, 2},
		{"s.lastIndexOf(\"a\", 1)", ir.StrLastIndexOf, 3},
		{"s.slice()", ir.StrSlice, 1},
		{"s.slice(1, -1)", ir.StrSlice, 3},
		{"s.substring(1)", ir.StrSubstring, 2},
		{"s.toLowerCase()", ir.StrToLowerCase, 1},
		{"s.toUpperCase()", ir.StrToUpperCase, 1},
		{"s.trim()", ir.StrTrim, 1},
		{"s.trimStart()", ir.StrTrimStart, 1},
		{"s.trimLeft()", ir.StrTrimStart, 1},
		{"s.trimEnd()", ir.StrTrimEnd, 1},
		{"s.split(\",\")", ir.StrSplit, 2},
		{"s.split(\",\", 2)", ir.StrSplit, 3},
		{"s.replace(\"a\", \"b\")", ir.StrReplace, 3},
		{"s.replaceAll(\"a\", \"b\")", ir.StrReplaceAll, 3},
		{"s.padStart(4)", ir.StrPadStart, 2},
		{"s.padEnd(4, \"-\")", ir.StrPadEnd, 3},
		{"s.repeat(2)", ir.StrRepeat, 2},
		{"String(n)", ir.ToString, 1},
		{"n.toString()", ir.ToStringMethod, 1},
		{"n.toFixed(2)", ir.NumToFixed, 2},
		{"n.toFixed()", ir.NumToFixed, 2},
		{"Number(s)", ir.NumFromStr, 1},
		{"parseInt(s)", ir.NumParseInt, 1},
		{"parseInt(s, 16)", ir.NumParseInt, 2},
		{"Number.parseInt(s)", ir.NumParseInt, 1},
		{"parseFloat(s)", ir.NumParseFlt, 1},
		{"isNaN(n)", ir.NumIsNaN, 1},
		{"Number.isFinite(n)", ir.NumIsFinite, 1},
		{"Number.isInteger(n)", ir.NumIsInteger, 1},
		{"encodeURIComponent(s)", ir.URIEncode, 1},
		{"decodeURIComponent(s)", ir.URIDecode, 1},
	} {
		t.Run(tt.expr, func(t *testing.T) {
			t.Parallel()
			m := lowerClean(t, "const s: string = \"abc\";\nconst n: number = 1.5;\nconsole.log("+tt.expr+");\n")
			arg := m.Main[2].(*ir.ExprStmt).Expr.(*ir.ConsoleLog).Args[0]
			call, ok := arg.(*ir.BuiltinCall)
			if !ok || call.Fn != tt.fn || len(call.Args) != tt.args {
				t.Fatalf("%s lowers to %#v, want %s with %d arguments", tt.expr, arg, tt.fn, tt.args)
			}
		})
	}

	// A number passed to encodeURIComponent is converted to a string first,
	// and Number of a number, and a string's toString, are the value itself.
	m := lowerClean(t, "const n: number = 5;\nconst s: string = \"x\";\nconsole.log(encodeURIComponent(n));\nconsole.log(Number(n));\nconsole.log(s.toString());\n")
	enc := m.Main[2].(*ir.ExprStmt).Expr.(*ir.ConsoleLog).Args[0].(*ir.BuiltinCall)
	if inner, ok := enc.Args[0].(*ir.BuiltinCall); !ok || inner.Fn != ir.ToString {
		t.Errorf("encodeURIComponent(n)'s argument is %#v, want String(n)", enc.Args[0])
	}
	for i := 3; i <= 4; i++ {
		if _, ok := m.Main[i].(*ir.ExprStmt).Expr.(*ir.ConsoleLog).Args[0].(*ir.Ident); !ok {
			t.Errorf("statement %d logs %#v, want the variable itself", i, m.Main[i].(*ir.ExprStmt).Expr.(*ir.ConsoleLog).Args[0])
		}
	}
}

func TestStringRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"a RegExp", "const s: string = \"a\";\nconsole.log(s.replace(/a/g, \"b\"));\n", "replace with a RegExp is not supported yet"},
		{"a RegExp value", "const s: string = \"a\";\nconst re: RegExp = new RegExp(\"a\");\nconsole.log(s.split(re));\n", "with a RegExp is not supported yet"},
		{"match", "const s: string = \"a\";\nconsole.log(s.match(\"a\"));\n", "a string's match takes a RegExp"},
		{"a function replacement", "const s: string = \"a\";\nconsole.log(s.replace(\"a\", (m: string): string => m));\n", "replace with a function argument is not supported yet"},
		{"localeCompare", "const s: string = \"a\";\nconsole.log(s.localeCompare(\"b\"));\n", "a string's localeCompare needs ICU collation"},
		{"toString with a radix", "const n: number = 255;\nconsole.log(n.toString(16));\n", "toString with a radix is not supported yet"},
		{"toPrecision", "const n: number = 1;\nconsole.log(n.toPrecision(2));\n", "a number's toPrecision is not supported yet"},
		{"mixed +", "const n: number = 1;\nconst s: string = \"a\" + n;\n", "write a template literal"},
		{"an object in a template", "const o: { a: number } = { a: 1 };\nconst s: string = `${o}`;\n", "a template literal's ${} can hold a string, a number or a boolean; a { a: number } is not supported yet (Node writes [object Object])"},
		{"an array in a template", "const xs: number[] = [1];\nconst s: string = `${xs}`;\n", "Node writes its elements joined with commas"},
		{"String of an object", "const o: { a: number } = { a: 1 };\nconst s: string = String(o);\n", "String() can hold a string, a number or a boolean"},
		{"Number of a boolean", "const n: number = Number(true);\n", "Number() of a boolean is not supported yet"},
		{"isNaN of a string", "const b: boolean = isNaN(\"a\" as unknown as number) && Number.isNaN(1);\nconst s: string = \"1\";\nconsole.log(Number.isFinite(s));\n", "Number.isFinite of a string is not supported yet"},
		{"Number.isSafeInteger", "console.log(Number.isSafeInteger(1));\n", "Number.isSafeInteger is not supported yet"},
		{"String.fromCharCode", "console.log(String.fromCharCode(65));\n", "String.fromCharCode is not supported yet"},
		{"a tagged template", "const s: string = String.raw`a`;\n", "not supported yet"},
		{"a member added to String", "export {};\ndeclare global {\n  interface String {\n    shout(): string;\n  }\n}\nconst s: string = \"a\";\nconsole.log(s.shout());\n",
			"shout is added to String outside TypeScript's libs, which is not supported"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, tt.src)
			if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
		})
	}

	// A program's own parseInt is an ordinary binding, called as itself.
	m := lowerClean(t, "export {};\nfunction parseInt(s: string): number {\n  return 7;\n}\nconsole.log(parseInt(\"1\"));\n")
	if call, ok := m.Main[0].(*ir.ExprStmt).Expr.(*ir.ConsoleLog).Args[0].(*ir.Call); !ok || call.Callee != "parseInt" {
		t.Errorf("the program's own parseInt is lowered to %#v, want a call of it", m.Main[0].(*ir.ExprStmt).Expr.(*ir.ConsoleLog).Args[0])
	}
}

// TestStringMembersAreAccountedFor: every member TypeScript's libs give a
// string is lowered, refused by name, or one of the handful handled where
// they appear (length, toString, valueOf), so a lib that gains one fails
// here rather than reaching a generic refusal.
func TestStringMembersAreAccountedFor(t *testing.T) {
	t.Parallel()

	program, file := checkSource(t, "const s: string = \"\";\n")
	c, done := program.GetTypeChecker(t.Context())
	defer done()
	decl := file.Statements.Nodes[0].AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0]
	var members []string
	for _, prop := range c.GetPropertiesOfType(c.GetApparentType(c.GetTypeAtLocation(decl.Name()))) {
		if name := prop.Name(); !strings.HasPrefix(name, ast.InternalSymbolNamePrefix) {
			members = append(members, name)
		}
	}
	for _, name := range members {
		_, lowered := stringMethods[name]
		_, refused := refusedStringMembers[name]
		if !lowered && !refused && name != "length" && name != "toString" && name != "valueOf" {
			t.Errorf("String.%s is neither lowered nor refused by name (lower/strings.go)", name)
		}
	}
	for name := range refusedStringMembers {
		if !slices.Contains(members, name) {
			t.Errorf("refusedStringMembers lists %s, which the libs no longer declare", name)
		}
	}
	if len(members) < 30 {
		t.Errorf("found %d members, too few to be the libs' String: %v", len(members), members)
	}
}
