package ir

import (
	"strings"
	"testing"
)

// TestStringParts: a chain of string `+` and the templates in it flatten to
// their pieces in order; `+` on numbers, and anything else, is one piece.
func TestStringParts(t *testing.T) {
	t.Parallel()

	s := &Ident{Name: "s", Typ: String}
	add := func(a, b Expr, typ Type) Expr { return &Binary{Op: OpAdd, Left: a, Right: b, Typ: typ} }
	chain := add(add(s, str("a"), String), &Concat{Parts: []Expr{str("b"), add(str("c"), str("d"), String)}}, String)
	got := StringParts(chain)
	var names []string
	for _, p := range got {
		switch p := p.(type) {
		case *Ident:
			names = append(names, p.Name)
		case *StringLit:
			names = append(names, p.Value)
		default:
			names = append(names, "?")
		}
	}
	if strings.Join(names, ",") != "s,a,b,c,d" {
		t.Errorf("StringParts = %v, want s,a,b,c,d", names)
	}
	if sum := add(n(1), n(2), Number); len(StringParts(sum)) != 1 {
		t.Errorf("a number's + flattened to %d pieces, want 1", len(StringParts(sum)))
	}
}

// TestValidateChecksAppends: an append is to a string binding marked
// Appended, of string pieces.
func TestValidateChecksAppends(t *testing.T) {
	t.Parallel()

	prog := func(s *Append) *Program {
		decl := &VarDecl{Name: "s", Type: String, Init: str(""), Mutable: true, Appended: true}
		return ProgramOf(&Module{Name: "t", Main: []Stmt{decl, s}})
	}
	marked := &Ident{Name: "s", Typ: String, Appended: true}
	for _, tt := range []struct {
		name string
		s    *Append
		want string
	}{
		{"an unmarked binding", &Append{Target: &Ident{Name: "s", Typ: String}, Parts: []Expr{str("a")}}, "not marked Appended"},
		{"a number binding", &Append{Target: &Ident{Name: "s", Typ: Number, Appended: true}, Parts: []Expr{str("a")}}, "an append to s, a number"},
		{"a number piece", &Append{Target: marked, Parts: []Expr{n(1)}}, "part 0 appended to s is a number"},
		{"no pieces", &Append{Target: marked}, "an append to s of nothing"},
	} {
		if err := prog(tt.s).Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Validate = %v, want an error containing %q", tt.name, err, tt.want)
		}
	}
	if err := prog(&Append{Target: marked, Parts: []Expr{str("a")}}).Validate(); err != nil {
		t.Errorf("a well-formed append: %v", err)
	}
}
