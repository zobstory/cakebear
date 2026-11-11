package runtime

// URL is WHATWG's URL as Node 24.18 has it (S1 plan, R38–R41): a URL record
// the URL Standard's parser makes (url_parse.go), its getters strings, an
// absent part "", and its setters running the same parser from the state
// each names. A URL and its searchParams belong to one domain: lowering
// keeps them from being shared, so they need no lock.
type URL struct {
	rec    urlRecord
	params *URLSearchParams
}

func newURL(rec *urlRecord) *URL {
	u := &URL{rec: *rec}
	u.params = &URLSearchParams{url: u}
	u.syncParams()
	return u
}

// syncParams makes the searchParams' list the query's, as Node does after
// every change to the URL: it parses its search again.
func (u *URL) syncParams() {
	u.params.list = nil
	if u.rec.hasQuery {
		u.params.list = parseForm(u.rec.query)
	}
}

// parseWithBase parses input against base, both USVStrings, or against no
// base when hasBase is false; a base that fails to parse fails it too.
func parseWithBase(input, base string, hasBase bool) (*urlRecord, bool) {
	var b *urlRecord
	if hasBase {
		var ok bool
		if b, ok = basicParse(base, nil, nil, noOverride); !ok {
			return nil, false
		}
	}
	return basicParse(input, b, nil, noOverride)
}

// invalidURL is the TypeError Node's URL constructor throws (R39), which
// its C++ makes: `[TypeError: Invalid URL] { code: 'ERR_INVALID_URL',
// input: 'not a url' }`, with base after input when there is one. Both are
// the strings as the URL saw them, USVStrings.
func invalidURL(input, base string, hasBase bool, site *Site) *Error {
	props := []ErrorProp{StringProp("code", "ERR_INVALID_URL"), StringProp("input", input)}
	if hasBase {
		props = append(props, StringProp("base", base))
	}
	return NewErrorWith(ErrorType, "Invalid URL", ErrorDetails{Props: props}, site)
}

func newURLOrThrow(input, base string, hasBase bool, site *Site) *URL {
	input, base = usv(input), usv(base)
	rec, ok := parseWithBase(input, base, hasBase)
	if !ok {
		panic(&Thrown{V: invalidURL(input, base, hasBase, site), Site: site})
	}
	return newURL(rec)
}

// NewURL is `new URL(input)`, which throws Invalid URL at site.
func NewURL(input string, site *Site) *URL { return newURLOrThrow(input, "", false, site) }

// NewURLWithBase is `new URL(input, base)` with a string base.
func NewURLWithBase(input, base string, site *Site) *URL {
	return newURLOrThrow(input, base, true, site)
}

// NewURLWithBaseURL is `new URL(input, base)` with a URL as the base, which
// Node converts to its href first.
func NewURLWithBaseURL(input string, base *URL, site *Site) *URL {
	return newURLOrThrow(input, base.Href(), true, site)
}

// URLCanParse is `URL.canParse(input)`, which agrees with the constructor.
func URLCanParse(input string) bool {
	_, ok := parseWithBase(usv(input), "", false)
	return ok
}

// URLCanParseWithBase is `URL.canParse(input, base)`.
func URLCanParseWithBase(input, base string) bool {
	_, ok := parseWithBase(usv(input), usv(base), true)
	return ok
}

// URLParse is `URL.parse(input)`: the URL, or null where the constructor
// throws.
func URLParse(input string) Maybe[*URL] {
	if rec, ok := parseWithBase(usv(input), "", false); ok {
		return Some(newURL(rec))
	}
	return NullOf[*URL]()
}

// URLParseWithBase is `URL.parse(input, base)`.
func URLParseWithBase(input, base string) Maybe[*URL] {
	if rec, ok := parseWithBase(usv(input), usv(base), true); ok {
		return Some(newURL(rec))
	}
	return NullOf[*URL]()
}

// Href is `u.href`, the URL serialized.
func (u *URL) Href() string { return u.rec.href() }

// String is href: `u.toString()`, `u.toJSON()`, `String(u)` and a
// template's `${u}`.
func (u *URL) String() string { return u.rec.href() }

// Origin is `u.origin`: "null" for an opaque origin (R38).
func (u *URL) Origin() string { return u.rec.origin() }

// Protocol is `u.protocol`, the scheme and a colon.
func (u *URL) Protocol() string { return u.rec.scheme + ":" }

// Username is `u.username`.
func (u *URL) Username() string { return u.rec.username }

// Password is `u.password`.
func (u *URL) Password() string { return u.rec.password }

// Host is `u.host`: the host, and the port when it isn't the default.
func (u *URL) Host() string {
	if !u.rec.hasHost {
		return ""
	}
	return u.rec.hostPort()
}

// Hostname is `u.hostname`.
func (u *URL) Hostname() string { return u.rec.host }

// Port is `u.port`, "" for none.
func (u *URL) Port() string {
	if u.rec.port == -1 {
		return ""
	}
	return portString(u.rec.port)
}

