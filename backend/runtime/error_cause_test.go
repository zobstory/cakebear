package runtime

import (
	"strings"
	"testing"
)

// refused is s1-node R9's system error, made as fetch's failure will make
// it: Node's errno, code, syscall, address and port, in that order.
func refused() *Error {
	return NewErrorWith(ErrorBase, "connect ECONNREFUSED 127.0.0.1:18901", ErrorDetails{Props: []ErrorProp{
		NumberProp("errno", -61), StringProp("code", "ECONNREFUSED"), StringProp("syscall", "connect"),
		StringProp("address", "127.0.0.1"), NumberProp("port", 18901),
	}}, nil)
}

// fetchFailed is R9's whole error: `TypeError: fetch failed`, caused by the
// refusal.
func fetchFailed() *Error {
	return NewErrorWith(ErrorType, "fetch failed", ErrorDetails{Cause: refused(), HasCause: true}, nil)
}

// R9's output, as Node 24.18 printed a refused fetch (s1-node case 04).
const fetchFailedPrinted = `[TypeError: fetch failed] {
  [cause]: [Error: connect ECONNREFUSED 127.0.0.1:18901] {
    errno: -61,
    code: 'ECONNREFUSED',
    syscall: 'connect',
    address: '127.0.0.1',
    port: 18901
  }
}`

// TestFetchFailureMatchesNode: R9's error through the API fetch will call,
// printed, converted and serialised as Node 24.18 does: the cause is no
// part of the stack or String, and no enumerable property, so
// JSON.stringify writes `{}` for the TypeError and the system error's own
// properties for its cause. An uncaught one is reported with its cause, as
// R12's report has it between Node's header and its version line.
func TestFetchFailureMatchesNode(t *testing.T) {
	t.Parallel()

	e, cause := fetchFailed(), refused()
	for _, c := range []struct{ what, got, want string }{
		{"inspect", Inspect(e), fetchFailedPrinted},
		{"console.log", Format(e), fetchFailedPrinted},
		{"String", ErrorToString(e), "TypeError: fetch failed"},
		{"a template", UnknownToString(e), "TypeError: fetch failed"},
		{"the stack", e.Stack().Must("stack"), "TypeError: fetch failed"},
		{"JSON", JSONStringify(e, JSONGap{}, nil), "{}"},
		{"the cause's JSON", JSONStringify(cause, JSONGap{}, nil),
			`{"errno":-61,"code":"ECONNREFUSED","syscall":"connect","address":"127.0.0.1","port":18901}`},
		{"the cause's String", ErrorToString(cause), "Error: connect ECONNREFUSED 127.0.0.1:18901"},
		{"the report", errorReport(e, nil), fetchFailedPrinted + versionLines},
	} {
		if c.got != c.want {
			t.Errorf("%s: got\n%s\nwant, as Node's\n%s", c.what, c.got, c.want)
		}
	}
	if got, ok := e.Cause().(*Error); !ok || !e.HasCause() || Inspect(got) != Inspect(cause) || !e.InstanceOf(ErrorType) || TypeOf(e) != TagObject {
		t.Errorf("fetch failed: cause %v, instanceof TypeError %v, typeof %s", e.Cause(), e.InstanceOf(ErrorType), TypeOf(e))
	}
	if v, ok := cause.Prop("errno"); !ok || v != any(float64(-61)) {
		t.Errorf("the cause's errno is %v, %v", v, ok)
	}
	if v, ok := cause.Prop("code"); !ok || v != any("ECONNREFUSED") {
		t.Errorf("the cause's code is %v, %v", v, ok)
	}
	if _, ok := e.Prop("code"); ok {
		t.Errorf("fetch failed has a code of its own")
	}
}

