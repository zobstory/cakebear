// gen-url-goldens.mjs: a golden corpus of WHATWG's URL and URLSearchParams,
// recorded from Node 24.18.0, for cakebear's runtime (url*.go; S1 plan,
// s1-node's R24, R25 and R38–R43).
//
//   node gen-url-goldens.mjs
//
// writes url-goldens.jsonl and url-goldens-index.json beside this file. No
// dependencies, and deterministic: the random parts run on a seeded
// mulberry32, so a rerun writes a byte-identical file. Errors are made
// under Error.stackTraceLimit = 0, as --stack-trace-limit=0 makes them.
//
// A string is itself, or {"u":[<code unit>…]} when it holds a lone
// surrogate. Each line is one of these, by "k":
//   p      new URL(in), or new URL(in, base), or with "bu" new URL(in, new
//          URL(base)): "r", its eleven getters (href, origin, protocol,
//          username, password, host, hostname, port, pathname, search,
//          hash) and searchParams.toString(), or "err", util.inspect of what
//          it threw. URL.canParse and URL.parse agree with it: this script
//          checks they do under Node, and the Go test that they do there.
//   set    new URL(base), then u[op] = v: "r" or "err", as for p.
//   usp    new URLSearchParams(init), or of the pairs "pairs" (name, value,
//          …), then each of "ops", [method, …args]: "out" is, for each, its
//          result ({"v":1} for none) and toString() after it.
//   live   new URL(base), then each of "ops": ["u", setter, v] or ["p",
//          method, …args] on its searchParams; "out" is, after each, href,
//          search, searchParams.toString() and size.
//   form   "in" as a query: "ser", toString() of the one pair [in, in];
//          "dec", the pairs new URLSearchParams(in) parses, flat; "round",
//          its toString().
//   insp   util.inspect of the value "v" describes: "out".
//   json   JSON.stringify of it, "out", and with a space of 2, "out2".
// A value "v" is {"url":s} or {"url":s,"base":s}, {"parse":s} (URL.parse,
// so perhaps null), {"usp":s}, {"uspp":[name, value, …]}, {"s":s}, {"n":1},
// {"o":[[key, value], …]} or {"a":[value, …]}.
//
// The index maps each section to its 1-based line range. IDNA holds what
// depends on UTS 46's tables, a non-ASCII host or an `xn--` label, which
// cakebear doesn't carry (S1 plan D6): its differences are accepted, and
// the Go test lists them rather than failing on them. INCONSISTENT holds
// the setters after which Node's href parses to another URL, its parser's
// bug, where the runtime follows the standard: accepted too.

import util from 'node:util';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

if (process.version !== 'v24.18.0') {
  throw new Error(`recorded against Node v24.18.0, this is ${process.version}`);
}
Error.stackTraceLimit = 0;

const HERE = path.dirname(fileURLToPath(import.meta.url));
const OUT = path.join(HERE, 'url-goldens.jsonl');
const INDEX = path.join(HERE, 'url-goldens-index.json');
const SEED = 0x2026_1111;

const units = (s) => Array.from({ length: s.length }, (_, i) => s.charCodeAt(i));
const X = (s) => (s.isWellFormed() ? s : { u: units(s) });
const dec = (x) => (typeof x === 'string' ? x : String.fromCharCode(...x.u));
const GETTERS = ['href', 'origin', 'protocol', 'username', 'password', 'host', 'hostname', 'port', 'pathname', 'search', 'hash'];
const record = (u) => [...GETTERS.map((g) => u[g]), u.searchParams.toString()].map(X);

