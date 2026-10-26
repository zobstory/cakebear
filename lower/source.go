package lower

import (
	"sort"
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
)

// describeKind names an AST node kind in words a TypeScript author recognises,
// rather than leaking the compiler's internal spelling into a diagnostic.
func describeKind(k ast.Kind) string {
	if k == ast.KindTypeOfExpression { // anywhere but `typeof u === "tag"` (errors.go)
		return "typeof as a value, not compared with a string literal,"
	}
	s := k.String()
	s = strings.TrimPrefix(s, "Kind")
	var out strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out.WriteByte(' ')
		}
		out.WriteRune(r)
	}
	return strings.ToLower(out.String())
}

// lineAndColumn resolves a source position to 0-based line and column.
//
// ECMALineMap holds the offset each line starts at, so the line containing pos
// is the last entry not greater than pos, and the column is the remainder.
func lineAndColumn(file *ast.SourceFile, pos int) (line, col int) {
	starts := file.ECMALineMap()
	if len(starts) == 0 || pos < 0 {
		return 0, 0
	}
	// The first start strictly greater than pos, minus one, is our line.
	i := sort.Search(len(starts), func(i int) bool { return int(starts[i]) > pos })
	if i == 0 {
		return 0, pos
	}
	line = i - 1
	return line, pos - int(starts[line])
}
