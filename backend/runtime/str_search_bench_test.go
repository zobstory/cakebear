package runtime

import (
	"fmt"
	"strings"
	"testing"
)

// The search methods with an ASCII pattern, over haystacks of 1 KB to 1 MB,
// ASCII and not. Each haystack ends in "needle", so includes, indexOf and
// replace scan all of it, and "from mid" starts halfway, at a "temp=".
func BenchmarkStrSearch(b *testing.B) {
	chunks := map[string]string{
		"ascii":    "name=Weather today, temp=72F, wind=NW 10 mph; ",
		"nonascii": "nom=Météo du jour, temp=22°C, vent=NO 16 km/h, 日本 😀; ",
	}
	for _, kind := range []string{"ascii", "nonascii"} {
		for _, size := range []int{1 << 10, 1 << 16, 1 << 20} {
			chunk := chunks[kind]
			hay := strings.Repeat(chunk, size/len(chunk)) + "needle"
			mid := StrIndexOf(hay, "temp=", StrLength(hay)/2)
			for _, c := range []struct {
				name string
				run  func()
			}{
				{"includes", func() { StrIncludes(hay, "needle") }},
				{"indexOf", func() { StrIndexOf(hay, "needle") }},
				{"indexOf from mid", func() { StrIndexOf(hay, "needle", mid) }},
				{"lastIndexOf", func() { StrLastIndexOf(hay, "temp=") }},
				{"startsWith at mid", func() { StrStartsWith(hay, "temp=", mid) }},
				{"endsWith", func() { StrEndsWith(hay, "needle") }},
				{"split", func() { StrSplit(hay, ", ") }},
				{"replace", func() { StrReplace(hay, "needle", "pin") }},
				{"replaceAll", func() { StrReplaceAll(hay, ", ", " | ") }},
			} {
				b.Run(fmt.Sprintf("%s/%s/%dKB", c.name, kind, size>>10), func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						c.run()
					}
				})
			}
		}
	}
}
