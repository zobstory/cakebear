package runtime

import (
	"math"
	"strconv"
	"strings"
	"sync"
)

// The values JSON.parse makes of objects (S1 plan D4): a dynamic object,
// keys and values, which an unknown holds. It is an Object, so printing and
// JSON.stringify take it as they take a shape's, through its key table.

// jsonObject is what JSON.parse makes of an object while it builds one:
// keys in V8's order (array indices first, ascending, then the rest in
// insertion order), a duplicate key's last value at its first place.
type jsonObject struct {
	keys []string
	vals []any
}

// JSONObject is a parsed object: its key table and its values, each as an
// unknown holds it. A program never writes to one (lowering has no write
// to an unknown), so it never changes once made.
type JSONObject struct {
	layout *Layout
	vals   []any
}

func (o *JSONObject) Layout() *Layout { return o.layout }
func (o *JSONObject) Field(i int) any { return o.vals[i] }

// CloneObject copies the object for a spawn's result, its values through
// the cloner, so an object or array inside is copied once.
func (o *JSONObject) CloneObject(c *Cloner) Object {
	cp := &JSONObject{layout: o.layout, vals: make([]any, len(o.vals))}
	c.Remember(o, cp)
	for i, v := range o.vals {
		cp.vals[i] = CloneValue(c, v)
	}
	return cp
}

// Get is the value of key, and whether the object has it.
func (o *JSONObject) Get(key string) (any, bool) {
	for i, k := range o.layout.keys {
		if k == key {
			return o.vals[i], true
		}
	}
	return nil, false
}

// jsonLayouts holds a table for each key order parsed so far, so a
// response of many records of one shape makes one table, not one each.
var jsonLayouts sync.Map // the count and the keys, joined by NUL -> *Layout

func jsonLayout(keys []string) *Layout {
	// The count first: joined alone, {} and {"": …} would both be "".
	id := strconv.Itoa(len(keys)) + "\x00" + strings.Join(keys, "\x00")
	if l, ok := jsonLayouts.Load(id); ok {
		return l.(*Layout)
	}
	fields := make([]int, len(keys))
	for i := range fields {
		fields[i] = i
	}
	l, _ := jsonLayouts.LoadOrStore(id, NewLayout(keys, fields))
	return l.(*Layout)
}

// value is the object built: its keys in V8's order.
func (o *jsonObject) value() *JSONObject {
	return &JSONObject{layout: jsonLayout(o.keys), vals: o.vals}
}

func (o *jsonObject) set(k string, v any) {
	for i, have := range o.keys {
		if have == k {
			o.vals[i] = v
			return
		}
	}
	o.keys = append(o.keys, k)
	o.vals = append(o.vals, v)
}

// arrayIndex is ECMA-262's: a canonical numeric string below 2^32 - 1.
func jsonArrayIndex(k string) (uint32, bool) {
	if k == "" || len(k) > 10 || (len(k) > 1 && k[0] == '0') {
		return 0, false
	}
	n, err := strconv.ParseUint(k, 10, 64)
	if err != nil || n >= math.MaxUint32 {
		return 0, false
	}
	return uint32(n), true
}

// order moves array-index keys to the front, ascending, keeping the rest.
func (o *jsonObject) order() {
	type kv struct {
		k string
		v any
		n uint32
	}
	var idx, rest []kv
	for i, k := range o.keys {
		if n, ok := jsonArrayIndex(k); ok {
			idx = append(idx, kv{k, o.vals[i], n})
		} else {
			rest = append(rest, kv{k, o.vals[i], 0})
		}
	}
	if len(idx) == 0 {
		return
	}
	for i := 1; i < len(idx); i++ { // insertion sort: index keys are few
		for j := i; j > 0 && idx[j].n < idx[j-1].n; j-- {
			idx[j], idx[j-1] = idx[j-1], idx[j]
		}
	}
	o.keys, o.vals = o.keys[:0], o.vals[:0]
	for _, e := range append(idx, rest...) {
		o.keys = append(o.keys, e.k)
		o.vals = append(o.vals, e.v)
	}
}
