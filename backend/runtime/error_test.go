package runtime

import (
	"encoding/json"
	"slices"
	"testing"
)

// The goldens' Errors and unknowns (testdata/golden-codec.mjs): what the
// decoder builds of them.

// errorValue is an Error of the kind the description names, with Node's
// code when it has one: what the runtime makes where Node throws.
func (d *decoder) errorValue(g goldenValue) *Error {
	k := slices.Index(errorKindNames[:], *g.Err)
	if k < 0 {
		d.t.Fatalf("no Error kind %q", *g.Err)
	}
	m := ""
	if g.M != nil {
		m = *g.M
	}
	if g.Code != nil {
		return newCodedError(ErrorKind(k), *g.Code, m, nil)
	}
	return NewError(ErrorKind(k), m, nil)
}

// unknown is a value an unknown holds: undefined and null as an unknown
// holds them, nil and Null, and anything else as it is (unknown.go).
func (d *decoder) unknown(raw json.RawMessage) any {
	var g goldenValue
	if err := json.Unmarshal(raw, &g); err != nil {
		d.t.Fatalf("decoding %s: %v", raw, err)
	}
	if g.E != nil {
		if *g.E == "null" {
			return Null
		}
		return nil
	}
	return d.value(raw)
}

// unknownArray is an unknown[], an *Array[any] of unknowns.
func (d *decoder) unknownArray(g goldenValue) *Array[any] {
	arr := NewArray[any]()
	d.register(g.ID, arr) // before its elements, which may refer back to it
	for _, raw := range g.A {
		arr.Push(d.unknown(raw))
	}
	return arr
}

// unknownObject is an object whose properties are unknowns.
func (d *decoder) unknownObject(g goldenValue) *testObject {
	obj := &testObject{}
	d.register(g.ID, obj)
	keys := make([]string, len(g.O))
	values := make([]any, len(g.O))
	for i, kv := range g.O {
		if err := json.Unmarshal(kv[0], &keys[i]); err != nil {
			d.t.Fatalf("a key: %v", err)
		}
		values[i] = d.unknown(kv[1])
	}
	*obj = *newTestObject(keys, values)
	return obj
}

// TestErrorKindsMatchNode: each built-in kind's name, message, stack and
// String, and which constructors it is an instance of, as Node 24.18 gave
// them under --stack-trace-limit=0 for no message, "", "m" and "a\nb"
// (R26, R28, R29): the stack is String's text, every line of the message
// included, and every kind is an Error and its own kind, and no other.
func TestErrorKindsMatchNode(t *testing.T) {
	t.Parallel()

	for k, name := range errorKindNames {
		kind := ErrorKind(k)
		for _, m := range []string{"", "m", "a\nb"} {
			want := name
			if m != "" {
				want = name + ": " + m
			}
			e := NewError(kind, m, nil)
			stack, ok := e.Stack().maybeState()
			if e.Name() != name || e.Message() != m || ErrorToString(e) != want || stack != stateValue || ok != any(want) {
				t.Errorf("new %s(%q): name %q, message %q, String %q, stack %v; want %q", name, m, e.Name(), e.Message(), ErrorToString(e), ok, want)
			}
			if e.Cause() != nil || TypeOf(e) != TagObject || e.Kind() != kind {
				t.Errorf("new %s(%q): cause %v, typeof %s, kind %s", name, m, e.Cause(), TypeOf(e), e.Kind())
			}
			for j := range errorKindNames {
				if got, want := e.InstanceOf(ErrorKind(j)), j == 0 || j == k; got != want {
					t.Errorf("new %s(%q) instanceof %s = %v, want %v", name, m, errorKindNames[j], got, want)
				}
				if got := InstanceOf(any(e), ErrorKind(j)); got != e.InstanceOf(ErrorKind(j)) {
					t.Errorf("as an unknown, instanceof %s = %v", errorKindNames[j], got)
				}
				if MaybeInstanceOf(Some(e), ErrorKind(j)) != e.InstanceOf(ErrorKind(j)) || MaybeInstanceOf(NullOf[*Error](), ErrorKind(j)) ||
					MaybeInstanceOf(Maybe[*Error]{}, ErrorKind(j)) {
					t.Errorf("a maybe of new %s(%q) instanceof %s is wrong", name, m, errorKindNames[j])
				}
			}
		}
	}
}

// TestCodedErrorsMatchNode: Node's own errors name their code in their
// stack and their String (R34), have it as their one own enumerable
// property, so JSON.stringify writes it, and console.log prints it in a
// block, on its line when it fits in 80 columns.
func TestCodedErrorsMatchNode(t *testing.T) {
	t.Parallel()

	long := newCodedError(ErrorBase, "ERR_HTTP_HEADERS_SENT", "Cannot set headers after they are sent to the client", nil)
	short := newCodedError(ErrorRange, "ERR_X", "short", nil)
	for _, c := range []struct{ got, want string }{
		{ErrorToString(long), "Error [ERR_HTTP_HEADERS_SENT]: Cannot set headers after they are sent to the client"},
		{long.Stack().Must("stack"), "Error [ERR_HTTP_HEADERS_SENT]: Cannot set headers after they are sent to the client"},
		{long.Message(), "Cannot set headers after they are sent to the client"},
		{JSONStringify(long, JSONGap{}, nil), `{"code":"ERR_HTTP_HEADERS_SENT"}`},
		{Inspect(long), "[Error [ERR_HTTP_HEADERS_SENT]: Cannot set headers after they are sent to the client] {\n  code: 'ERR_HTTP_HEADERS_SENT'\n}"},
		{Inspect(short), "[RangeError [ERR_X]: short] { code: 'ERR_X' }"},
		{JSONStringify(NewError(ErrorType, "t", nil), JSONGapNumber(2), nil), "{}"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
