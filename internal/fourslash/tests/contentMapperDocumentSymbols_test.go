package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/testutil"
	"github.com/zobstory/cakebear/internal/testutil/contentmappertest"
)

func TestContentMapperSynthesizedDocumentSymbols(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	f, done := newContentMapperFourslash(t, `// @Filename: /app.vue
component source with no direct TypeScript span/**/
`, contentmappertest.SynthesizingMapper, ".vue")
	defer done()

	f.GoToMarker(t, "")
	f.VerifyBaselineDocumentSymbol(t)
}

func TestContentMapperSupplementalDocumentSymbols(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	f, done := newContentMapperFourslash(t, `// @Filename: /app.astro
export function supplementalSymbol() {}
`, contentmappertest.SupplementalMapper, ".astro")
	defer done()

	f.GoToFile(t, "/app.astro")
	f.VerifyBaselineDocumentSymbol(t)
}
