package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	. "github.com/zobstory/cakebear/internal/fourslash/tests/util"
	"github.com/zobstory/cakebear/internal/ls/lsutil"
	"github.com/zobstory/cakebear/internal/lsp/lsproto"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestCompletionForStringLiteral_quotePreference3(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `const a = {
    "#": "a"
};
a[|./**/|]`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyCompletions(t, "", &fourslash.CompletionsExpectedList{
		IsIncomplete: false,
		ItemDefaults: &fourslash.CompletionsExpectedItemDefaults{
			CommitCharacters: &DefaultCommitCharacters,
			EditRange:        Ignored,
		},
		Items: &fourslash.CompletionsExpectedItems{
			Includes: []fourslash.CompletionsExpectedItem{
				&lsproto.CompletionItem{
					Label:      "#",
					InsertText: new("[\"#\"]"),
					TextEdit: &lsproto.TextEditOrInsertReplaceEdit{
						TextEdit: &lsproto.TextEdit{
							NewText: "[\"#\"]",
							Range:   f.Ranges()[0].LSRange,
						},
					},
				},
			},
		},
		UserPreferences: &lsutil.UserPreferences{QuotePreference: lsutil.QuotePreference("double")},
	})
}
