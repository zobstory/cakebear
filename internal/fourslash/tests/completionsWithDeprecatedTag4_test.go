package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	. "github.com/zobstory/cakebear/internal/fourslash/tests/util"
	"github.com/zobstory/cakebear/internal/ls"
	"github.com/zobstory/cakebear/internal/lsp/lsproto"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestCompletionsWithDeprecatedTag4(t *testing.T) {
	t.Parallel()

	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `// @noLib: true
f({
    [|a|]/**/
    xyz: ` + "`" + `` + "`" + `,
});
declare function f(options: {
    /** @deprecated abc */
    abc?: number,
    xyz?: string
}): void;`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyCompletions(t, "", &fourslash.CompletionsExpectedList{
		IsIncomplete: false,
		ItemDefaults: &fourslash.CompletionsExpectedItemDefaults{
			CommitCharacters: &DefaultCommitCharacters,
			EditRange:        Ignored,
		},
		Items: &fourslash.CompletionsExpectedItems{
			Exact: []fourslash.CompletionsExpectedItem{
				&lsproto.CompletionItem{
					Label:      "abc?",
					FilterText: new("abc"),
					Kind:       new(lsproto.CompletionItemKindField),
					Tags:       &[]lsproto.CompletionItemTag{lsproto.CompletionItemTagDeprecated},
					SortText:   new(string(ls.DeprecateSortText(ls.SortTextOptionalMember))),
					TextEdit: &lsproto.TextEditOrInsertReplaceEdit{
						InsertReplaceEdit: &lsproto.InsertReplaceEdit{
							NewText: "abc",
							Insert:  f.Ranges()[0].LSRange,
							Replace: f.Ranges()[0].LSRange,
						},
					},
				},
			},
		},
	})
}
