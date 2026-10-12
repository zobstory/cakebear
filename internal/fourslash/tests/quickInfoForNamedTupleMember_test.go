package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestQuickInfoForNamedTupleMember(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `type foo = [/**/x: string];`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyQuickInfoAt(t, "", "string", "")
}
