package ir

import (
	"slices"
	"strconv"
	"strings"
)

// Field is one property of an object shape.
type Field struct {
	Name string
	Type Type
	// Optional marks `name?: T` (M5 plan D3): an object may leave it out,
	// and whether one has it is its layout's business, as Node prints and
	// serialises only the keys an object has. Type is then `T | null |
	// undefined`, as TypeScript reads an optional property. `{ a?: string }`
	// and `{ a: string | undefined }` are two shapes: Node prints
	// `{ a: undefined }` for the second and `{}` for the first.
	Optional bool
}

// Shape is the structure of an object type: its fields, sorted by name (M4
// plan D3). Shapes are interned (Shapes), so one structure is one *Shape in
// the whole program, and an object type's identity is its shape's:
// `interface Point { x: number; y: number }` and `type Vec = { y: number;
// x: number }` are one shape, so a Point flows into a Vec unchanged, and `===`
// stays an identity test across the flow.
//
// The order is canonical, not the source's. The order a literal writes its
// keys in, which is the order Node prints them and JSON.stringify writes
// them, belongs to the literal (ObjectLit): `{ y, x }` written for a Point is
// still a Point.
type Shape struct {
	// ID is the shape's index in Program.Shapes.
	ID     int
	Fields []Field
	// Names are the interfaces and type aliases that declare the shape, for
	// messages only: a shape is its fields, whatever it was called.
	Names []string
	// pending marks a shape reserved and not yet defined (Shapes.Reserve).
	pending bool
}

// Index returns the index in Fields of the field called name, or -1.
func (s *Shape) Index(name string) int {
	i, ok := slices.BinarySearchFunc(s.Fields, name, func(f Field, name string) int { return strings.Compare(f.Name, name) })
	if !ok {
		return -1
	}
	return i
}

// String names the shape by the first declaration that named it, or spells
// it as TypeScript would: `{ x: number; y: number }`.
func (s *Shape) String() string {
	if s == nil {
		return "{ invalid }"
	}
	if len(s.Names) > 0 {
		return s.Names[0]
	}
	if len(s.Fields) == 0 {
		return "{}"
	}
	// A recursive type needs a name to refer to itself by, so a shape spelled
	// out here never reaches itself.
	parts := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		if f.Optional && f.Type.Kind == KindMaybe && f.Type.Elem != nil {
			parts[i] = f.Name + "?: " + f.Type.Elem.String()
			continue
		}
		parts[i] = f.Name + ": " + f.Type.String()
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

func (s *Shape) addName(name string) {
	if name != "" && !slices.Contains(s.Names, name) {
		s.Names = append(s.Names, name)
	}
}

// Shapes interns a program's shapes. Lowering keeps one for the whole program,
// since modules share shapes, and the order shapes are first met in, which
// lowering keeps deterministic, is their order in Program.Shapes.
type Shapes struct {
	list  []*Shape
	byKey map[string]*Shape
}

// Intern returns the one shape with these fields, in whatever order they are
// given, adding it if it is new. name, if not empty, joins its Names.
func (s *Shapes) Intern(name string, fields []Field) *Shape {
	sorted := sortedFields(fields)
	key := shapeKey(sorted)
	if sh, ok := s.byKey[key]; ok {
		sh.addName(name)
		return sh
	}
	sh := s.add(name)
	sh.Fields = sorted
	s.byKey[key] = sh
	return sh
}

// Reserve adds a shape whose fields aren't known yet, for a type that refers
// to itself (`interface Tree { kids: Tree[] }`): the types of its fields need
// the shape before its fields can be interned. Define gives it its fields.
//
// A recursive shape is not merged with another, so two declarations of one
// recursive structure are two shapes, where TypeScript would call them one
// type. A flow between them is lowering's to refuse.
func (s *Shapes) Reserve(name string) *Shape {
	sh := s.add(name)
	sh.pending = true
	return sh
}

// Define gives a reserved shape its fields. If a shape interned while sh was
// pending already has exactly these fields, Define returns it: the two are
// one structure, but references to each exist, so merging them would mean
// rewriting those. Lowering refuses such a program by name, and Validate
// reports the pair if it doesn't.
func (s *Shapes) Define(sh *Shape, fields []Field) (same *Shape) {
	if !sh.pending {
		panic("ir: Define of a shape that is not reserved, or defined twice")
	}
	sh.Fields = sortedFields(fields)
	sh.pending = false
	key := shapeKey(sh.Fields)
	if other, ok := s.byKey[key]; ok {
		return other
	}
	s.byKey[key] = sh
	return nil
}

// All returns the shapes in the order they were added, which is their order
// in Program.Shapes.
func (s *Shapes) All() []*Shape { return s.list }

func (s *Shapes) add(name string) *Shape {
	if s.byKey == nil {
		s.byKey = make(map[string]*Shape)
	}
	sh := &Shape{ID: len(s.list)}
	sh.addName(name)
	s.list = append(s.list, sh)
	return sh
}

func sortedFields(fields []Field) []Field {
	sorted := slices.Clone(fields)
	slices.SortStableFunc(sorted, func(a, b Field) int { return strings.Compare(a.Name, b.Name) })
	return sorted
}

// shapeKey identifies a structure: its fields' names and types, in canonical
// order. A field holding an object is keyed by its shape's ID, so a key is
// structural one level down and nominal below that, which interning makes
// the same thing.
func shapeKey(fields []Field) string {
	var b strings.Builder
	for _, f := range fields {
		b.WriteString(strconv.Quote(f.Name))
		if f.Optional {
			b.WriteByte('?')
		}
		b.WriteByte(':')
		writeTypeKey(&b, f.Type)
		b.WriteByte(';')
	}
	return b.String()
}

func writeTypeKey(b *strings.Builder, t Type) {
	b.WriteString(strconv.Itoa(int(t.Kind)))
	switch t.Kind {
	case KindHost:
		b.WriteString("h" + strconv.Itoa(int(t.Host)))
	case KindObject:
		if t.Shape == nil {
			b.WriteString("#nil")
			return
		}
		b.WriteString("#" + strconv.Itoa(t.Shape.ID))
	case KindPromise, KindArray, KindMaybe:
		b.WriteByte('<')
		if t.Elem != nil {
			writeTypeKey(b, *t.Elem)
		}
		b.WriteByte('>')
	case KindFunc:
		b.WriteByte('(')
		if t.Sig != nil {
			for _, p := range t.Sig.Params {
				writeTypeKey(b, p)
				b.WriteByte(',')
			}
			b.WriteString(")")
			writeTypeKey(b, t.Sig.Result)
		}
	}
}
