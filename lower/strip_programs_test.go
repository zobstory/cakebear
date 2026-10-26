package lower

import (
	"encoding/json"
	"flag"
	goast "go/ast"
	goparser "go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var stripProgramsOut = flag.String("strip-programs-out", "",
	"write the TypeScript programs TestStripGoldensHoldTheTestPrograms finds here, for testdata/gen-strip-goldens.mjs, in place of checking the goldens hold them")

// srcArgs are cakec's test helpers that build a TypeScript program, each
// with the index of the argument holding its source.
var srcArgs = map[string]int{
	"writeTS": 2, "buildProgram": 2, "buildServer": 1, "checkParallel": 1,
	"checkRaceFree": 1, "compileAndRun": 1, "packageProject": 0,
}

type namedProgram struct {
	Name string `json:"name"`
	Src  string `json:"src"`
}

// TestStripGoldensHoldTheTestPrograms holds strip-goldens.jsonl to having
// every program cakec's tests build, every .ts file in examples/, and every
// program the runtime's uncaught goldens record, as a case, so a program
// added since the goldens were recorded fails here until they are recorded
// again.
func TestStripGoldensHoldTheTestPrograms(t *testing.T) {
	t.Parallel()
	programs := cakecTestPrograms(t)
	for _, dir := range []string{"../examples", "../backend/runtime/testdata/uncaught"} {
		programs = append(programs, tsFiles(t, dir)...)
	}
	if *stripProgramsOut != "" {
		data, err := json.Marshal(programs)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(*stripProgramsOut, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	// A floor, so an extraction that stops matching cakec's tests can't
	// pass by finding nothing to hold.
	if len(programs) < 50 {
		t.Errorf("found only %d programs", len(programs))
	}
	recorded := map[string]bool{}
	for _, g := range readStripGoldens(t) {
		recorded[g.Src] = true
	}
	for _, p := range programs {
		if !recorded[p.Src] {
			t.Errorf("%s is no case of testdata/strip-goldens.jsonl; record them again: node lower/testdata/gen-strip-goldens.mjs", p.Name)
		}
	}
}

// cakecTestPrograms returns the sources cmd/cakec's tests build: each
// constant string a helper takes as one (srcArgs), and each a map literal
// gives a .ts file, in source order, once each.
func cakecTestPrograms(t *testing.T) []namedProgram {
	t.Helper()
	paths, err := filepath.Glob("../cmd/cakec/*_test.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("finding cakec's tests: %v", err)
	}
	fset := token.NewFileSet()
	var files []*goast.File
	global := map[string]goast.Expr{}
	for _, path := range paths {
		f, err := goparser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
		for _, d := range f.Decls {
			if gd, ok := d.(*goast.GenDecl); ok {
				constants(gd, global)
			}
		}
	}
	var out []namedProgram
	seen := map[string]bool{}
	for _, f := range files {
		goast.Inspect(f, func(n goast.Node) bool {
			fn, ok := n.(*goast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			local := map[string]goast.Expr{}
			goast.Inspect(fn.Body, func(n goast.Node) bool {
				var src goast.Expr
				switch n := n.(type) {
				case *goast.GenDecl:
					constants(n, local)
				case *goast.AssignStmt:
					if n.Tok == token.DEFINE && len(n.Lhs) == 1 && len(n.Rhs) == 1 {
						if id, ok := n.Lhs[0].(*goast.Ident); ok {
							local[id.Name] = n.Rhs[0]
						}
					}
				case *goast.CallExpr:
					if id, ok := n.Fun.(*goast.Ident); ok {
						if i, ok := srcArgs[id.Name]; ok && i < len(n.Args) {
							src = n.Args[i]
						}
					}
				case *goast.KeyValueExpr:
					if k, ok := constantString(n.Key, local, global); ok && strings.HasSuffix(k, ".ts") && !strings.HasSuffix(k, ".d.ts") {
						src = n.Value
					}
				}
				if s, ok := constantString(src, local, global); ok && !seen[s] {
					seen[s] = true
					at := fset.Position(src.Pos())
					out = append(out, namedProgram{"cmd/cakec/" + filepath.Base(at.Filename) + ":" + strconv.Itoa(at.Line), s})
				}
				return true
			})
			return false
		})
	}
	return out
}

// constants records the constant and variable declarations of d by name.
func constants(d *goast.GenDecl, into map[string]goast.Expr) {
	if d.Tok != token.CONST && d.Tok != token.VAR {
		return
	}
	for _, spec := range d.Specs {
		if vs, ok := spec.(*goast.ValueSpec); ok && len(vs.Names) == len(vs.Values) {
			for i, name := range vs.Names {
				into[name.Name] = vs.Values[i]
			}
		}
	}
}

// constantString evaluates a string built of literals, +, and names
// declared as one, a function's own names first.
func constantString(e goast.Expr, local, global map[string]goast.Expr) (string, bool) {
	switch e := e.(type) {
	case *goast.BasicLit:
		if e.Kind == token.STRING {
			s, err := strconv.Unquote(e.Value)
			return s, err == nil
		}
	case *goast.BinaryExpr:
		if e.Op == token.ADD {
			a, okA := constantString(e.X, local, global)
			b, okB := constantString(e.Y, local, global)
			return a + b, okA && okB
		}
	case *goast.ParenExpr:
		return constantString(e.X, local, global)
	case *goast.Ident:
		if v, ok := local[e.Name]; ok {
			return constantString(v, nil, global)
		}
		if v, ok := global[e.Name]; ok {
			return constantString(v, nil, global)
		}
	}
	return "", false
}

// tsFiles returns every .ts file under dir, in path order.
func tsFiles(t *testing.T, dir string) []namedProgram {
	t.Helper()
	var out []namedProgram
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".ts") {
			return err
		}
		data, err := os.ReadFile(path)
		out = append(out, namedProgram{strings.TrimPrefix(filepath.ToSlash(path), "../"), string(data)})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
