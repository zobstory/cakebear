package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestDoubleUnderscoreRenames(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `// @Filename: fileA.ts
[|export function [|{| "contextRangeIndex": 0 |}__foo|]() {
}|]

// @Filename: fileB.ts
[|import { [|{| "contextRangeIndex": 2 |}__foo|] as bar } from "./fileA";|]

bar();`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	f.VerifyBaselineRenameAtRangesWithText(t, nil /*preferences*/, "__foo")
}
