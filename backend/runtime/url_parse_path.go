package runtime

import (
	"strings"
	"unicode/utf8"
)

// The basic URL parser's states for file URLs, the path, the query and the
// fragment (url_parse.go has the rest), and the URL serializer.

func (p *urlParser) file(c rune) {
	url, base := p.url, p.base
	url.scheme, url.host, url.hasHost = "file", "", true
	switch {
	case c == '/' || c == '\\':
		p.state = stFileSlash
	case base != nil && base.scheme == "file":
		url.host, url.hasHost = base.host, base.hasHost
		url.path = append([]string(nil), base.path...)
		url.query, url.hasQuery = base.query, base.hasQuery
		switch c {
		case '?':
			p.enter(stQuery)
		case '#':
			p.enter(stFragment)
		case eof:
		default:
			url.query, url.hasQuery = "", false
			if !p.startsWithDriveLetter() {
				p.shortenPath()
			} else {
				url.path = nil
			}
			p.state = stPath
			p.p--
		}
	default:
		p.state = stPath
		p.p--
	}
}

func (p *urlParser) fileSlash(c rune) {
	if c == '/' || c == '\\' {
		p.state = stFileHost
		return
	}
	if base := p.base; base != nil && base.scheme == "file" {
		p.url.host, p.url.hasHost = base.host, base.hasHost
		if !p.startsWithDriveLetter() && len(base.path) > 0 && isNormalizedDriveLetter(base.path[0]) {
			p.url.path = append(p.url.path, base.path[0])
		}
	}
	p.state = stPath
	p.p--
}

func (p *urlParser) fileHost(c rune) urlStep {
	if c != eof && c != '/' && c != '\\' && c != '?' && c != '#' {
		p.buf = utf8.AppendRune(p.buf, c)
		return stepOn
	}
	p.p--
	switch {
	case p.override == noOverride && isDriveLetter(string(p.buf)):
		// The buffer isn't reset: the path state takes it as the drive.
		p.state = stPath
	case len(p.buf) == 0:
		p.url.host, p.url.hasHost = "", true
		if p.override != noOverride {
			return stepReturn
		}
		p.state = stPathStart
	default:
		host, ok := parseHost(string(p.buf), p.url.special())
		if !ok {
			return stepFail
		}
		if host == "localhost" {
			host = ""
		}
		p.url.host, p.url.hasHost = host, true
		if p.override != noOverride {
			return stepReturn
		}
		p.buf = p.buf[:0]
		p.state = stPathStart
	}
	return stepOn
}

func (p *urlParser) pathStart(c rune) {
	url := p.url
	switch {
	case url.special():
		p.state = stPath
		if c != '/' && c != '\\' {
			p.p--
		}
	case p.override == noOverride && c == '?':
		p.enter(stQuery)
	case p.override == noOverride && c == '#':
		p.enter(stFragment)
	case c != eof:
		p.state = stPath
		if c != '/' {
			p.p--
		}
	case p.override != noOverride && !url.hasHost:
		url.path = append(url.path, "")
	}
}

func (p *urlParser) path(c rune) {
	url := p.url
	slash := c == '/' || url.special() && c == '\\'
	if !slash && c != eof && (p.override != noOverride || c != '?' && c != '#') {
		p.buf = appendEncoded(p.buf, c, setPath)
		return
	}
	segment := string(p.buf)
	switch {
	case isDoubleDot(segment):
		p.shortenPath()
		if !slash {
			url.path = append(url.path, "")
		}
	case isSingleDot(segment):
		if !slash {
			url.path = append(url.path, "")
		}
	default:
		if url.scheme == "file" && len(url.path) == 0 && isDriveLetter(segment) {
			segment = segment[:1] + ":"
		}
		url.path = append(url.path, segment)
	}
	p.buf = p.buf[:0]
	switch c {
	case '?':
		p.enter(stQuery)
	case '#':
		p.enter(stFragment)
	}
}

