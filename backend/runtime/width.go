package runtime

import "sort"

// Display width, which Node groups an array's entries by (R10): the columns
// a terminal gives a string. inspect.js's getStringWidth counts printable
// ASCII as 1 and hands anything else to ICU, which gives East Asian wide and
// full-width characters and emoji 2, and combining marks, controls and
// format characters 0. The tables in width_table.go hold ICU's answer for
// every code point, as Node 24.18 gives it (scripts/gen-width-table.mjs),
// including the few that NFC decomposes, which ICU measures as what they
// decompose into: U+1D160, a musical note, is 3 columns.
//
// Node measures a string's NFC form: summing the table over it gives Node's
// width exactly. Only a composition can change the sum, and only two kinds
// do: conjoining Hangul jamo composing into a syllable, which is algorithmic,
// and the pairs in composes, such as Bengali's two-part vowels and kana with
// a combining voiced mark. displayWidth composes those as it goes rather than
// carry NFC's tables, which change nothing else about the width.

// displayWidth is getStringWidth(s, false): the width of an entry as
// grouping measures it.
func displayWidth(s string) int {
	n := 0
	prev := rune(-1)
	for _, r := range s {
		if c, ok := compose(prev, r); ok {
			n += runeWidth(c) - runeWidth(prev)
			prev = c
			continue
		}
		n += runeWidth(r)
		prev = r
	}
	return n
}

// Conjoining Hangul jamo, and the syllables they compose into (Unicode
// §3.12): a leading consonant and a vowel make an LV syllable, and an LV
// syllable and a trailing consonant an LVT one.
const (
	hangulL, hangulLCount = 0x1100, 19
	hangulV, hangulVCount = 0x1161, 21
	hangulT, hangulTCount = 0x11A7, 28 // T counts the syllable with none
	hangulS, hangulSCount = 0xAC00, hangulLCount * hangulVCount * hangulTCount
)

// compose is what NFC composes prev and r into, where that changes the width.
func compose(prev, r rune) (rune, bool) {
	switch {
	case prev >= hangulL && prev < hangulL+hangulLCount && r >= hangulV && r < hangulV+hangulVCount:
		return hangulS + ((prev-hangulL)*hangulVCount+(r-hangulV))*hangulTCount, true
	case prev >= hangulS && prev < hangulS+hangulSCount && (prev-hangulS)%hangulTCount == 0 &&
		r > hangulT && r < hangulT+hangulTCount:
		return prev + (r - hangulT), true
	}
	c, ok := composes[[2]rune{prev, r}]
	return c, ok
}

func runeWidth(r rune) int {
	switch {
	case r >= 0x20 && r < 0x7f:
		return 1
	case inRanges(zeroWidth, r):
		return 0
	case inRanges(wideWidth, r):
		return 2
	}
	if w, ok := otherWidth[r]; ok {
		return w
	}
	return 1
}

// widthRange is an inclusive range of code points.
type widthRange struct{ lo, hi rune }

func inRanges(ranges []widthRange, r rune) bool {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].hi >= r })
	return i < len(ranges) && ranges[i].lo <= r
}
