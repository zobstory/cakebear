package lower

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestReferencesNameTheirModule: a reference to a top-level declaration is
// qualified with its module's file, and one to a function's own binding, or
// to a block's inside top-level code, is not (M3 plan D5).
func TestReferencesNameTheirModule(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `let count: number = 0;
const limit: number = 3;
function bump(by: number): number {
  const step: number = by;
  count = count + step;
  return count;
}
function apply(f: (n: number) => number, x: number): number {
  return f(x);
}
console.log(apply((n: number): number => n + limit + count, bump(1)));
if (limit > 0) {
  let local: number = 1;
  local = local + 1;
  console.log(local);
}
`)
	file := m.File
	if !strings.HasSuffix(file, "t.ts") {
		t.Fatalf("the module's file is %q", file)
	}

	// Every reference, as "kind name", with the module it names.
	got := map[string]string{}
	note := func(key, module string) {
		if prev, seen := got[key]; seen && prev != module {
			t.Errorf("%s is qualified both %q and %q", key, prev, module)
		}
		got[key] = module
	}
	ir.Inspect(m, func(node any) {
		switch n := node.(type) {
		case *ir.Ident:
			note("ident "+n.Name, n.Module)
		case *ir.Call:
			note("call "+n.Callee, n.Module)
		case *ir.Assign:
			note("assign "+n.Name, n.Module)
		case *ir.FuncLit:
			for _, c := range n.Captures {
				note("capture "+c.Name, c.Module)
			}
		}
	})

	want := map[string]string{
		"ident count": file, "ident limit": file, "assign count": file,
		"call bump": file, "call apply": file,
		"capture limit": file, "capture count": file,
		"ident by": "", "ident step": "", "ident x": "", "ident n": "",
		"call f": "", "ident local": "", "assign local": "",
	}
	for _, key := range slices.Sorted(maps.Keys(want)) {
		if module, ok := got[key]; !ok {
			t.Errorf("no %s among the references", key)
		} else if module != want[key] {
			t.Errorf("%s is qualified %q, want %q", key, module, want[key])
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected reference %s", key)
		}
	}

	if err := ir.ProgramOf(m).Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
