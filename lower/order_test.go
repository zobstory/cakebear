package lower

import (
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestStableReads: a read is stable exactly when nothing anywhere writes its
// binding after it is declared, a closure included.
func TestStableReads(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `const k: number = 1;
let fixed: number = 2;
let moved: number = 3;
function f(p: number, q: number): number {
  const set = (): number => {
    q = 0;
    return 1;
  };
  return p + q + set() + k + fixed + moved;
}
moved = 4;
console.log(f(5, 6));
`)
	got := map[string]bool{}
	ir.Inspect(m, func(node any) {
		if id, ok := node.(*ir.Ident); ok {
			got[id.Name] = id.Stable
		}
	})
	for name, want := range map[string]bool{"k": true, "fixed": true, "p": true, "moved": false, "q": false} {
		if stable, ok := got[name]; !ok || stable != want {
			t.Errorf("%s: Stable = %v (read %v), want %v", name, stable, ok, want)
		}
	}
}
