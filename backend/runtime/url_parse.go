package runtime

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The URL Standard's basic URL parser (https://url.spec.whatwg.org/#concept-basic-url-parser),
// state by state, as Node 24.18's URL follows it (R38): the same parser runs
// a setter, from the state the setter names, over the URL it changes (R40).
// Go's net/url follows RFC 3986 instead, and differs from Node on 20 of
// s1-node's 37 URLs (s1-go, Part 2b).

// urlRecord is a URL record: its host as it serializes, and its path a list
// of segments or, when opaque, a string.
type urlRecord struct {
	scheme, username, password string
	host                       string
	hasHost                    bool // false for a null host
	port                       int  // -1 for null
	path                       []string
	opaquePath                 string
	opaque                     bool
	query                      string
	hasQuery                   bool
	fragment                   string
	hasFragment                bool
}

// specialPorts are the special schemes and their default ports, -1 for
// none.
var specialPorts = map[string]int{"ftp": 21, "file": -1, "http": 80, "https": 443, "ws": 80, "wss": 443}

func isSpecialScheme(s string) bool { _, ok := specialPorts[s]; return ok }

func defaultPort(scheme string) int {
	if p, ok := specialPorts[scheme]; ok {
		return p
	}
	return -1
}

func (r *urlRecord) special() bool { return isSpecialScheme(r.scheme) }

func (r *urlRecord) includesCredentials() bool { return r.username != "" || r.password != "" }

func (r *urlRecord) cannotHaveCredentialsOrPort() bool {
	return !r.hasHost || r.host == "" || r.scheme == "file"
}

// urlState is one of the parser's states, and as a state override the one
// a setter starts in.
type urlState uint8

const (
	noOverride urlState = iota
	stSchemeStart
	stScheme
	stNoScheme
	stSpecialRelativeOrAuthority
	stPathOrAuthority
	stRelative
	stRelativeSlash
	stSpecialAuthoritySlashes
	stSpecialAuthorityIgnoreSlashes
	stAuthority
	stHost
	stHostname
	stPort
	stFile
	stFileSlash
	stFileHost
	stPathStart
	stPath
	stOpaquePath
	stQuery
	stFragment
)

// eof is the EOF code point.
const eof = -1

// urlStep is how a state's step ends: the parser goes on, returns, or
// fails.
type urlStep uint8

const (
	stepOn urlStep = iota
	stepReturn
	stepFail
)

type urlParser struct {
	in       []rune
	p        int
	url      *urlRecord
	base     *urlRecord
	override urlState
	state    urlState
	buf      []byte
	atSign   bool
	brackets bool
	password bool
	// joinPath is set while a relative URL is resolved against an opaque
	// path, which Node's parser does where the standard fails (noScheme).
	joinPath bool
}

// basicParse is the basic URL parser of input, a USVString, against base
// (nil for none). With url nil it makes a new URL, first trimming C0
// controls and spaces from either end; otherwise it changes url from the
// state override, as a setter does, and what it changed before a failure
// stays changed. Tabs and newlines anywhere are removed either way.
func basicParse(input string, base, url *urlRecord, override urlState) (*urlRecord, bool) {
	in := []rune(input)
	if url == nil {
		url = &urlRecord{port: -1}
		start, end := 0, len(in)
		for start < end && in[start] <= ' ' {
			start++
		}
		for end > start && in[end-1] <= ' ' {
			end--
		}
		in = in[start:end]
	}
	kept := in[:0:0]
	for _, r := range in {
		if r != '\t' && r != '\n' && r != '\r' {
			kept = append(kept, r)
		}
	}
	p := urlParser{in: kept, url: url, base: base, override: override, state: override}
	if override == noOverride {
		p.state = stSchemeStart
	}
	for ; p.p <= len(p.in); p.p++ {
		c := rune(eof)
		if p.p < len(p.in) {
			c = p.in[p.p]
		}
		switch p.step(c) {
		case stepReturn:
			return url, true
		case stepFail:
			return url, false
		}
	}
	if p.joinPath {
		url.opaque, url.opaquePath, url.path = true, strings.Join(url.path, "/"), nil
	}
	return url, true
}

// at is the code point at p.p+offset, or eof past the end.
func (p *urlParser) at(offset int) rune {
	if i := p.p + offset; i >= 0 && i < len(p.in) {
		return p.in[i]
	}
	return eof
}

