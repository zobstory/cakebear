package lower

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"testing"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/parser"
)

// stripGolden is a source Node 24.18 stripped, from strip-goldens.jsonl
// (testdata/gen-strip-goldens.mjs): its lines, split as V8 numbers them, or
// the error Node refused it with.
type stripGolden struct {
	Section string   `json:"section"`
	Name    string   `json:"name"`
	Src     string   `json:"src"`
	Lines   []string `json:"lines"`
	Refused string   `json:"refused"`
}

// checkerSection holds sources Node refuses or strips otherwise for a
// reason TypeScript reports as an error itself, so lowering never strips
// them: TestStripDifferencesAreTypeScriptErrors holds each to that.
const checkerSection = "TS_ERRORS"

func readStripGoldens(t *testing.T) []stripGolden {
	t.Helper()
	f, err := os.Open("testdata/strip-goldens.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []stripGolden
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<24)
	for sc.Scan() {
		var g stripGolden
		if err := json.Unmarshal(sc.Bytes(), &g); err != nil {
			t.Fatalf("strip-goldens.jsonl line %d: %v", len(out)+1, err)
		}
		out = append(out, g)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// parseTS parses a source as cakec's program does, with nothing else.
func parseTS(src string) *ast.SourceFile {
	return parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/strip.ts", PathKey: "/strip.ts"}, src, core.ScriptKindTS)
}

func TestStripTypesMatchesNode(t *testing.T) {
	t.Parallel()
	counts := map[string]int{}
	for i, g := range readStripGoldens(t) {
		if g.Section == checkerSection {
			continue
		}
		counts[g.Section]++
		got := stripTypes(parseTS(g.Src))
		switch {
		case g.Refused != "" && got != nil:
			t.Errorf("%d %s %s: Node refuses it (%s), stripTypes doesn't:\n%s", i+1, g.Section, g.Name, g.Refused, strconv.Quote(g.Src))
		case g.Refused == "" && !slices.Equal(got, g.Lines):
			t.Errorf("%d %s %s: %s\n%s", i+1, g.Section, g.Name, firstLineDifference(g.Lines, got), strconv.Quote(g.Src))
		}
	}
	t.Logf("cases per section: %v", counts)
}

// TestStripDifferencesAreTypeScriptErrors holds each of checkerSection's
// sources to what puts it there: TypeScript reports an error in it, and
// stripTypes gives what Node doesn't.
func TestStripDifferencesAreTypeScriptErrors(t *testing.T) {
	t.Parallel()
	for i, g := range readStripGoldens(t) {
		if g.Section != checkerSection {
			continue
		}
		program, file := checkSource(t, g.Src)
		diags := append(program.GetSyntacticDiagnostics(t.Context(), file), program.GetSemanticDiagnostics(t.Context(), file)...)
		if len(diags) == 0 {
			t.Errorf("%d %s: TypeScript reports no error in it:\n%s", i+1, g.Name, strconv.Quote(g.Src))
		}
		got := stripTypes(parseTS(g.Src))
		if (g.Refused != "" && got == nil) || (g.Refused == "" && slices.Equal(got, g.Lines)) {
			t.Errorf("%d %s: stripTypes gives what Node does, so it belongs in another section:\n%s", i+1, g.Name, strconv.Quote(g.Src))
		}
	}
}

func firstLineDifference(want, got []string) string {
	if got == nil {
		return "stripTypes refused it"
	}
	for i := range min(len(want), len(got)) {
		if want[i] != got[i] {
			return "line " + strconv.Itoa(i+1) + ": Node " + strconv.Quote(want[i]) + ", stripTypes " + strconv.Quote(got[i])
		}
	}
	return "Node gives " + strconv.Itoa(len(want)) + " lines, stripTypes " + strconv.Itoa(len(got))
}

// TestModulesCarryStrippedLines holds lowering to filling every module's
// Lines, the entry's and its imports', with its file stripped.
func TestModulesCarryStrippedLines(t *testing.T) {
	t.Parallel()
	p, errs := lowerFiles(t, map[string]string{
		"main.ts": "import { twice } from \"./lib\";\nconst n: number = twice(2);\nconsole.log(n);\n",
		"lib.ts":  "export function twice(x: number): number {\n  return x * 2;\n}\n",
	}, "main.ts")
	if len(errs) > 0 {
		t.Fatalf("lowering refused: %v", errs)
	}
	want := map[string][]string{
		"main": {"import { twice } from \"./lib\";", "const n         = twice(2);", "console.log(n);", ""},
		"lib":  {"export function twice(x        )         {", "  return x * 2;", "}", ""},
	}
	for _, m := range p.Modules {
		if !slices.Equal(m.Lines, want[m.Name]) {
			t.Errorf("module %s: Lines %q, want %q", m.Name, m.Lines, want[m.Name])
		}
	}
}
