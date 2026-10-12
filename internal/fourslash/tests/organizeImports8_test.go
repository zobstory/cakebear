package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/lsp/lsproto"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestOrganizeImports8(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `import { foo as foo } from "foo";
foo;`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyOrganizeImports(t,
		`import { foo } from "foo";
foo;`,
		lsproto.CodeActionKindSourceOrganizeImportsTs,
		nil,
	)
}
