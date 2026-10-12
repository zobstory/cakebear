package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestGoToDefinitionShorthandProperty02(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `let x = {
    [|f/*1*/oo|]
}`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyBaselineGoToDefinition(t, true, "1")
}