function mulberry32(seed) {
  let a = seed >>> 0;
  return function next() {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const rnd = mulberry32(SEED);
const pick = (xs) => xs[Math.floor(rnd() * xs.length)];
const int = (n) => Math.floor(rnd() * n);

const lines = [];
const index = {};
const seen = new Set();
let section = null;

function begin(name) {
  section = name;
  index[name] = { first: lines.length + 1, last: lines.length, count: 0, errors: 0 };
}

function emit(line, key) {
  if (key !== undefined) {
    if (seen.has(key)) return;
    seen.add(key);
  }
  lines.push(JSON.stringify(line));
  index[section].count++;
  index[section].last = lines.length;
  if ('err' in line) index[section].errors++;
}

function invalid(e) {
  if (!(e instanceof TypeError) || e.code !== 'ERR_INVALID_URL') throw e;
  return X(util.inspect(e));
}

// parse records new URL(input[, base]), checking canParse and parse agree.
function parse(input, base, baseURL = false) {
  const key = JSON.stringify(['p', input, base, baseURL]);
  if (seen.has(key)) return;
  const line = { k: 'p', in: X(input) };
  if (base !== undefined) {
    line.base = X(base);
    if (baseURL) line.bu = true;
  }
  const b = baseURL ? new URL(base) : base;
  let u, err;
  try {
    u = base === undefined ? new URL(input) : new URL(input, b);
  } catch (e) {
    err = e;
  }
  const can = base === undefined ? URL.canParse(input) : URL.canParse(input, b);
  const parsed = base === undefined ? URL.parse(input) : URL.parse(input, b);
  if (can === !!err || (parsed === null) !== !!err || (parsed && parsed.href !== u.href)) {
    throw new Error(`canParse or parse disagrees with the constructor on ${JSON.stringify([input, base])}`);
  }
  if (err) line.err = invalid(err);
  else line.r = record(u);
  if (!err && !consistent(u)) throw new Error(`new URL gave an href that parses to another: ${JSON.stringify([input, base])}`);
  emit(line, key);
}

// A setter can leave Node's URL in a state its own parser wouldn't make:
// an href that parses to a different URL. Those lines go to the
// INCONSISTENT section, where the runtime follows the standard instead.
const inconsistent = [];
const consistent = (u) => URL.parse(u.href)?.href === u.href;

function setter(base, op, v) {
  const key = JSON.stringify(['set', base, op, v]);
  if (seen.has(key)) return;
  const u = new URL(base);
  const line = { k: 'set', base: X(base), op, v: X(v) };
  try {
    u[op] = v;
    line.r = record(u);
  } catch (e) {
    line.err = invalid(e);
  }
  if (line.r && !consistent(u)) {
    seen.add(key);
    inconsistent.push(line);
    return;
  }
  emit(line, key);
}

// ---------------------------------------------------------------------------
// Inputs.

// Code points to put in each component: all of ASCII, and a spread of the
// rest, lone surrogates among them.
const ASCII = Array.from({ length: 128 }, (_, c) => String.fromCharCode(c));
const WIDE = [0x80, 0x9f, 0xa0, 0xad, 0xe9, 0xff, 0x100, 0x2fe, 0x7ff, 0x800, 0x200b, 0x2028, 0x3000, 0x3002, 0xd7ff,
  0xe000, 0xfeff, 0xff0e, 0xfffd, 0xffff, 0x10000, 0x1f600, 0x10fffd, 0x10ffff].map((c) => String.fromCodePoint(c));
const LONE = ['\ud800', '\udbff', '\udc00', '\udfff', '\udc00\ud800'];
const CHARS = [...ASCII, ...WIDE, ...LONE];
const isASCII = (s) => /^[\x00-\x7f]*$/.test(s);
const pct = (s) => [...Buffer.from(s, 'utf8')].map((b) => '%' + b.toString(16).toUpperCase().padStart(2, '0')).join('');

// Each template puts x in one component; a host's own non-ASCII goes to the
// IDNA section.
const TEMPLATES = [
  [(x) => `http://u${x}v:p@h/`], [(x) => `http://u:p${x}q@h/`], [(x) => `sc://u${x}v@h/`],
  [(x) => `http://h/a${x}b/c`], [(x) => `sc://h/a${x}b/c`], [(x) => `sc:/a${x}b`], [(x) => `sc:a${x}b`],
  [(x) => `http://h/?a${x}b`], [(x) => `sc://h/?a${x}b`], [(x) => `ws://h/?a${x}b`],
  [(x) => `http://h/#a${x}b`], [(x) => `sc:a#a${x}b`], [(x) => `sc://a${x}b/`],
  [(x) => `http://a${x}b/`, true], [(x) => `http://a${pct(x)}b/`, true], [(x) => `file://a${x}b/c`, true],
  [(x) => `http://h:8${x}0/`], [(x) => `${x}http://h/`], [(x) => `ht${x}tp://h/`], [(x) => `file:///a${x}b`],
  [(x) => `http://[::${x}1]/`],
];

const idnaInputs = [];

function sectionComponents() {
  begin('COMPONENTS');
  for (const [t, host] of TEMPLATES) {
    for (const x of CHARS) {
      const input = t(x);
      if (host && !isASCII(x)) idnaInputs.push(input);
      else parse(input);
    }
  }
}

function sectionHosts() {
  begin('HOSTS');
  const PARTS = ['0', '1', '9', '10', '127', '255', '256', '4294967295', '4294967296', '0x0', '0xff', '0x100', '0XFF', '0x',
    '00', '010', '0377', '0400', '08', '09', '0xg', '', 'a', '1a', 'foo', '99999999999999999999', '0x00000000000000ff'];
  const fixed = ['a.b.c.09', 'foo.0x', 'foo.09', 'foo.1', '1.foo', 'foo.1.', 'foo.1..', '0..0x300', '1.2.3.4.5', '1.2.3.4.',
    '1.2.3.4..', '.1.2.3', '127.0.0.1', '127.1', '127.0.1', '0x7f000001', '017700000001', '0x7f.1', 'EXAMPLE.COM.', 'a..b',
    '.', '..', 'localhost', 'LOCALHOST', 'xn-a.com', 'a-.com', '-a.com', 'a_b.com', 'a$b.com', 'a%41b', 'a%2Eb', '%2E',
    'a%20b', 'a%00b', 'a%7Fb', 'a%25b', '%zz', 'a%', 'a%2', 'a%2G'];
  const hosts = new Set(fixed);
  while (hosts.size < 1500) {
    const n = 1 + int(5);
    let h = Array.from({ length: n }, () => pick(PARTS)).join('.');
    if (rnd() < 0.2) h += '.';
    hosts.add(h);
  }
  for (const h of hosts) {
    parse(`http://${h}/`);
    if (rnd() < 0.15) parse(`sc://${h}/`);
  }
  for (const h of fixed) {
    parse(`sc://${h}/`);
    parse(`file://${h}/x`);
  }
  const v6 = ['[', '[]', '[:]', '[::]', '[:::]', '[1::2::3]', '[1:2:3:4:5:6:7:8:9]', '[1:2:3:4:5:6:7]', '[12345::]', '[::1:]',
    '[:1::]', '[::1.2.3.4]', '[::1.2.3]', '[::1.2.3.4.5]', '[::01.2.3.4]', '[::1.2.3.256]', '[1:2:3:4:5:6:7:1.2.3.4]',
    '[1:2:3:4:5:6:1.2.3.4]', '[::ffff:1.2.3.4]', '[::ffff:0:0]', '[::g]', '[::1%eth0]', '[::1]x', 'x[::1]', '[::1', '::1]',
    '[0:0:0:0:0:0:0:0]', '[1:0:0:2:0:0:0:3]', '[1:0:0:2:0:0:3:4]', '[1.2.3.4]', '[::.1.2.3]', '[::1.]', '[::1..2.3]',
    '[0:0:0:0:0:0:13.1.68.3]', '[1:0::]', '[::1]', '[::]', '[FFFF::]', '[0001:0002::0003]', '[::1:2:3:4:5:6:7]', '[1::1.2.3.4]',
    '[1:2::3:4:5:6:7:8]', '[a:b:c:d:e:f:1.2.3.4]', '[::1.2.3.04]', '[::0.0.0.0]', '[::1.2.3.4x]', '[1::2:]', '[ 1::]',
    '[::%31]', '[0::0::0]', '[]x', '[\u00e9]'];
  for (const h of v6) {
    parse(`http://${h}/`);
    parse(`sc://${h}/`);
    parse(`http://${h}:8080/p`);
  }
  for (let i = 0; i < 600; i++) {
    const p = Array.from({ length: 8 }, () => (rnd() < 0.45 ? 0 : rnd() < 0.5 ? int(256) : int(65536)));
    const hex = p.map((x) => x.toString(16));
    let form;
    switch (int(5)) {
      case 0: form = hex.join(':'); break;
      case 1: form = hex.map((x) => x.toUpperCase().padStart(4, '0')).join(':'); break;
      case 2: {
        const at = int(8), len = int(8 - at) + 1;
        form = hex.slice(0, at).join(':') + '::' + hex.slice(at + len).join(':');
        break;
      }
      case 3: form = hex.slice(0, 6).join(':') + ':' + [p[6] >> 8, p[6] & 255, p[7] >> 8, p[7] & 255].join('.'); break;
      default: {
        const at = int(6);
        form = hex.slice(0, at).join(':') + '::' + [p[6] >> 8, p[6] & 255, p[7] >> 8, p[7] & 255].join('.');
      }
    }
    parse(`http://[${form}]/`);
  }
  const PORTS = ['', '0', '1', '21', '80', '443', '8080', '65535', '65536', '99999', '000080', '0443', '-1', '1a', '+1',
    ' 1', '1 ', '0x50', '80.0', '1e3', '18446744073709551616'];
  for (const scheme of ['http', 'https', 'ws', 'wss', 'ftp', 'file', 'sc']) {
    for (const port of PORTS) parse(`${scheme}://h:${port}/p`);
    parse(`${scheme}://u@h:80/p`);
    parse(`${scheme}://:443`);
    parse(`${scheme}://h:/`);
  }
}

function sectionPaths() {
  begin('PATHS');
  const SEGS = ['a', 'b', '.', '..', '%2e', '%2E', '.%2e', '%2e.', '%2E%2e', '...', '', '.a', 'a.', 'C:', 'C|', 'c:', '%2e%2e%2f'];
  const PREFIX = ['http://h/', 'sc://h/', 'sc:/', 'file:///', 'file://host/', 'sc://h', 'http://h\\', 'file:/C:/'];
  for (let i = 0; i < 2000; i++) {
    const n = 1 + int(5);
    const sep = rnd() < 0.8 ? '/' : '\\';
    let p = Array.from({ length: n }, () => pick(SEGS)).join(sep);
    if (rnd() < 0.2) p += pick(['?q', '#f', '/', '\\']);
    parse(pick(PREFIX) + p);
  }
}

const BASES = ['http://u:p@h:81/a/b/c?q=1#f', 'https://h/', 'http://h', 'sc://h/a/b', 'sc://h', 'sc:/a/b', 'sc:opaque?q#f',
  'file:///C:/a/b', 'file://host/a/b', 'file:///', 'file:///a/b?q#f', 'data:text/plain,x', 'https://example.com/dir/page.html?old=1#oldhash',
  'ws://h/x/', 'blob:https://h/uuid'];
const RELATIVE = ['', ' ', '.', '..', './', '../', '../x', '../../../../x', 'x', 'x/', '/x', '//h2/x', '///x', '////x', '?q', '??',
  '#f', '##', '?#', '\\x', '\\\\h2\\x', '/\\h2', 'http:x', 'http:/x', 'http://x', 'https:x', 'sc:x', 'sc:/x', 'file:x',
  'file:/x', 'file:', 'C:/x', '/C:/x', 'C|', 'C|/x', '//C:/', 'C:', '/C|', '..\\..', ' x ', 'x\ty', '%2e%2e', '.%2e/x',
  'a?b#c', 'é', ':x', '#\u00e9', '?\u00e9', 'other.html', '/root', '../up', './same', '?q=2', '#h2', '//other.host/x',
  'https://abs.example/', 'http:', 'HTTP:x', 'ws:x', 'wss:', 'ftp:x', 'blob:x', 'javascript:x', '\\', '\\\\', '/..', '/./x'];

function sectionRelative() {
  begin('RELATIVE');
  for (const base of BASES) {
    for (const input of RELATIVE) {
      parse(input, base);
      if (rnd() < 0.25) parse(input, base, true);
    }
  }
  for (const base of ['not a base', '', 'mailto:x@y.test', 'sc:', '/rel', 'http://', 'http://[::1/']) {
    for (const input of ['x', '#f', '?q', 'http://ok.test/', '', 'sc:x']) parse(input, base);
  }
  parse('x', 'http://b.test/a/', true);
  // Against an opaque path, an input with a fragment past its start: the
  // standard fails, and Node's parser resolves it (url_parse.go, noScheme).
  for (const base of ['sc:opaque', 'sc:a/b/c', 'sc:a/b/', 'data:text/plain,x', 'mailto:x@y', 'sc:op?q', 'sc:op#f', 'sc:a b',
    'sc:a%2Fb/c', 'sc:', 'sc:a/..']) {
    for (const input of ['x#f', 'a/b#f', '../x#f', '?q#f', '/x#f', '//h/x#f', 'a?b#c', '#f', 'x/#', './x#', '\\x#f', '?#', '#',
      'x', '?q', '..#', 'x/../y#', '%2e#', 'sc:x#', '', ' #', 'a b#f', 'a^b#f', '../../..#', 'x?#', 'é#']) {
      parse(input, base);
    }
  }
}

function sectionMisc() {
  begin('MISC');
  const inputs = [
    // s1-node's case 22 and 25.
    'https://user:pw@example.com:8080/a/b?x=1&y=two#frag', 'HTTPS://EXAMPLE.COM/Path', 'http://example.com:80/',
    'https://example.com:443', 'http://example.com:8080', 'http://example.com', 'http://example.com/a/../b/./c',
    'http://example.com/a b?q=a b#h a', 'http://example.com/café?q=é#é', 'http://127.0.0.1:18900/echo?',
    'http://[::1]:3000/x', 'http://example.com/?', 'http://example.com/#', 'http://example.com/%7e%41?%41#%41',
    'http://example.com/a?q=1&q=2&empty=&flag', '  http://example.com/trim  ', 'http:example.com/no-slashes',
    'http://example.com\\back\\slash', 'mailto:someone@example.com', 'file:///etc/hosts', 'data:text/plain,hi',
    'blob:https://example.com/uuid', 'ws://example.com/socket', 'custom-scheme://host/p?q#h',
    'not a url', '', '/relative', 'http://', 'http://exa mple.com/', 'http://example.com:99999/', 'http://[::1/',
    'https://a.test:-1/',
    // trimming, tabs and newlines
    '\u0000http://h/\u001f', ' \t\nhttp://h/ \r', 'ht\ttp://h\n/a\rb', 'http://h/a\t b', '\u0001\u0002 sc:x \u0003',
    '\u007fhttp://h/', 'http://h/\u007f', 'http://h/ ', '\u00a0http://h/', 'http://h\t:8\n0/',
    // authority
    'http://@h/', 'http://:@h/', 'http://a:b@c:d@e/', 'http://a@b@c/', 'http://a:b:c@h/', 'http://@/', 'http://a@/',
    'sc://@/', 'sc://@h/', 'sc://a:b@/', 'http://h:80:80/', 'http://[::1]:', 'http://h:/', 'http://user@h:99999',
    'http://%41@h/', 'http://ü@h/', 'http://u@h/x@y', 'http:@h', 'http:a@b@c',
    // schemes
    'a:', 'A:', 'aA1+-.:x', '1a:x', '+a:x', 'a b:x', 'a\u00e9:x', 'sc:', 'sc://', 'sc:///', 'sc:////x', 'sc:///x',
    'http:', 'http:/', 'http://', 'http:///h', 'http:////h', 'http:\\\\h\\p', 'http:/h', 'https:h', 'ws:h', 'wss:/h',
    'ftp:/h', 'ftp://h:21/', 'file:', 'file:/', 'file://', 'file:///', 'file:////x', 'file://localhost', 'file://localhost/x',
    'file://LOCALHOST/x', 'file://h:1/x', 'file://u@h/x', 'file://[::1]/x', 'file://1.2.3/x',
    // file and drive letters
    'file:///C|/x', 'file:c:\\x\\y', 'file://host/C:/x/../..', 'file:///C:/../..', 'file:///./y:', 'file:///./y:/',
    'file:/C|/foo/bar', 'file://C:/x', 'file://C|/x', 'file:C|', 'file:/C:', 'file:///C:', 'file:///c:/x/../../..',
    'file:///C%3A/x', 'file:///C%7C/x', 'file:..', 'file:a/../..', 'file:///a/%2e%2E/b', 'file:\\\\h\\s', 'file:\\x',
    'file:?q', 'file:#f', 'file:///?q#f',
    // opaque paths
    'sc:a b', 'sc:a b?x', 'sc:a b#x', 'sc:a  ?q', 'sc:a  #f', 'sc:  x', 'sc:a\u0001b', 'sc:a%zzb', 'sc:a b ', 'sc:a ?',
    'mailto:a@b?subject=hi there', 'javascript:alert(1)', 'data:text/html,<b>hi</b>', 'sc:\\x', 'sc:a/../b',
    'urn:isbn:0451450523', 'tel:+1-201-555-0123', 'sc:#', 'sc:?',
    // non-special
    'non-spec:/.//p', 'non-spec:/..//p', 'web+demo:/.//not-a-host/', 'sc://h/./..', 'sc://h/..', 'sc:/..', 'sc:/./..//x',
    'sc://[1:2::3]:80/', 'sc://[v]/', 'sc://a[b/', 'sc://a]b/', 'sc://h:abc/', 'sc://h:99999/', 'sc://h:0/', 'sc://\\h/',
    'sc:\\\\h', 'sc://h\\p', 'sc://a b/', 'sc://a%20b/', 'sc://a<b/', 'sc://a^b/', 'sc://a|b/', 'sc://a%00b/',
    'sc://1.2.3.4/', 'sc://0x7f.1/', 'sc://EXAMPLE/', 'sc://ü/', 'foo://///', 'foo://h//p',
    // blob
    'blob:http://a.com:81/x', 'blob:file:///x', 'blob:ftp://a/x', 'blob:ws://a/', 'blob:bad', 'blob:blob:https://a/',
    'blob:https://u:p@a.com:443/x', 'blob:', 'blob:http:a',
    // queries and fragments
    'http://h/a?b#c?d#e', 'http://h#', 'http://h?', 'http://h?#', 'http://h/?a=b&c', "http://h/?a='b", "sc://h/?a='b",
    'http://h/#a#b', 'http://h/?%zz', 'http://h/#%zz', 'http://h/?a=%20b+c', 'http://h/??', 'http://h/?&&',
    'ws://h/?a\'b', 'wss://h/?a"b', 'ftp://h/?a\'b',
    // long
    'http://h/' + 'a'.repeat(300), 'http://h/?' + 'q'.repeat(300) + '=' + 'v'.repeat(300),
  ];
  for (const s of inputs) parse(s);
}

// IDNA: a non-ASCII host, as each component template made one, and these.
function sectionIDNA() {
  begin('IDNA');
  parse('http://exämple.com/');
  const hosts = ['exämple.com', 'EXÄMPLE.COM', 'ÄÖÜ.de', 'faß.de', 'ﬁ.com', 'a\u3002b.com', 'a\uff0eb.com', 'a\uff61b.com',
    '\u00ad.com', 'a\u00adb.com', '日本語.jp', '💩.la', 'xn--ls8h.la', 'xn--a.com', 'xn--', 'xn--abc', 'xn--exmple-cua.com',
    'XN--EXMPLE-CUA.COM', 'xn--zca.de', 'xn--fa-hia.de', 'xn--abc-.com', 'xn--99999999999.com', 'xn--a-ecp.ru',
    'مثال.إختبار', 'a\u200db.com', 'a\u200cb.com', '\u0301a.com', 'İstanbul.tr', 'ΣΑΣ.gr', 'ς.gr', 'ＡＢＣ.com', '＜.com',
    'a\u2028b', '\ufffd.com', '\ud800.com', 'a\ue000.com', '\u{10ffff}.com', 'a\u00a0b', 'a\u3000b', 'ex%C3%A4mple.com',
    '%E2%80%8B.com', '%F0%9F%92%A9.la', '%C3%A4%00.com', 'пример.рф', 'ПРИМЕР.РФ', '例え.テスト', 'münchen.de',
    'xn--mnchen-3ya.de', 'MÜNCHEN.DE', 'a.ü.', 'ü..b', 'ẞ.de', 'a\u0300.com', 'ⓐ.com', 'a\u2024b', '¹.com', 'a\u2060b.com',
    'a\ufe0fb.com', 'a\ufeffb.com', 'xn--1.com', 'xn--55qx5d.cn', 'xn--55qx5d.xn--55qx5d', '例子.xn--', 'ü.0x10', 'ü.1'];
  for (const h of hosts) {
    for (const scheme of ['http', 'ws']) parse(`${scheme}://${h}/p`);
    parse(`file://${h}/p`);
    parse(`https://u@${h}:8080/p?q#f`);
  }
  const words = ['é', 'ß', 'ü', 'ø', 'ł', 'ж', 'λ', 'Ω', '中', '日', 'ア', '한', 'ا', 'ע', '😀', 'ﬀ', 'Ⅻ', 'ǅ'];
  for (let i = 0; i < 300; i++) {
    const n = 1 + int(3);
    const label = () => Array.from({ length: 1 + int(6) }, () => (rnd() < 0.5 ? pick(words) : pick(['a', 'b', 'x', '1', '-']))).join('');
    parse(`http://${Array.from({ length: n }, label).join('.')}/`);
  }
  for (const s of idnaInputs) parse(s);
  for (const base of ['http://h/', 'https://u:p@h:81/a?q#f']) {
    for (const v of ['exämple', 'EXÄMPLE.com:99', 'xn--a', 'xn--exmple-cua', '💩', 'a\u3002b', 'ü:1']) {
      setter(base, 'host', v);
      setter(base, 'hostname', v);
    }
    setter(base, 'href', 'http://exämple.com/');
  }
}

// ---------------------------------------------------------------------------
// Setters.

function sectionSetters() {
  begin('SETTERS');
  const bases = ['https://user:pw@example.com:8080/a/b?x=1&y=two#frag', 'http://h/', 'http://h', 'sc://h:1/p?q#f', 'sc://h',
    'sc:/p', 'sc:opaque?q#f', 'sc:a ?q', 'file:///C:/a', 'file://host/x', 'file:///', 'data:text/plain,hi',
    'http://[::1]:3000/x', 'ws://h:80/', 'mailto:x@y', 'sc://u:p@h/', 'blob:https://h/x', 'http://1.2.3.4/',
    'https://example.com/a?x=1#h', 'sc://h/?a=1&b=2', 'sc:/', 'sc:/.//p', 'sc:/a/b', 'sc://h/p', 'sc:/p?q#f'];
  const ASCII_RUN = ASCII.slice(32, 127).join('');
  const values = {
    href: ['http://new.test/n?m=1', 'not a url', '', 'sc:x', '/rel', 'http://h:99999/', 'x\ud800', 'HTTPS://A.B/C?D#E',
      '  http://trim/  ', 'file:///C|/x', 'http://[::1/'],
    protocol: ['http', 'https:', 'ws', 'wss', 'ftp', 'file', 'sc', 'SC', 'a+b', '1a', '', ':', 'https:x', 'h t', 'mailto',
      'http\t', '\thttp', 'javascript', 'http:', 'blob', 'sd', 'é'],
    username: ['', 'me', 'me@x', 'a:b', 'a b', '%41', 'é', '\ud800', '/?#', ASCII_RUN, '\u0000\u001f'],
    password: ['', 'pw', 'p:w', 'p@w', 'a b', '%zz', 'é', '\ud800', ASCII_RUN],
    host: ['', 'b', 'b:99', 'b:', ':2', 'b:99x', 'b:99999', 'B.COM', 'b c', 'b/c', 'b?c', 'b#c', 'b\\c', '[::1]', '[::1]:5',
      '1.2.3', '0x7f.1', 'a%41', '%zz', 'a@b', 'a:b@c', '\tb\n', 'localhost', 'b:80', 'b:443', 'b:0', '[::1', 'b:-1',
      'a^b', 'a<b', '.', 'b:8\t0', ASCII_RUN],
    hostname: ['', 'b', 'b:99', 'B.COM', 'b c', 'b/c', 'b?c', 'b#c', 'b\\c', '[::1]', '1.2.3', '0x7f.1', 'a%41', '%zz', 'a@b',
      '\tb\n', 'localhost', '[::1]:5', '.', 'a^b'],
    port: ['', '0', '80', '443', '81', '81abc', 'abc', '99999', '65535', '65536', '-1', ' 1', '\t1', '1 ', '0x1', '08',
      '1.5', '+1', '00081', '8\t1', '\n'],
    pathname: ['', '/', 'x', '/x/y', 'a b', 'b c/d', '..', './x', '/a/../b', '%2e%2E', 'a?b#c', '\\a\\b', 'é', 'C:', 'C|/x',
      '//y', '/./y', '/café', '\ud800', ASCII_RUN, '/..//x', ' x '],
    search: ['', '?', '??', 'q', '?q=a b&r=é', 'a=%20b+c', '#x', "a'b", 'a\tb', '\ud800', '?a=1&a=2', '?lead=1',
      'q=a b&r=é', ASCII_RUN],
    hash: ['', '#', '##', 'x y', '#x', 'a#b', '`<>"', 'é', '\ud800', '\u0000', ASCII_RUN],
  };
  for (const base of bases) {
    for (const [op, vs] of Object.entries(values)) {
      for (const v of vs) setter(base, op, v);
    }
  }
}

// ---------------------------------------------------------------------------
// URLSearchParams.

const NAMES = ['a', 'b', 'c', '', 'A', 'é', '😀', '\ue000', 'a b', 'x=y', '\ud800', '\ufffd', 'b'];
const VALUES = ['1', '2', '', 'two words', 'é', '+', '&', '%', '\ud800', 'x'];

function applyParams(p, [m, ...a]) {
  switch (m) {
    case 'append': return p.append(a[0], a[1]);
    case 'delete': return a.length > 1 ? p.delete(a[0], a[1]) : p.delete(a[0]);
    case 'get': return p.get(a[0]);
    case 'getAll': return p.getAll(a[0]);
    case 'has': return a.length > 1 ? p.has(a[0], a[1]) : p.has(a[0]);
    case 'set': return p.set(a[0], a[1]);
    case 'sort': return p.sort();
    case 'size': return p.size;
    case 'toString': return p.toString();
    case 'forEach': {
      const out = [];
      p.forEach((v, k) => out.push(k, v));
      return out;
    }
  }
  throw new Error(`no method ${m}`);
}

function result(v) {
  if (v === undefined) return { v: 1 };
  if (typeof v === 'string') return X(v);
  if (Array.isArray(v)) return v.map(X);
  return v; // null, a boolean or a number
}

function randomOp() {
  const n = pick(NAMES), v = pick(VALUES);
  switch (int(11)) {
    case 0: case 1: return ['append', n, v];
    case 2: return ['delete', n];
    case 3: return ['delete', n, v];
    case 4: return ['get', n];
    case 5: return ['getAll', n];
    case 6: return rnd() < 0.5 ? ['has', n] : ['has', n, v];
    case 7: return ['set', n, v];
    case 8: return ['sort'];
    case 9: return ['size'];
    default: return ['forEach'];
  }
}

function sectionParams() {
  begin('PARAMS');
  const inits = ['', '?', 'a=1', '?a=1&b=2', 'b=2&a=1&b=3&c=&d&=e&a=4', 'a=%20b+c', '&&a=1&&', '=', '==', 'a==b', '%zz=%',
    '%FF=%C3%A9', 'a+b=c+d', '\ud800=x', 'é=日', '??a=1', 'z=1&a=2&m=3&a=4', 'a=1+2&b=1%202&c=%2B&d=%E6%97%A5&e=%zz&f=%&g=café&h=%FF',
    'b=1&a=1&b=2&a=2&\ue000=1&😀=1&\uffff=1&=1&B=1'];
  const run = (init, pairs, ops) => {
    const p = pairs ? new URLSearchParams(Array.from({ length: pairs.length / 2 }, (_, i) => [pairs[2 * i], pairs[2 * i + 1]])) : new URLSearchParams(init);
    const line = { k: 'usp' };
    if (pairs) line.pairs = pairs.map(X);
    else line.init = X(init);
    line.ops = ops.map((op) => op.map((x) => (typeof x === 'string' ? X(x) : x)));
    line.out = ops.map((op) => [result(applyParams(p, op)), X(p.toString())]);
    emit(line);
  };
  for (const init of inits) {
    run(init, null, [['toString'], ['size'], ['forEach'], ['get', 'a'], ['getAll', 'a'], ['get', ''], ['get', 'missing'],
      ['getAll', 'missing'], ['has', 'b'], ['has', 'b', '3'], ['sort'], ['forEach']]);
  }
  // s1-node's case 24, step by step.
  run('b=2&a=1&b=3&c=&d&=e&a=4', null, [['set', 'b', 'new'], ['append', 'a', '5'], ['delete', 'a', '1'], ['delete', 'a'],
    ['set', 'z', 'last'], ['sort']]);
  const encode = ['space', 'a b', 'plus', 'a+b', 'amp', 'a&b', 'eq', 'a=b', 'pct', '100%', 'hash', '#x', 'q', '?', 'slash', 'a/b',
    'tilde', '~', 'safe', '*-._', 'bang', "!'()", 'unicode', 'é日', 'emoji', '\u{1F600}', 'lone', '\ud800', 'nl', 'a\nb',
    'ctl', '\u0001', 'k e y', 'v'];
  run(null, encode, [['toString'], ['forEach']]);
  run(null, ['x', '1', 'y', 'two words'], [['toString']]);
  run(null, ['x', '1', 'x', '2', '\ud800', '\udc00'], [['toString'], ['get', '\ufffd'], ['getAll', '\ud800']]);
  run(null, [], [['toString'], ['size'], ['sort']]);
  // Sort's stability and its UTF-16 order: duplicates, and names either
  // side of U+E000 and past U+FFFF.
  const sortNames = ['b', 'a', 'B', '', '\ue000', '😀', '\uffff', '\ud7ff', 'a', 'é', 'e\u0301', 'b'];
  for (let i = 0; i < 60; i++) {
    const pairs = [];
    for (let j = 0; j < 3 + int(10); j++) pairs.push(pick(sortNames), String(j));
    run(null, pairs, [['sort'], ['forEach'], ['append', pick(sortNames), 'z'], ['sort'], ['toString']]);
  }
  for (let i = 0; i < 700; i++) {
    const init = rnd() < 0.5 ? pick(inits) : null;
    const pairs = init === null ? Array.from({ length: 2 * int(5) }, (_, j) => (j % 2 ? pick(VALUES) : pick(NAMES))) : null;
    run(init ?? '', pairs, Array.from({ length: 4 + int(9) }, randomOp));
  }
}

function sectionForm() {
  begin('FORM');
  const form = (s) => {
    const p = new URLSearchParams(s);
    emit({ k: 'form', in: X(s), ser: X(new URLSearchParams([[s, s]]).toString()), dec: [...p].flat().map(X), round: X(p.toString()) },
      JSON.stringify(['form', s]));
  };
  for (const c of CHARS) {
    form(c);
    form(`${c}=${c}&${c}`);
    form(`a${c}b=c${c}d`);
  }
  const escapes = ['%', '%2', '%zz', '%%', '%25', '%2B', '+', '%20', '%FF', '%C3', '%C3%A9', '%c3%a9', '%E6%97', '%E6%97%A5',
    '%ED%A0%80', '%ED%9F%BF', '%EE%80%80', '%F0%9F%98', '%F0%9F%98%80', '%F4%8F%BF%BF', '%F4%90%80%80', '%F5%80%80%80',
    '%C0%80', '%C1%BF', '%C2%80', '%E0%80%80', '%E0%A0%80', '%F0%80%80%80', '%F0%90%80%80', '%EF%BB%BF', '%EF%BB%BFa',
    '%80', '%BF', '%C3%28', '%E6%28%A5', '%F0%28%98%80', '%%41', '%4%41', '%x%41', 'a%2', 'a%2=b%', '%3D=%3D', '%26=%26'];
  for (const q of escapes) {
    form(q);
    form(`k=${q}&${q}=v`);
  }
  const BYTES = [0x00, 0x20, 0x25, 0x26, 0x2b, 0x3d, 0x41, 0x7f, 0x80, 0x8f, 0x90, 0x9f, 0xa0, 0xbf, 0xc0, 0xc1, 0xc2, 0xdf,
    0xe0, 0xe1, 0xed, 0xee, 0xef, 0xf0, 0xf1, 0xf4, 0xf5, 0xff];
  for (let i = 0; i < 1500; i++) {
    const n = 1 + int(6);
    form('k=' + Array.from({ length: n }, () => '%' + pick(BYTES).toString(16).padStart(2, '0')).join(''));
  }
}

// ---------------------------------------------------------------------------
// A URL and its searchParams, each changing the other.

function sectionLive() {
  begin('LIVE');
  const bases = ['http://h/?a=%20b+c', 'http://h/', 'http://h/?', 'http://h/?#f', "http://h/?a='b", 'sc://h/?x=1#f', 'sc:op?x=1',
    'sc:a ?q', 'https://example.com/a?x=1#h', 'http://x.test/p?k=a%20b&k2=c+d', 'file:///x?y=1'];
  const urlOps = [['u', 'search', ''], ['u', 'search', '?n=1&n=2'], ['u', 'search', 'a\tb=c'], ['u', 'search', '#x'],
    ['u', 'search', "q='"], ['u', 'hash', 'h'], ['u', 'hash', ''], ['u', 'href', 'http://new.test/?m=1&m=2'],
    ['u', 'href', 'http://new.test/'], ['u', 'pathname', '/p?q'], ['u', 'protocol', 'https'], ['u', 'host', 'other:1']];
  const run = (base, ops) => {
    const u = new URL(base);
    const p = u.searchParams;
    if (p !== u.searchParams) throw new Error('searchParams is a new object');
    const out = ops.map((op) => {
      if (op[0] === 'u') u[op[1]] = op[2];
      else applyParams(p, op.slice(1));
      return [X(u.href), X(u.search), X(p.toString()), p.size];
    });
    emit({ k: 'live', base: X(base), ops: ops.map((op) => op.map((x) => (typeof x === 'string' ? X(x) : x))), out });
  };
  run('https://example.com/a?x=1#h', [['p', 'set', 'm', 'a b'], ['u', 'search', 'a=%20b+c'], ['p', 'append', 'z', '1'],
    ['p', 'delete', 'a'], ['p', 'delete', 'z']]);
  for (const base of bases) {
    run(base, [['p', 'sort']]);
    run(base, [['p', 'delete', 'a'], ['p', 'delete', 'x'], ['p', 'delete', 'k'], ['p', 'delete', 'k2']]);
    for (const op of urlOps) run(base, [op, ['p', 'append', 'z', '1 2'], op]);
  }
  for (let i = 0; i < 250; i++) {
    const ops = Array.from({ length: 3 + int(6) }, () => (rnd() < 0.3 ? pick(urlOps) : ['p', ...randomOp()]));
    run(pick(bases), ops);
  }
}

// ---------------------------------------------------------------------------
// Printing and JSON.

function build(d) {
  if ('url' in d) return 'base' in d ? new URL(dec(d.url), dec(d.base)) : new URL(dec(d.url));
  if ('parse' in d) return URL.parse(dec(d.parse));
  if ('usp' in d) return new URLSearchParams(dec(d.usp));
  if ('uspp' in d) return new URLSearchParams(Array.from({ length: d.uspp.length / 2 }, (_, i) => [dec(d.uspp[2 * i]), dec(d.uspp[2 * i + 1])]));
  if ('s' in d) return dec(d.s);
  if ('n' in d) return d.n;
  if ('o' in d) {
    const o = {};
    for (const [k, v] of d.o) o[k] = build(v);
    return o;
  }
  if ('a' in d) return d.a.map(build);
  throw new Error(`bad description ${JSON.stringify(d)}`);
}

function sectionPrint() {
  const values = [];
  const urls = ['https://user:pw@example.com:8080/a/b?x=1&y=two#frag', 'http://127.0.0.1/', 'http://a.test/', "http://h/it's",
    "sc:a'b\"c", 'sc:a\'b"c`d', 'mailto:x@y', 'file:///C:/x', 'blob:https://h/x', 'http://[::1]:3000/x?q#f',
    'http://h/?' + Array.from({ length: 12 }, (_, i) => `key${i}=value number ${i}`).join('&'), 'http://h/' + 'p'.repeat(90),
    'http://h/' + 'p'.repeat(10050), 'http://h/?k=' + 'v'.repeat(70) + '%0A' + 'w'.repeat(20), 'http://h/?a=%27&b=%22&c=%60',
    'http://h/?%F0%9F%98%80=%E6%97%A5', 'sc:', 'data:,', 'http://h/?a=\\'];
  for (const s of urls) values.push({ url: X(s) });
  values.push({ url: 'x', base: 'http://b.test/a/' }, { parse: 'not a url' }, { parse: 'http://p.test/' });
  // A URLSearchParams around the break: one pair 'k' => 'v…' of inner
  // length L + 9, two, and many short ones.
  for (const L of [60, 66, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 119, 120]) values.push({ uspp: ['k', 'v'.repeat(L)] });
  for (const L of [30, 31, 32, 33, 34, 35]) values.push({ uspp: ['a', 'v'.repeat(L), 'b', 'v'.repeat(L)] });
  for (const L of [33, 34, 35]) values.push({ uspp: ['a', '😀'.repeat(L / 2 | 0) + 'v'.repeat(L % 2), 'b', 'v'.repeat(L)] });
  values.push({ usp: 'a=1&b=2&c=3&d=4&e=5&f=6&g=7' }, { usp: '' }, { usp: 'a=1&b=two words&a=3' },
    { uspp: Array.from({ length: 16 }, (_, i) => (i % 2 ? `value number ${i >> 1}` : `key${i >> 1}`)) },
    { uspp: ["it's", 'a"b', 'c`d', "e'f\"g", 'nl', 'a\nb', 'tab', 'a\tb', 'bs', 'a\\b', 'ctl', '\u0001\u007f\u009f'] },
    { uspp: ['k', 'x'.repeat(75) + '\n' + 'y'.repeat(10)] }, { uspp: ['k', 'x'.repeat(10) + '\ny'] },
    { uspp: ['long', 'z'.repeat(10010)] }, { uspp: ['\ud800', '\udc00\ud83d'] });
  const leaves = [...values];
  // Nested at depths 1 to 4, in objects and in arrays.
  const nests = [];
  for (const v of [{ url: 'http://a.test/p?x=1' }, { usp: 'a=1' }, { usp: '' }, { uspp: ['a', 'v'.repeat(35), 'b', 'v'.repeat(35)] },
    { parse: 'http://p.test/' }, { parse: 'nope' }, { url: "sc:a'b\"c" }, { uspp: ['k', 'v'.repeat(72)] }]) {
    let o = v, a = v;
    for (let d = 1; d <= 4; d++) {
      o = { o: [[`l${d}`, o]] };
      a = { a: [a] };
      nests.push(o, a);
    }
    nests.push({ o: [['u', v], ['n', { n: 1 }], ['s', { s: 'str' }]] }, { a: [v, { n: 2 }, v] },
      { o: [['nested', { o: [['p', v]] }]] }, { o: [['k'.repeat(70), v]] });
  }
  nests.push({ a: Array.from({ length: 7 }, () => ({ usp: '' })) }, { a: Array.from({ length: 12 }, (_, i) => ({ usp: `a=${i}` })) },
    { a: Array.from({ length: 26 }, (_, i) => ({ usp: i % 3 ? '' : 'q=1' })) }, { a: [{ url: 'http://a/' }, { url: 'http://b/' }, { url: 'http://c/' }] },
    { a: Array.from({ length: 8 }, () => ({ url: 'http://a/' })) }, { o: [['u', { url: 'http://a/' }], ['p', { usp: 'a=1' }]] });
  // And random trees of them, in objects and arrays up to four deep.
  const pool = [{ url: 'http://a.test/p?x=1' }, { url: "sc:a'b" }, { url: 'https://u:p@h:8/' + 'z'.repeat(60) }, { usp: 'a=1&b=2' },
    { usp: '' }, { uspp: ['k', 'v'.repeat(40)] }, { uspp: ['k', 'v'.repeat(72)] }, { parse: 'nope' }, { parse: 'http://p/' },
    { s: 'str' }, { n: 1 }, { s: 'x'.repeat(50) }];
  const tree = (depth) => {
    if (depth >= 4 || rnd() < 0.35) return pick(pool);
    const n = 1 + int(rnd() < 0.2 ? 9 : 3);
    if (rnd() < 0.5) return { a: Array.from({ length: n }, () => tree(depth + 1)) };
    return { o: Array.from({ length: n }, (_, i) => [`k${i}`, tree(depth + 1)]) };
  };
  for (let i = 0; i < 300; i++) nests.push(tree(0));
  begin('INSPECT');
  for (const v of [...leaves, ...nests]) emit({ k: 'insp', v, out: X(util.inspect(build(v))) });
  begin('JSON');
  for (const v of [...leaves.filter((d) => !('uspp' in d && d.uspp.some((x) => x.length > 9000))), ...nests]) {
    const value = build(v);
    emit({ k: 'json', v, out: X(JSON.stringify(value)), out2: X(JSON.stringify(value, null, 2)) });
  }
}

// ---------------------------------------------------------------------------

sectionMisc();
sectionComponents();
sectionHosts();
sectionPaths();
sectionRelative();
sectionSetters();
sectionParams();
sectionForm();
sectionLive();
sectionPrint();
sectionIDNA();
begin('INCONSISTENT');
for (const line of inconsistent) emit(line);

fs.writeFileSync(OUT, lines.join('\n') + '\n');
fs.writeFileSync(INDEX, JSON.stringify({ node: process.version, unicode: process.versions.unicode, ada: process.versions.ada, seed: SEED, sections: index }, null, 2) + '\n');
console.log(`wrote ${OUT}: ${lines.length} lines, ${fs.statSync(OUT).size} bytes`);
for (const [s, { first, last, count, errors }] of Object.entries(index)) {
  console.log(`  ${s}: ${count} (lines ${first}-${last}), ${errors} errors`);
}
