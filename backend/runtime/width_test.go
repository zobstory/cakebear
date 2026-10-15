package runtime

import "testing"

// TestDisplayWidth: widths Node 24.18's getStringWidth(s, false) gives.
func TestDisplayWidth(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		s    string
		want int
	}{
		{"abc", 3},
		{"日本", 4},
		{"é", 1},
		{"é", 1},
		{"\U0001f44d", 2},
		{"\U0001f468‍\U0001f469‍\U0001f467", 6},
		{"한", 2},
		{"café", 4},
		{"​", 0},
		{"\U0001D160", 3}, // NFC decomposes it, and ICU measures the parts
	} {
		if got := displayWidth(tt.s); got != tt.want {
			t.Errorf("displayWidth(%q) = %d, want %d", tt.s, got, tt.want)
		}
	}
}

// TestDisplayWidthComposes: what NFC composes into a character of another
// width is measured as that character, as Node measures it. Each want is
// Node's width for the text as written.
func TestDisplayWidthComposes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		s    string
		want int
	}{
		{"jamo L V T", "각", 2},
		{"jamo L V", "가", 2},
		{"an LV syllable and a T jamo", "각", 2},
		{"an LVT syllable and a T jamo, which doesn't compose", "각ᆨ", 3},
		{"Bengali e + aa", "ো", 1},
		{"ka + a voiced mark", "が", 2},
		{"two syllables in jamo", "가각", 4},
	} {
		if got := displayWidth(tt.s); got != tt.want {
			t.Errorf("%s: displayWidth(%q) = %d, want %d", tt.name, tt.s, got, tt.want)
		}
	}
}
