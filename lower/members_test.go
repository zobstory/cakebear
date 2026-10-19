package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/ir"
)

// lowerUnchecked lowers src under noUncheckedIndexedAccess, which types a read
// of an element `T | undefined`.
func lowerUnchecked(t *testing.T, src string) *ir.Program {
	t.Helper()
	program, file := checkSourceWith(t, src, func(o *core.CompilerOptions) { o.NoUncheckedIndexedAccess = core.TSTrue })
	c, done := program.GetTypeChecker(t.Context())
	defer done()
	p, errs := Program(file, c)
	if len(errs) > 0 {
		t.Fatalf("unexpected lowering errors: %v", joinMsgs(errs))
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return p
}

// TestMembersMayBeUndefined (D9): find, findLast, at, and pop and shift as
// values are the element or undefined; pop and shift as statements are void;
// a string's at and codePointAt the same.
func TestMembersMayBeUndefined(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `const xs: number[] = [1, 2, 3];
const ms: (number | undefined)[] = [1, undefined];
const f: number | undefined = xs.find((x) => x > 1);
const l: number | undefined = xs.findLast((x, i) => i < 2);
const a: number | undefined = xs.at(-1);
const p: number | undefined = xs.pop();
const s: number | undefined = ms.shift();
xs.pop();
xs.shift();
const c: string | undefined = "ab".at(-1);
const u: number | undefined = "ab".codePointAt(5);
`)
	ops := nodesOf[*ir.ArrayOp](p.EntryModule().Main)
	want := []struct {
		m ir.ArrayMethod
		t ir.Type
	}{
		{ir.ArrayFind, ir.MaybeOf(ir.Number)}, {ir.ArrayFindLast, ir.MaybeOf(ir.Number)}, {ir.ArrayAt, ir.MaybeOf(ir.Number)},
		{ir.ArrayPop, ir.MaybeOf(ir.Number)}, {ir.ArrayShift, ir.MaybeOf(ir.Number)}, {ir.ArrayPop, ir.Void}, {ir.ArrayShift, ir.Void},
	}
	if len(ops) != len(want) {
		t.Fatalf("%d array ops, want %d: %v", len(ops), len(want), ops)
	}
	for i, w := range want {
		if ops[i].Method != w.m || !ops[i].Typ.Equal(w.t) {
			t.Errorf("op %d is %s typed %s, want %s typed %s", i, ops[i].Method, ops[i].Typ, w.m, w.t)
		}
	}
	calls := nodesOf[*ir.BuiltinCall](p.EntryModule().Main)
	if len(calls) != 2 || calls[0].Fn != ir.StrAt || calls[1].Fn != ir.StrCodePointAt {
		t.Errorf("the string members are %v, want at and codePointAt", calls)
	}
}

// TestIndexReads (D9): a read the checker types as one that may be undefined
// is undefined past the end, and so is any read of an array of maybes; one
// typed as the element, without the flag or narrowed, is checked.
func TestIndexReads(t *testing.T) {
	t.Parallel()

	const src = `const xs: number[] = [1];
const ms: (number | undefined)[] = [1];
const s: string = "ab";
const i: number = 1;
const x = xs[i];
const m = ms[i];
const c = s[i];
if (xs[0] !== undefined) {
  const n: number = xs[0] + 1;
}
`
	reads := func(p *ir.Program) ([]*ir.IndexGet, []*ir.BuiltinCall) {
		return nodesOf[*ir.IndexGet](p.EntryModule().Main), nodesOf[*ir.BuiltinCall](p.EntryModule().Main)
	}
	gets, calls := reads(lowerProgramClean(t, src))
	if len(gets) != 4 || gets[0].OrUndefined || !gets[1].OrUndefined || gets[2].OrUndefined || gets[3].OrUndefined {
		t.Errorf("without the flag, the reads are %v, want only ms[i]'s undefined past the end", gets)
	}
	if len(calls) != 1 || calls[0].Fn != ir.StrIndex {
		t.Errorf("without the flag, s[i] is %v, want the checked read", calls)
	}
	gets, calls = reads(lowerUnchecked(t, src))
	if len(gets) != 4 || !gets[0].OrUndefined || !gets[0].Typ.Equal(ir.MaybeOf(ir.Number)) || !gets[1].OrUndefined || !gets[2].OrUndefined || gets[3].OrUndefined {
		t.Errorf("with the flag, the reads are %v, want all but the narrowed xs[0] undefined past the end", gets)
	}
	if len(calls) != 1 || calls[0].Fn != ir.StrIndexOrUndefined {
		t.Errorf("with the flag, s[i] is %v, want undefined past the end", calls)
	}
}

// TestUndefinedElementWrites: `xs[0] ??= v` reads the element as one that may
// be undefined and writes v as the element it stores, unwrapped, where the
// array's elements aren't maybes.
func TestUndefinedElementWrites(t *testing.T) {
	t.Parallel()

	const src = "const xs: number[] = [];\nxs[0] ??= 5;\nconst ms: (number | undefined)[] = [];\nms[0] ??= 6;\n"
	// Without the flag xs[0] is typed number, but is undefined past the end
	// all the same, so ??= still tests it.
	if sets := nodesOf[*ir.IndexSet](lowerProgramClean(t, src).EntryModule().Main); len(sets) != 2 {
		t.Errorf("without the flag, %d element writes, want 2", len(sets))
	}
	p := lowerUnchecked(t, src)
	sets := nodesOf[*ir.IndexSet](p.EntryModule().Main)
	if len(sets) != 2 {
		t.Fatalf("%d element writes, want 2", len(sets))
	}
	if _, ok := sets[0].Value.(*ir.NumberLit); !ok {
		t.Errorf("xs[0] ??= 5 writes %#v, want 5 as a number", sets[0].Value)
	}
	if !isWrap(sets[1].Value) {
		t.Errorf("ms[0] ??= 6 writes %#v, want 6 wrapped", sets[1].Value)
	}
}

func TestMemberRefusals(t *testing.T) {
	t.Parallel()

	const xs = "const xs: (number | undefined)[] = [];\n"
	for _, tt := range []struct{ name, src, want string }{
		{"find by truthiness", "const xs: number[] = [];\nconst n: number | undefined = xs.find((x) => x);\n", "testing a number for truthiness is not supported yet"},
		{"find's thisArg", "const xs: number[] = [];\nconst n: number | undefined = xs.find((x) => x > 0, xs);\n", "find's thisArg is not supported"},
		{"a string's element read", "const s: string[] = [\"a\"];\nconst t: string = \"b\";\nconsole.log(s, t[0]);\n", ""},
	} {
		_, errs := lowerProgram(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) || (tt.want == "") != (msgs == "") {
			t.Errorf("%s: errors %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}
}
