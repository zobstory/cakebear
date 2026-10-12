package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	. "github.com/zobstory/cakebear/internal/fourslash/tests/util"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestGetOccurrencesTryCatchFinally2(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `try {
    [|t/*1*/r/*2*/y|] {
    }
    [|c/*3*/atch|] (x) {
    }

    try {
    }
    finally {
    }
}
catch (e) {
}
finally {
}`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyBaselineDocumentHighlights(t, nil /*preferences*/, ToAny(f.Markers())...)
}
