package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/lsp/lsproto"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestGetOutliningSpansForUnbalancedEndRegion(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `// bottom-heavy region balance
[|// #region matched

// #endregion matched|]

// #endregion unmatched`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyOutliningSpans(t, lsproto.FoldingRangeKindRegion)
}
