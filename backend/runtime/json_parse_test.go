package runtime

import (
	"bufio"
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

// goldenText is a string of a golden line: plain, or {"u":[…]} for one
// holding a lone surrogate.
type goldenText string

func (g *goldenText) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*g = goldenText(s)
		return nil
	}
	var u struct {
		U []uint16 `json:"u"`
	}
	if err := json.Unmarshal(b, &u); err != nil {
		return err
	}
	*g = goldenText(wtf8(u.U))
	return nil
}

// TestJSONParseGoldens holds JSONParse to what Node 24.18's JSON.parse made
// of 9,674 inputs (testdata/gen-json-parse-goldens.mjs): for a value, how it
// prints and serialises; for a failure, V8's message, byte for byte.
func TestJSONParseGoldens(t *testing.T) {
	t.Parallel()
	f, err := os.Open("testdata/json-parse-goldens.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	lines, bad := 0, 0
	for sc.Scan() {
		lines++
		var g struct {
			In      goldenText  `json:"in"`
			Inspect *goldenText `json:"inspect"`
			JSON    *goldenText `json:"json"`
			Err     *goldenText `json:"err"`
		}
		if err := json.Unmarshal(sc.Bytes(), &g); err != nil {
			t.Fatalf("line %d: %v", lines, err)
		}
		v, perr := parseJSON(string(g.In))
		var got, want string
		switch {
		case g.Err != nil && perr == nil:
			got, want = "a value: "+Inspect(v), string(*g.Err)
		case g.Err != nil:
			got, want = perr.Message(), string(*g.Err)
		case perr != nil:
			got, want = "SyntaxError: "+perr.Message(), "a value: "+string(*g.Inspect)
		default:
			got = Inspect(v) + "\n" + JSONStringify(v, JSONGap{}, nil)
			want = string(*g.Inspect) + "\n" + string(*g.JSON)
		}
		if got != want {
			if bad++; bad <= 20 {
				t.Errorf("JSON.parse(%s)\n got  %s\n want %s", strconv.Quote(string(g.In)), strconv.Quote(got), strconv.Quote(want))
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if lines != 9674 || bad > 0 {
		t.Errorf("%d of %d inputs differ from Node's (want 9674 inputs)", bad, lines)
	}
}
