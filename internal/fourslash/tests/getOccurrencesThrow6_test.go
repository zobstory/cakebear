package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	. "github.com/zobstory/cakebear/internal/fourslash/tests/util"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestGetOccurrencesThrow6(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `[|throw|] 100;

try {
    throw 0;
    var x = () => { throw 0; };
}
catch (y) {
    var x = () => { throw 0; };
    [|throw|] 200;
}
finally {
    [|throw|] 300;
}`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyBaselineDocumentHighlights(t, nil /*preferences*/, ToAny(f.Ranges())...)
}
