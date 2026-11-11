package lower

import "testing"

// TestUnknownObjectsLower: what M6 refused of an unknown narrowed to an
// object or an array lowers since S1 step 2 (plan D4 (b)).
func TestUnknownObjectsLower(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"function has(u: unknown): boolean {\n  return typeof u === \"object\" && u !== null && \"k\" in u;\n}\nconsole.log(has(1));\n",
		"function nameOf(u: unknown): unknown {\n  if (typeof u === \"object\" && u !== null && \"name\" in u) {\n    return u.name;\n  }\n  return undefined;\n}\nconsole.log(nameOf(JSON.parse(\"{}\")));\n",
		"function first(u: unknown): unknown {\n  if (Array.isArray(u) && u.length > 0) {\n    const x: unknown = u[0];\n    return x;\n  }\n  return null;\n}\nconsole.log(first(JSON.parse(\"[1]\")));\n",
	} {
		if _, errs := lowerProgram(t, src); len(errs) > 0 {
			t.Errorf("%s: errors = %q, want none", src, joinMsgs(errs))
		}
	}
}
