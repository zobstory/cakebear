package fourslash_test

import (
	"testing"

	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/fourslash"
	"github.com/zobstory/cakebear/internal/testutil"
)

func TestFormatTypeAnnotation1(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `function foo(x: number, y?: string): number {}
interface Foo {
    x: number;
    y?: number;
}`
	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()
	opts207 := f.GetOptions()
	opts207.FormatCodeSettings.InsertSpaceBeforeTypeAnnotation = core.TSTrue
	f.Configure(t, opts207)
	f.FormatDocument(t, "")
	f.VerifyCurrentFileContent(t, `function foo(x : number, y ?: string) : number { }
interface Foo {
    x : number;
    y ?: number;
}`)
}
