package runtime

import (
	"slices"
	"sync"
	"sync/atomic"
)

// Objects and the clone (M4 plan D2, D4, D11).
//
// The backend generates a struct for each shape and makes it an Object: the
// runtime never knows a shape's fields, only how to list them for printing
// and how to ask an object to copy itself.

// Object is implemented by the struct the backend generates for each shape.
// An object is always a pointer to one, so `===` is pointer equality.
type Object interface {
	// Layout is the key table the object's literal chose: its keys in the
	// order the literal wrote them, which is the order Node prints them in.
	Layout() *Layout
	// Field is the value of the shape's field i, in the shape's own order.
	Field(i int) any
	// CloneObject copies the object for a Cloner: it allocates the copy,
	// calls c.Remember(original, copy) before copying any field, and copies
	// each field that holds an object or an array through CloneValue.
	CloneObject(c *Cloner) Object
}

// Layout is a key table: the keys of one object literal, in the order it
// wrote them (D4). Every object created at that literal shares it.
//
// An object with optional properties changes its table as it gains and
// loses them (M5 plan D3): `o.p = v` for a p it doesn't have appends the
// key, as Node orders it, and `delete o.p` removes it (With, Without). An
// object leaves the table it was made with for another, and objects that
// take the same steps share the table they reach, as V8's hidden classes
// do.
type Layout struct {
	keys    []string // as written
	fields  []int    // the shape field each key names
	printed []string // each key as Node prints it (R5)
	// edges are the tables With and Without have made from this one, so
	// each is made once. Every request's goroutine may step from a table,
	// since tables are package-level, so the list is read with one atomic
	// load and no lock, and replaced whole, under layoutsMu, when a step is
	// first taken.
	edges atomic.Pointer[[]layoutEdge]
}

// layoutEdge is a step from one table to another: field gained (with) or
// lost.
type layoutEdge struct {
	field int
	with  bool
	to    *Layout
}

// layoutsMu orders the first taking of every step, in every table.
var layoutsMu sync.Mutex

// Has reports whether an object of this table has field: `"p" in o`.
func (l *Layout) Has(field int) bool { return slices.Contains(l.fields, field) }

// With is the table of an object of this one after `o.p = v`, where field
// is p's and key its name: this one when it has p already, and otherwise
// this one with p appended, which is where Node puts a property assigned
// after the object was made.
func (l *Layout) With(field int, key string) *Layout {
	if l.Has(field) {
		return l
	}
	return l.step(field, true, func() *Layout {
		return NewLayout(append(slices.Clone(l.keys), key), append(slices.Clone(l.fields), field))
	})
}

// Without is the table of an object of this one after `delete o.p`: this
// one less p's key, or this one when it has no p.
func (l *Layout) Without(field int) *Layout {
	i := slices.Index(l.fields, field)
	if i < 0 {
		return l
	}
	return l.step(field, false, func() *Layout {
		return NewLayout(slices.Delete(slices.Clone(l.keys), i, i+1), slices.Delete(slices.Clone(l.fields), i, i+1))
	})
}

// step is the table one step from l, made by newLayout the first time.
func (l *Layout) step(field int, with bool, newLayout func() *Layout) *Layout {
	if to := l.stepped(field, with); to != nil {
		return to
	}
	layoutsMu.Lock()
	defer layoutsMu.Unlock()
	if to := l.stepped(field, with); to != nil {
		return to // another goroutine took the step while this one waited
	}
	to := newLayout()
	var edges []layoutEdge
	if old := l.edges.Load(); old != nil {
		edges = slices.Clone(*old)
	}
	edges = append(edges, layoutEdge{field: field, with: with, to: to})
	l.edges.Store(&edges)
	return to
}

func (l *Layout) stepped(field int, with bool) *Layout {
	if edges := l.edges.Load(); edges != nil {
		for _, e := range *edges {
			if e.field == field && e.with == with {
				return e.to
			}
		}
	}
	return nil
}

// NewLayout makes the key table for a literal: keys in the order it writes
// them, and for each the index of the field it sets in the shape's order.
// The backend makes one per literal, at package level.
func NewLayout(keys []string, fields []int) *Layout {
	if len(keys) != len(fields) {
		panic("cakebear: a layout needs one field index per key")
	}
	printed := make([]string, len(keys))
	for i, k := range keys {
		printed[i] = printedKey(k)
	}
	return &Layout{keys: keys, fields: fields, printed: printed}
}

// printedKey is how Node prints a property name (R5): bare when it is an
// identifier of ASCII letters, digits and `_`, reserved words included, and
// otherwise quoted and escaped as a string is. __proto__ never gets here:
// lowering refuses it.
func printedKey(k string) string {
	if isBareKey(k) {
		return k
	}
	return quote(k)
}

func isBareKey(k string) bool {
	if k == "" {
		return false
	}
	for i := range len(k) {
		c := k[i]
		letter := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// Cloner copies a graph of objects and arrays, keeping its aliasing: a value
// reached twice is copied once, and a cycle stays a cycle. It is what a
// spawned function's result goes through on its way back to the caller, so
// leftover work in the spawned domain can't change what the caller holds
// (D11).
type Cloner struct {
	copies map[any]any
}

// Clone copies v with a fresh Cloner.
func Clone[T any](v T) T {
	return CloneValue(&Cloner{}, v)
}

// CloneValue copies v as part of c's copy: an object or an array once,
// anything else as it is. Primitives are values already, and a function is
// shared, as JavaScript shares it.
func CloneValue[T any](c *Cloner, v T) T {
	switch x := any(v).(type) {
	case Object:
		if done, ok := c.copies[x]; ok {
			return done.(T)
		}
		copied, _ := x.CloneObject(c).(T)
		return copied
	case cloneable:
		if done, ok := c.copies[x]; ok {
			return done.(T)
		}
		copied, _ := x.cloneArray(c).(T)
		return copied
	case maybe:
		// A Maybe is a value; what it holds may be an object or an array.
		copied, _ := x.cloneMaybe(c).(T)
		return copied
	}
	return v
}

// Remember records that copied is original's copy. A CloneObject calls it
// before copying its fields, so a field leading back to original finds the
// copy rather than starting another.
func (c *Cloner) Remember(original, copied any) {
	if c.copies == nil {
		c.copies = make(map[any]any)
	}
	c.copies[original] = copied
}

// cloneable is an array, which copies itself through cloneArray.
type cloneable interface {
	cloneArray(c *Cloner) any
}
