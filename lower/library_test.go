package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// A name that only TypeScript's libs declare has no Go behind it, so lowering
// refuses it by name. It used to lower to a plain Go reference and fail inside
// go build with "undefined: encodeURI".
func TestLibraryGlobalsAreRefused(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"call", "const s: string = encodeURI(\"a b\");\n", "encodeURI is a library function cakebear does not implement yet"},
		// setTimeout itself is lowered since M2 (async.go); setInterval is not.
		{"call with a callback", "setInterval(() => {\n  console.log(\"t\");\n}, 0);\n",
			"setInterval is a library function cakebear does not implement yet"},
		{"value", "console.log(NaN);\n", "NaN is a library global cakebear does not implement yet"},
		{"function passed as a value", "function apply(f: (x: number) => boolean, v: number): boolean {\n  return f(v);\n}\nconsole.log(apply(isNaN, 1));\n",
			"isNaN is a library global cakebear does not implement yet"},
		{"stored in a variable", "const g = parseInt;\n", "parseInt is a library global cakebear does not implement yet"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, tt.src)
			if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
			for _, e := range errs {
				if e.Kind != Unsupported {
					t.Errorf("%q is reported as an error; it is a limit of this phase", e.Msg)
				}
			}
		})
	}
}

// The program's own declarations are not library globals, even when they
// share a library name: symbols decide, not spellings.
func TestOwnNamesThatShadowLibraryGlobalsAreKept(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `function parseInt(s: string): number {
  return 7;
}
function run(setTimeout: () => void): void {
  setTimeout();
}
const n: number = parseInt("5");
run(() => console.log(n));
`)
	if call, ok := m.Main[0].(*ir.VarDecl).Init.(*ir.Call); !ok || call.Callee != "parseInt" {
		t.Errorf("the program's own parseInt lowered to %#v, want a call to it", m.Main[0].(*ir.VarDecl).Init)
	}
	if call, ok := m.Funcs[1].Body[0].(*ir.ExprStmt).Expr.(*ir.Call); !ok || call.Callee != "setTimeout" {
		t.Errorf("a parameter named setTimeout lowered to %#v, want a call through it", m.Funcs[1].Body[0])
	}
}
