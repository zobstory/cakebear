package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/ls/lsutil"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestInlayHintsTypeParameterModifiers1(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `function test1() {
  return function <const T>(a: T) {};
}`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyBaselineInlayHints(t, nil /*span*/, &lsutil.UserPreferences{IncludeInlayFunctionLikeReturnTypeHints: core.TSTrue})
}
