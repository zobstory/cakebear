package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	. "github.com/zobstory/cakebear/internal/fourslash/tests/util"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestCompletionListBuilderLocations_properties(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `var aa = 1;
class A1 {
    public static /*property1*/
}
class A2 {
    public static a/*property2*/
}`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyCompletions(t, f.Markers(), &fourslash.CompletionsExpectedList{
		IsIncomplete: false,
		ItemDefaults: &fourslash.CompletionsExpectedItemDefaults{
			CommitCharacters: &[]string{},
			EditRange:        Ignored,
		},
		Items: &fourslash.CompletionsExpectedItems{
			Exact: CompletionClassElementKeywords,
		},
	})
}
