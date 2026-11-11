package runtime

import "testing"

// TestJSONParseReportsPointIntoTheData holds the report of an uncaught
// JSON.parse SyntaxError to Node's (R30): `<anonymous_script>:L`, the
// data's line whole, a caret at its column in UTF-16 units with a tab where
// the line has one, and none for the end of the input. Each want is what
// Node 24.18 wrote to stderr for s1-node's case, under
// --stack-trace-limit=0, its last line `Node.js v24.18.0`.
func TestJSONParseReportsPointIntoTheData(t *testing.T) {
	t.Parallel()
	program := &Site{File: "/src/main.ts", Line: 3, Col: 20, Text: "const v: unknown = JSON.parse(s);"}
	for _, tt := range []struct{ name, input, node string }{
		{"28-u-json-parse", "{bad", "<anonymous_script>:1\n{bad\n ^\n\n[SyntaxError: Expected property name or '}' in JSON at position 1 (line 1 column 2)]"},
		{"38-u-json-parse-multiline", "{\n  \"a\": 1,\n  \"b\": ,\n  \"c\": 3\n}", "<anonymous_script>:3\n  \"b\": ,\n       ^\n\n[SyntaxError: Unexpected token ',', ...\"1,\n  \"b\": ,\n  \"c\": 3\"... is not valid JSON]"},
		{"40-u-json-empty", "", "<anonymous_script>:1\n\n\n[SyntaxError: Unexpected end of JSON input]"},
		{"46-u-json-crlf", "{\r\n  \"a\": 1,\r\n  \"b\": ,\r\n}", "<anonymous_script>:3\n  \"b\": ,\n       ^\n\n[SyntaxError: Unexpected token ',', ...\",\r\n  \"b\": ,\r\n}\" is not valid JSON]"},
		{"47-u-json-eof", "{\"a\":", "<anonymous_script>:1\n{\"a\":\n\n[SyntaxError: Unexpected end of JSON input]"},
		{"48-u-json-unicode-caret", "[\"é😀日\", 1 2]", "<anonymous_script>:1\n[\"é😀日\", 1 2]\n           ^\n\n[SyntaxError: Expected ',' or ']' after array element in JSON at position 11 (line 1 column 12)]"},
		{"49-u-json-tab-caret", "[\t1,\t2 3]", "<anonymous_script>:1\n[\t1,\t2 3]\n \t  \t  ^\n\n[SyntaxError: Expected ',' or ']' after array element in JSON at position 7 (line 1 column 8)]"},
	} {
		_, err := parseJSON(tt.input)
		if err == nil {
			t.Fatalf("%s: parsed", tt.name)
		}
		for _, p := range []phase{inModule, inTimer, inHandler} {
			got := uncaughtReport(&Thrown{V: err, Site: program}, p)
			if want := tt.node + versionLines; got != want {
				t.Errorf("%s: the report is\n%s\nwant, as Node's\n%s", tt.name, got, want)
			}
		}
	}
}