func (p *urlParser) step(c rune) urlStep {
	switch p.state {
	case stSchemeStart:
		return p.schemeStart(c)
	case stScheme:
		return p.scheme(c)
	case stNoScheme:
		return p.noScheme(c)
	case stSpecialRelativeOrAuthority, stSpecialAuthoritySlashes:
		if c != '/' || p.at(1) != '/' {
			if p.state == stSpecialRelativeOrAuthority {
				p.state = stRelative
			} else {
				p.state = stSpecialAuthorityIgnoreSlashes
			}
			p.p--
			return stepOn
		}
		p.state = stSpecialAuthorityIgnoreSlashes
		p.p++
	case stPathOrAuthority:
		if c == '/' {
			p.state = stAuthority
		} else {
			p.state = stPath
			p.p--
		}
	case stRelative:
		p.relative(c)
	case stRelativeSlash:
		switch {
		case p.url.special() && (c == '/' || c == '\\'):
			p.state = stSpecialAuthorityIgnoreSlashes
		case c == '/':
			p.state = stAuthority
		default:
			p.copyAuthority(p.base)
			p.state = stPath
			p.p--
		}
	case stSpecialAuthorityIgnoreSlashes:
		if c != '/' && c != '\\' {
			p.state = stAuthority
			p.p--
		}
	case stAuthority:
		return p.authority(c)
	case stHost, stHostname:
		return p.host(c)
	case stPort:
		return p.port(c)
	case stFile:
		p.file(c)
	case stFileSlash:
		p.fileSlash(c)
	case stFileHost:
		return p.fileHost(c)
	case stPathStart:
		p.pathStart(c)
	case stPath:
		p.path(c)
	case stOpaquePath:
		p.opaquePath(c)
	case stQuery:
		p.query(c)
	case stFragment:
		if c != eof {
			p.buf = appendEncoded(p.buf, c, setFragment)
		} else {
			p.url.fragment += string(p.buf)
		}
	}
	return stepOn
}

func isASCIIAlpha(c rune) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

func isASCIIDigit(c rune) bool { return c >= '0' && c <= '9' }

func (p *urlParser) schemeStart(c rune) urlStep {
	switch {
	case isASCIIAlpha(c):
		p.buf = append(p.buf, byte(c|0x20))
		p.state = stScheme
	case p.override == noOverride:
		p.state = stNoScheme
		p.p--
	default:
		return stepFail
	}
	return stepOn
}

func (p *urlParser) scheme(c rune) urlStep {
	url := p.url
	switch {
	case isASCIIAlpha(c) || isASCIIDigit(c) || c == '+' || c == '-' || c == '.':
		if isASCIIAlpha(c) {
			c |= 0x20
		}
		p.buf = append(p.buf, byte(c))
		return stepOn
	case c != ':':
		if p.override != noOverride {
			return stepFail
		}
		p.buf = p.buf[:0]
		p.state = stNoScheme
		p.p = -1
		return stepOn
	}
	scheme := string(p.buf)
	if p.override != noOverride {
		if url.special() != isSpecialScheme(scheme) ||
			(url.includesCredentials() || url.port != -1) && scheme == "file" ||
			url.scheme == "file" && url.hasHost && url.host == "" {
			return stepReturn
		}
	}
	url.scheme = scheme
	if p.override != noOverride {
		if url.port == defaultPort(url.scheme) {
			url.port = -1
		}
		return stepReturn
	}
	p.buf = p.buf[:0]
	switch {
	case url.scheme == "file":
		p.state = stFile
	case url.special() && p.base != nil && p.base.scheme == url.scheme:
		p.state = stSpecialRelativeOrAuthority
	case url.special():
		p.state = stSpecialAuthoritySlashes
	case p.at(1) == '/':
		p.state = stPathOrAuthority
		p.p++
	default:
		url.opaque, url.opaquePath = true, ""
		p.state = stOpaquePath
	}
	return stepOn
}

func (p *urlParser) noScheme(c rune) urlStep {
	base, url := p.base, p.url
	switch {
	case base == nil || base.opaque && c != '#' && !slices.Contains(p.in, '#'):
		return stepFail
	case base.opaque && c == '#':
		url.scheme, url.opaque, url.opaquePath = base.scheme, true, base.opaquePath
		url.query, url.hasQuery = base.query, base.hasQuery
		p.enter(stFragment)
	case base.opaque:
		// The standard fails here. Node 24.18's parser, Ada, checks for a
		// fragment anywhere in the input rather than at its start, so it
		// resolves the input against the opaque path, as a path of the
		// segments its slashes make, and the result's path is opaque
		// again: "x#f" against "sc:a/b" is "sc:a/x#f" (measured).
		p.state = stRelative
		p.p--
	case base.scheme != "file":
		p.state = stRelative
		p.p--
	default:
		p.state = stFile
		p.p--
	}
	return stepOn
}

