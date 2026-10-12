package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/ls/lsutil"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestInlayHintsVariableTypes3(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `// @strict: true
// @target: esnext
interface DivElement {}
declare var DivElementCtor: {
  prototype: DivElement;
  new(): DivElement;
};
interface ElementMap {
  div: typeof DivElementCtor;
}
declare function getCtor<K extends keyof ElementMap>(tagName: K): ElementMap[K] | undefined;
const div = getCtor("div");`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyBaselineInlayHints(t, nil /*span*/, &lsutil.UserPreferences{IncludeInlayVariableTypeHints: core.TSTrue})
}
