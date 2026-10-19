package ir

import (
	"strings"
	"testing"
)

func pointShapes() (*Shapes, *Shape) {
	var shapes Shapes
	return &shapes, shapes.Intern("Point", []Field{{Name: "y", Type: Number}, {Name: "x", Type: Number}})
}

// TestInterning: one structure is one shape, whatever order its fields come
// in and whatever declares it, and its fields are kept sorted.
func TestInterning(t *testing.T) {
	t.Parallel()

	shapes, point := pointShapes()
	vec := shapes.Intern("Vec", []Field{{Name: "x", Type: Number}, {Name: "y", Type: Number}})
	named := shapes.Intern("", []Field{{Name: "name", Type: String}})
	if vec != point {
		t.Fatalf("Point and Vec are two shapes, want one")
	}
	if named == point || named.ID != 1 || point.ID != 0 {
		t.Errorf("IDs = %d, %d; want 0 for Point and 1 for { name }", point.ID, named.ID)
	}
	if got := point.Fields[0].Name + point.Fields[1].Name; got != "xy" {
		t.Errorf("fields in order %q, want x then y", got)
	}
	if point.Index("y") != 1 || point.Index("x") != 0 || point.Index("z") != -1 {
		t.Errorf("Index(y, x, z) = %d, %d, %d; want 1, 0, -1", point.Index("y"), point.Index("x"), point.Index("z"))
	}
	if got := strings.Join(point.Names, ","); got != "Point,Vec" {
		t.Errorf("Names = %q, want both declarations", got)
	}
	if shapes.Intern("", []Field{{Name: "x", Type: String}, {Name: "y", Type: Number}}) == point {
		t.Errorf("a field of another type interned as the same shape")
	}
	if got := len(shapes.All()); got != 3 {
		t.Errorf("%d shapes, want 3", got)
	}
}

func TestObjectAndArrayTypeStrings(t *testing.T) {
	t.Parallel()

	shapes, point := pointShapes()
	anon := shapes.Intern("", []Field{{Name: "at", Type: ObjectOf(point)}, {Name: "tags", Type: ArrayOf(String)}})
	for _, tt := range []struct {
		in   Type
		want string
	}{
		{ObjectOf(point), "Point"},
		{ObjectOf(anon), "{ at: Point; tags: string[] }"},
		{ArrayOf(ArrayOf(Number)), "number[][]"},
		{ArrayOf(FuncType(nil, Void)), "(() => void)[]"},
		{Type{Kind: KindArray}, "invalid[]"},
	} {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestTypeEqualObjectsAndArrays(t *testing.T) {
	t.Parallel()

	shapes, point := pointShapes()
	other := shapes.Intern("", []Field{{Name: "x", Type: Number}})
	for _, tt := range []struct {
		a, b Type
		want bool
	}{
		{ObjectOf(point), ObjectOf(point), true},
		{ObjectOf(point), ObjectOf(other), false},
		// Two ArrayOf calls give two Elem pointers; Equal looks through them.
		{ArrayOf(Number), ArrayOf(Number), true},
		{ArrayOf(Number), ArrayOf(String), false},
		{ArrayOf(ObjectOf(point)), ArrayOf(ObjectOf(point)), true},
		{ArrayOf(Number), PromiseOf(Number), false},
	} {
		if got := tt.a.Equal(tt.b); got != tt.want {
			t.Errorf("%s.Equal(%s) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// TestRecursiveShape: a type that refers to itself is reserved, then
// defined, and a program holding it validates.
func TestRecursiveShape(t *testing.T) {
	t.Parallel()

	var shapes Shapes
	tree := shapes.Reserve("Tree")
	if same := shapes.Define(tree, []Field{{Name: "label", Type: String}, {Name: "kids", Type: ArrayOf(ObjectOf(tree))}}); same != nil {
		t.Fatalf("Define returned %s, want nil", same)
	}
	if got := tree.Fields[0].Type.String(); got != "Tree[]" {
		t.Errorf("kids is a %q, want Tree[]", got)
	}
	p := &Program{Modules: []*Module{{Name: "t"}}, Shapes: shapes.All()}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Errorf("a second Define didn't panic")
		}
	}()
	shapes.Define(tree, nil)
}

// TestDefineReportsTheSameStructure: a shape interned while another was
// pending, with the fields the pending one turns out to have, is the same
// structure, and Define says so.
func TestDefineReportsTheSameStructure(t *testing.T) {
	t.Parallel()

	var shapes Shapes
	a := shapes.Reserve("A")
	c := shapes.Intern("C", []Field{{Name: "n", Type: Number}})
	if same := shapes.Define(a, []Field{{Name: "n", Type: Number}}); same != c {
		t.Errorf("Define returned %v, want C", same)
	}
	p := &Program{Modules: []*Module{{Name: "t"}}, Shapes: shapes.All()}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "have the same fields") {
		t.Errorf("Validate = %v, want the two shapes reported", err)
	}
}

func TestArrayMethodNames(t *testing.T) {
	t.Parallel()

	for m := ArrayLength; m <= ArrayIndexOf; m++ {
		if got := m.String(); got == "" || strings.Contains(got, "invalid") {
			t.Errorf("ArrayMethod(%d) has no name: %q", m, got)
		}
	}
	if got := ArrayMethodInvalid.String(); got != "invalid array method" {
		t.Errorf("the zero method prints %q", got)
	}
}
