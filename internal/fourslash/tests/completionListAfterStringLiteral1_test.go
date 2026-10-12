package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	. "github.com/zobstory/cakebear/internal/fourslash/tests/util"
	"github.com/zobstory/cakebear/internal/ls"
	"github.com/zobstory/cakebear/internal/lsp/lsproto"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestCompletionListAfterStringLiteral1(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `// @lib: es5
"a"./**/`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyCompletions(t, "", &fourslash.CompletionsExpectedList{
		IsIncomplete: false,
		ItemDefaults: &fourslash.CompletionsExpectedItemDefaults{
			CommitCharacters: &DefaultCommitCharacters,
			EditRange:        Ignored,
		},
		Items: &fourslash.CompletionsExpectedItems{
			Unsorted: []fourslash.CompletionsExpectedItem{
				"toString",
				"charAt",
				"charCodeAt",
				"concat",
				"indexOf",
				"lastIndexOf",
				"localeCompare",
				"match",
				"replace",
				"search",
				"slice",
				"split",
				"substring",
				"toLowerCase",
				"toLocaleLowerCase",
				"toUpperCase",
				"toLocaleUpperCase",
				"trim",
				"length",
				&lsproto.CompletionItem{
					Label:    "substr",
					SortText: new(string(ls.DeprecateSortText(ls.SortTextLocationPriority))),
					Tags:     &[]lsproto.CompletionItemTag{lsproto.CompletionItemTagDeprecated},
				},
				"valueOf",
			},
		},
	})
}