// TestCauseOrUndefined: an Error made with no cause has none, and one made
// with `{ cause: undefined }` has one, which Node prints (measured:
// `[Error: m] { [cause]: undefined }`); e.cause reads undefined for both.
func TestCauseOrUndefined(t *testing.T) {
	t.Parallel()

	none := NewErrorWith(ErrorBase, "m", ErrorDetails{}, nil)
	undef := NewErrorWith(ErrorBase, "m", ErrorDetails{HasCause: true}, nil)
	null := NewErrorWith(ErrorBase, "m", ErrorDetails{Cause: Null, HasCause: true}, nil)
	for _, c := range []struct {
		e        *Error
		has      bool
		cause    any
		printed  string
		asString string
	}{
		{none, false, nil, "[Error: m]", "Error: m"},
		{undef, true, nil, "[Error: m] { [cause]: undefined }", "Error: m"},
		{null, true, Null, "[Error: m] { [cause]: null }", "Error: m"},
	} {
		if c.e.HasCause() != c.has || c.e.Cause() != c.cause || Inspect(c.e) != c.printed || ErrorToString(c.e) != c.asString ||
			JSONStringify(c.e, JSONGapNumber(2), nil) != "{}" {
			t.Errorf("has %v, cause %v, printed %q, String %q", c.e.HasCause(), c.e.Cause(), Inspect(c.e), ErrorToString(c.e))
		}
	}
}

// TestCycleThroughACause: a cause leading back to its error is a circular
// reference, as Node prints it (measured: `const o = {}; const e = new
// Error('m', { cause: o }); o.e = e`), and JSON.stringify never follows a
// cause, so it is no cycle there. The runtime's causes are its own Errors,
// so none can lead back; this holds the printer to Node anyway.
func TestCycleThroughACause(t *testing.T) {
	t.Parallel()

	o := newTestObject([]string{"e"}, []any{nil})
	e := NewErrorWith(ErrorBase, "m", ErrorDetails{Cause: o, HasCause: true}, nil)
	o.values[0] = e
	for _, c := range []struct{ got, want string }{
		{Inspect(e), "<ref *1> [Error: m] { [cause]: { e: [Circular *1] } }"},
		{Inspect(o), "<ref *1> { e: [Error: m] { [cause]: [Circular *1] } }"},
		{JSONStringify(e, JSONGap{}, nil), "{}"},
		{JSONStringify(o, JSONGap{}, nil), `{"e":{}}`},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want, as Node's, %q", c.got, c.want)
		}
	}
}

// TestErrorPropsNeverChange: an Error keeps the properties it was made
// with, whatever happens to the slice they came in, so it never changes
// once made (CLAUDE.md, M6 step 5) and domains can still share one.
func TestErrorPropsNeverChange(t *testing.T) {
	t.Parallel()

	props := []ErrorProp{StringProp("code", "E")}
	e := NewErrorWith(ErrorBase, "m", ErrorDetails{Props: props}, nil)
	props[0] = StringProp("code", "CHANGED")
	if got := Inspect(e); got != "[Error: m] { code: 'E' }" {
		t.Errorf("printed %q after its props' slice changed", got)
	}
	if CloneValue(&Cloner{}, any(e)) != any(e) {
		t.Errorf("a clone of an Error with properties is a new Error, not the one it never changes from")
	}
}

// TestBadErrorPropsPanic: properties only the runtime's own bug could pass
// panic, naming what is wrong, rather than make an Error Node couldn't.
func TestBadErrorPropsPanic(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		props []ErrorProp
		want  string
	}{
		{[]ErrorProp{{Key: "port", Value: 18901}}, `property "port" of int`},
		{[]ErrorProp{{Key: "ok", Value: true}}, `property "ok" of bool`},
		{[]ErrorProp{StringProp("message", "m")}, `property "message", which`},
		{[]ErrorProp{StringProp("cause", "c")}, `property "cause", which`},
		{[]ErrorProp{NumberProp("0", 1)}, `property "0", which`},
		{[]ErrorProp{NumberProp("4294967294", 1)}, `property "4294967294", which`},
		{[]ErrorProp{StringProp("code", "A"), StringProp("code", "B")}, `property "code" twice`},
	} {
		got := func() (p any) {
			defer func() { p = recover() }()
			NewErrorWith(ErrorBase, "m", ErrorDetails{Props: c.props}, nil)
			return nil
		}()
		if s, _ := got.(string); !strings.Contains(s, c.want) {
			t.Errorf("props %v: panicked with %v, want a panic saying %q", c.props, got, c.want)
		}
	}
	// Keys JavaScript keeps in their order are fine.
	for _, key := range []string{"4294967295", "01", "1abc", "", "-1"} {
		e := NewErrorWith(ErrorBase, "m", ErrorDetails{Props: []ErrorProp{NumberProp("a", 0), NumberProp(key, 1)}}, nil)
		if _, ok := e.Prop(key); !ok {
			t.Errorf("no property %q", key)
		}
	}
}
