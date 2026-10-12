package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestFindAllReferencesFromLinkTagReference1(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `enum E {
    /** {@link /**/A} */
    A
}`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyBaselineFindAllReferences(t, "")
}
