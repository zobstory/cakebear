package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	. "github.com/zobstory/cakebear/internal/fourslash/tests/util"
	"github.com/zobstory/cakebear/internal/lsp/lsproto"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestPathCompletionsPackageJsonExportsBundlerNoNodeCondition(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `// @moduleResolution: bundler
// @Filename: /node_modules/foo/package.json
{
  "name": "foo",
  "exports": {
    "./only-for-node": {
      "node": "./something.js"
    },
    "./for-everywhere": "./other.js"
  }
}
// @Filename: /node_modules/foo/something.d.ts
export const index = 0;
// @Filename: /node_modules/foo/other.d.ts
export const index = 0;
// @Filename: /index.ts
import { } from "foo//**/";`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyCompletions(t, "", &fourslash.CompletionsExpectedList{
		IsIncomplete: false,
		ItemDefaults: &fourslash.CompletionsExpectedItemDefaults{
			CommitCharacters: &[]string{},
			EditRange:        Ignored,
		},
		Items: &fourslash.CompletionsExpectedItems{
			Exact: []fourslash.CompletionsExpectedItem{
				&lsproto.CompletionItem{
					Label:  "for-everywhere",
					Kind:   new(lsproto.CompletionItemKindFile),
					Detail: new("for-everywhere.js"),
				},
			},
		},
	})
}