// enter sets the query or the fragment to the empty string and goes to its
// state.
func (p *urlParser) enter(state urlState) {
	if state == stQuery {
		p.url.query, p.url.hasQuery = "", true
	} else {
		p.url.fragment, p.url.hasFragment = "", true
	}
	p.buf = p.buf[:0]
	p.state = state
}

func (p *urlParser) copyAuthority(from *urlRecord) {
	p.url.username, p.url.password = from.username, from.password
	p.url.host, p.url.hasHost, p.url.port = from.host, from.hasHost, from.port
}

func (p *urlParser) relative(c rune) {
	url, base := p.url, p.base
	url.scheme = base.scheme
	if c == '/' || url.special() && c == '\\' {
		p.state = stRelativeSlash
		return
	}
	p.copyAuthority(base)
	url.path = append([]string(nil), base.path...)
	if base.opaque {
		url.path, p.joinPath = strings.Split(base.opaquePath, "/"), true
	}
	url.query, url.hasQuery = base.query, base.hasQuery
	switch c {
	case '?':
		p.enter(stQuery)
	case '#':
		p.enter(stFragment)
	case eof:
	default:
		url.query, url.hasQuery = "", false
		p.shortenPath()
		p.state = stPath
		p.p--
	}
}

// endsAuthority reports whether c ends the authority, the host or the
// port.
func (p *urlParser) endsAuthority(c rune) bool {
	return c == eof || c == '/' || c == '?' || c == '#' || p.url.special() && c == '\\'
}

func (p *urlParser) authority(c rune) urlStep {
	switch {
	case c == '@':
		if p.atSign {
			p.buf = append([]byte("%40"), p.buf...)
		}
		p.atSign = true
		for _, r := range string(p.buf) {
			if r == ':' && !p.password {
				p.password = true
				continue
			}
			if p.password {
				p.url.password = string(appendEncoded([]byte(p.url.password), r, setUserinfo))
			} else {
				p.url.username = string(appendEncoded([]byte(p.url.username), r, setUserinfo))
			}
		}
		p.buf = p.buf[:0]
	case p.endsAuthority(c):
		if p.atSign && len(p.buf) == 0 {
			return stepFail
		}
		p.p -= utf8.RuneCount(p.buf) + 1
		p.buf = p.buf[:0]
		p.state = stHost
	default:
		p.buf = utf8.AppendRune(p.buf, c)
	}
	return stepOn
}

func (p *urlParser) host(c rune) urlStep {
	url := p.url
	switch {
	case p.override != noOverride && url.scheme == "file":
		p.p--
		p.state = stFileHost
	case c == ':' && !p.brackets:
		if len(p.buf) == 0 || p.override == stHostname {
			return stepFail
		}
		host, ok := parseHost(string(p.buf), url.special())
		if !ok {
			return stepFail
		}
		url.host, url.hasHost = host, true
		p.buf = p.buf[:0]
		p.state = stPort
	case p.endsAuthority(c):
		p.p--
		if url.special() && len(p.buf) == 0 {
			return stepFail
		}
		if p.override != noOverride && len(p.buf) == 0 && (url.includesCredentials() || url.port != -1) {
			return stepReturn
		}
		host, ok := parseHost(string(p.buf), url.special())
		if !ok {
			return stepFail
		}
		url.host, url.hasHost = host, true
		p.buf = p.buf[:0]
		p.state = stPathStart
		if p.override != noOverride {
			return stepReturn
		}
	default:
		if c == '[' {
			p.brackets = true
		} else if c == ']' {
			p.brackets = false
		}
		p.buf = utf8.AppendRune(p.buf, c)
	}
	return stepOn
}

func (p *urlParser) port(c rune) urlStep {
	switch {
	case isASCIIDigit(c):
		p.buf = append(p.buf, byte(c))
	case p.endsAuthority(c) || p.override != noOverride:
		if len(p.buf) != 0 {
			port := 0
			for _, d := range p.buf {
				port = min(port*10+int(d-'0'), 1<<20)
			}
			if port > 1<<16-1 {
				return stepFail
			}
			if port == defaultPort(p.url.scheme) {
				port = -1
			}
			p.url.port = port
			p.buf = p.buf[:0]
		}
		if p.override != noOverride {
			return stepReturn
		}
		p.state = stPathStart
		p.p--
	default:
		return stepFail
	}
	return stepOn
}

func portString(port int) string { return strconv.Itoa(port) }