// Pathname is `u.pathname`.
func (u *URL) Pathname() string { return u.rec.pathname() }

// Search is `u.search`: "" for no query and an empty one.
func (u *URL) Search() string {
	if !u.rec.hasQuery || u.rec.query == "" {
		return ""
	}
	return "?" + u.rec.query
}

// Hash is `u.hash`: "" for no fragment and an empty one.
func (u *URL) Hash() string {
	if !u.rec.hasFragment || u.rec.fragment == "" {
		return ""
	}
	return "#" + u.rec.fragment
}

// SearchParams is `u.searchParams`, the same object every time, live both
// ways (R41).
func (u *URL) SearchParams() *URLSearchParams { return u.params }

// SetHref is `u.href = v`: v parsed as a new URL, or the TypeError Node's
// setter throws (measured): one of its own, `TypeError [ERR_INVALID_URL]:
// Invalid URL`, with code and then input, v as it was given.
func (u *URL) SetHref(v string, site *Site) {
	rec, ok := basicParse(usv(v), nil, nil, noOverride)
	if !ok {
		props := []ErrorProp{StringProp("code", "ERR_INVALID_URL"), StringProp("input", v)}
		err := NewErrorWith(ErrorType, "Invalid URL", ErrorDetails{Props: props}, site)
		err.code = "ERR_INVALID_URL"
		panic(&Thrown{V: err, Site: site})
	}
	u.rec = *rec
	u.syncParams()
}

// The other setters change what parses and leave the URL as it was where
// nothing does (R40), but for what the parser changed before failing: a
// host set before a port too large for one.

// SetProtocol is `u.protocol = v`.
func (u *URL) SetProtocol(v string) {
	basicParse(usv(v)+":", nil, &u.rec, stSchemeStart)
}

// SetUsername is `u.username = v`.
func (u *URL) SetUsername(v string) {
	if !u.rec.cannotHaveCredentialsOrPort() {
		u.rec.username = encodeString(usv(v), setUserinfo)
	}
}

// SetPassword is `u.password = v`.
func (u *URL) SetPassword(v string) {
	if !u.rec.cannotHaveCredentialsOrPort() {
		u.rec.password = encodeString(usv(v), setUserinfo)
	}
}

// SetHost is `u.host = v`.
func (u *URL) SetHost(v string) { u.setHost(v, stHost) }

// SetHostname is `u.hostname = v`.
func (u *URL) SetHostname(v string) { u.setHost(v, stHostname) }

// setHost runs the parser from the host or hostname state. Where the
// standard gives a URL with no host an empty one, Node 24.18 leaves it as
// it was, unless its path starts with an empty segment, which the "/." the
// serializer writes stood for (measured: `sc:/p` stays `sc:/p`, and
// `sc:/.//p` becomes `sc:////p`).
func (u *URL) setHost(v string, state urlState) {
	if u.rec.opaque {
		return
	}
	hadHost := u.rec.hasHost
	basicParse(usv(v), nil, &u.rec, state)
	path := u.rec.path
	if !hadHost && u.rec.hasHost && u.rec.host == "" && !(len(path) > 1 && path[0] == "") {
		u.rec.hasHost = false
	}
}

// SetPort is `u.port = v`: "" removes it, and a value starting with digits
// sets them (`'81abc'` is 81).
func (u *URL) SetPort(v string) {
	switch {
	case u.rec.cannotHaveCredentialsOrPort():
	case v == "":
		u.rec.port = -1
	default:
		basicParse(usv(v), nil, &u.rec, stPort)
	}
}

// SetPathname is `u.pathname = v`.
func (u *URL) SetPathname(v string) {
	if !u.rec.opaque {
		u.rec.path = nil
		basicParse(usv(v), nil, &u.rec, stPathStart)
	}
}

// SetSearch is `u.search = v`: "" removes the query, `?` included, and
// anything else is the query, a leading `?` dropped, which searchParams
// then holds.
func (u *URL) SetSearch(v string) {
	v = usv(v)
	if v == "" {
		u.rec.query, u.rec.hasQuery = "", false
	} else {
		if v[0] == '?' {
			v = v[1:]
		}
		u.rec.query, u.rec.hasQuery = "", true
		basicParse(v, nil, &u.rec, stQuery)
	}
	u.syncParams()
}

// SetHash is `u.hash = v`, as SetSearch is for the fragment.
func (u *URL) SetHash(v string) {
	v = usv(v)
	if v == "" {
		u.rec.fragment, u.rec.hasFragment = "", false
		return
	}
	if v[0] == '#' {
		v = v[1:]
	}
	u.rec.fragment, u.rec.hasFragment = "", true
	basicParse(v, nil, &u.rec, stFragment)
}

// updateQuery is the URLSearchParams update steps: the list serialized is
// the query, and an empty one is none, which drops the `?` (R41).
func (u *URL) updateQuery(list []paramPair) {
	if s := serializeForm(list); s != "" {
		u.rec.query, u.rec.hasQuery = s, true
	} else {
		u.rec.query, u.rec.hasQuery = "", false
	}
}
