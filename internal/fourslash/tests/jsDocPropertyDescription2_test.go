package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestJsDocPropertyDescription2(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `interface SymbolExample {
    /** Something generic */
    [key: symbol]: string;
}
function symbolExample(e: SymbolExample) {
    console.log(e./*symbol*/anything);
}`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyQuickInfoAt(t, "symbol", "any", "")
}
