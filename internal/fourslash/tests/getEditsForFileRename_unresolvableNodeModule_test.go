package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestGetEditsForFileRename_unresolvableNodeModule(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `// @allowJs: true
// @checkJs: true
// @Filename: /modules/@app/something/index.js
import "doesnt-exist";
// @Filename: /modules/@local/foo.js
import "doesnt-exist"; `
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyWillRenameFilesEdits(t, "/modules/@app/something", "/modules/@app/something-2", map[string]string{}, nil /*preferences*/)
}
