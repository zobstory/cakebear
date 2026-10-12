package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/ls/lsutil"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestInlayHintsEnumMemberValue(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `enum E {
    A,
    AA,
    B = 10,
    BB,
    C = 'C',
}`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyBaselineInlayHints(t, nil /*span*/, &lsutil.UserPreferences{IncludeInlayEnumMemberValueHints: core.TSTrue})
}
