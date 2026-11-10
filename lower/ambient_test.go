package lower

import (
	"strings"
	"testing"
)

// TestAmbientDeclarationsAreErased (found by M7's re-measurement): a module
// declaration Node strips and TypeScript emits nothing for, `declare global`,
// `declare module "x"` or a namespace of types alone, is erased as an
// interface is; it was refused as a module declaration, though it evaluates
// nothing. A value it declares has no code, and is refused where it is used;
// a namespace with code stays refused.
func TestAmbientDeclarationsAreErased(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, want string // want "" for a program that lowers
	}{
		{"declare global", "declare global {\n  interface Window {\n    tag: string;\n  }\n}\nexport {};\nconsole.log(1);\n", ""},
		{"declare module, an augmentation", "declare module \"node:http\" {\n  interface ServerResponse {\n    tag?: string;\n  }\n}\nexport {};\nconsole.log(2);\n", ""},
		{"a namespace of types", "namespace N {\n  export interface T {\n    a: number;\n  }\n}\nconst t: N.T = { a: 3 };\nconsole.log(t.a);\n", ""},
		{"a value declare global declares", "declare global {\n  var BUILD: string;\n}\nconsole.log(BUILD);\nexport {};\n",
			"BUILD is declared with `declare` in t.ts, so the program has no code for it"},
		{"a namespace with code", "namespace C {\n  export const x: number = 1;\n}\nconsole.log(C.x);\n", "module declaration is not supported yet"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerFiles(t, map[string]string{"t.ts": tt.src}, "t.ts")
			msgs := joinMsgs(errs)
			switch {
			case tt.want == "" && len(errs) > 0:
				t.Errorf("refused:\n%s", msgs)
			case tt.want != "" && !strings.Contains(msgs, tt.want):
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
		})
	}
}
