package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/ls/lsutil"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestInlayHintsInteractiveAnyParameter2(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `function foo (v: any) {}
foo(1);
foo('');
foo(true);
foo(() => 1);
foo(function () { return 1 });
foo({});
foo({ a: 1 });
foo([]);
foo([1]);
foo(foo);
foo((1));
foo(foo(1));`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyBaselineInlayHints(t, nil /*span*/, &lsutil.UserPreferences{IncludeInlayParameterNameHints: lsutil.IncludeInlayParameterNameHintsAll})
}
