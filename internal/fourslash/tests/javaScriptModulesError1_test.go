package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestJavaScriptModulesError1(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `// @allowNonTsExtensions: true
// @Filename: Foo.js
define('mod1', ['a'], /**/function(a, b) {
	
});`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.GoToMarker(t, "")
}
