package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/ls/lsutil"
	"github.com/zobstory/cakebear/internal/lsp/lsproto"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestOrganizeImportsUnicode3(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `import {
    B,
    À,
    A,
} from './foo';

console.log(A, À, B);`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyOrganizeImports(t,
		`import {
    A,
    À,
    B,
} from './foo';

console.log(A, À, B);`,
		lsproto.CodeActionKindSourceOrganizeImports,
		&lsutil.UserPreferences{
			OrganizeImportsIgnoreCase:      core.TSFalse,
			OrganizeImportsCollation:       lsutil.OrganizeImportsCollationUnicode,
			OrganizeImportsAccentCollation: core.TSFalse,
		},
	)
	f.VerifyOrganizeImports(t,
		`import {
    A,
    À,
    B,
} from './foo';

console.log(A, À, B);`,
		lsproto.CodeActionKindSourceOrganizeImports,
		&lsutil.UserPreferences{
			OrganizeImportsIgnoreCase:      core.TSFalse,
			OrganizeImportsCollation:       lsutil.OrganizeImportsCollationUnicode,
			OrganizeImportsAccentCollation: core.TSTrue,
		},
	)
}