// opaquePath is the opaque path state: a space before a `?` or a `#` is
// escaped, so that the path never ends in one (the standard since 2025;
// whatwg-url 13 stripped them instead).
func (p *urlParser) opaquePath(c rune) {
	switch c {
	case '?', '#', eof:
		p.url.opaquePath = string(p.buf)
		if c == '?' {
			p.enter(stQuery)
		} else if c == '#' {
			p.enter(stFragment)
		}
	case ' ':
		if next := p.at(1); next == '?' || next == '#' {
			p.buf = append(p.buf, "%20"...)
		} else {
			p.buf = append(p.buf, ' ')
		}
	default:
		p.buf = appendEncoded(p.buf, c, setC0)
	}
}

func (p *urlParser) query(c rune) {
	if c != eof && (p.override != noOverride || c != '#') {
		p.buf = utf8.AppendRune(p.buf, c)
		return
	}
	set := setQuery
	if p.url.special() {
		set = setSpecialQuery
	}
	p.url.query += encodeString(string(p.buf), set)
	p.buf = p.buf[:0]
	if c == '#' {
		p.enter(stFragment)
	}
}

// shortenPath removes the path's last segment, but not a file URL's drive,
// nor the first segment of an opaque path a relative URL is resolved
// against, which has no slash before it for Ada to cut at (noScheme).
func (p *urlParser) shortenPath() {
	path := p.url.path
	if p.url.scheme == "file" && len(path) == 1 && isNormalizedDriveLetter(path[0]) || p.joinPath && len(path) == 1 {
		return
	}
	if len(path) > 0 {
		p.url.path = path[:len(path)-1]
	}
}

// startsWithDriveLetter is whether what is left of the input, from the
// pointer, starts with a Windows drive letter.
func (p *urlParser) startsWithDriveLetter() bool {
	rest := p.in[p.p:]
	if len(rest) < 2 || !isASCIIAlpha(rest[0]) || rest[1] != ':' && rest[1] != '|' {
		return false
	}
	return len(rest) == 2 || rest[2] == '/' || rest[2] == '\\' || rest[2] == '?' || rest[2] == '#'
}

func isDriveLetter(s string) bool {
	return len(s) == 2 && isASCIIAlpha(rune(s[0])) && (s[1] == ':' || s[1] == '|')
}

func isNormalizedDriveLetter(s string) bool { return isDriveLetter(s) && s[1] == ':' }

func isSingleDot(s string) bool { return s == "." || strings.EqualFold(s, "%2e") }

func isDoubleDot(s string) bool {
	switch strings.ToLower(s) {
	case "..", ".%2e", "%2e.", "%2e%2e":
		return true
	}
	return false
}

// pathname is the URL path serializer.
func (r *urlRecord) pathname() string {
	if r.opaque {
		return r.opaquePath
	}
	n := 0
	for _, s := range r.path {
		n += len(s) + 1
	}
	b := make([]byte, 0, n)
	for _, s := range r.path {
		b = append(b, '/')
		b = append(b, s...)
	}
	return string(b)
}

// hostPort is the host, and its port when it has one.
func (r *urlRecord) hostPort() string {
	if r.port == -1 {
		return r.host
	}
	return r.host + ":" + portString(r.port)
}

// href is the URL serializer.
func (r *urlRecord) href() string {
	var b strings.Builder
	b.WriteString(r.scheme)
	b.WriteByte(':')
	if r.hasHost {
		b.WriteString("//")
		if r.includesCredentials() {
			b.WriteString(r.username)
			if r.password != "" {
				b.WriteByte(':')
				b.WriteString(r.password)
			}
			b.WriteByte('@')
		}
		b.WriteString(r.hostPort())
	} else if !r.opaque && len(r.path) > 1 && r.path[0] == "" {
		b.WriteString("/.")
	}
	b.WriteString(r.pathname())
	if r.hasQuery {
		b.WriteByte('?')
		b.WriteString(r.query)
	}
	if r.hasFragment {
		b.WriteByte('#')
		b.WriteString(r.fragment)
	}
	return b.String()
}

// origin is the ASCII serialization of the URL's origin: scheme, host and
// port for the special schemes but file, a blob's inner http(s) URL's, and
// "null" for an opaque origin.
func (r *urlRecord) origin() string {
	switch r.scheme {
	case "blob":
		inner, ok := basicParse(r.pathname(), nil, nil, noOverride)
		if ok && (inner.scheme == "http" || inner.scheme == "https") {
			return inner.origin()
		}
	case "ftp", "http", "https", "ws", "wss":
		return r.scheme + "://" + r.hostPort()
	}
	return "null"
}
