// gen-json-goldens.mjs: a golden corpus of JSON.stringify output, recorded
// from Node 24.18.0, for cakebear's JSONStringify.
//
//   node gen-json-goldens.mjs
//
// writes json-goldens.jsonl and json-goldens-index.json beside this file,
// then re-reads the corpus and checks every line. No dependencies, and
// deterministic: the spaces of the INSPECT section are picked by a seeded
// mulberry32, so a rerun writes a byte-identical file.
//
// Each line is {"value":<value>,"space":<value>,"out":"<exact output>"},
// where out is JSON.stringify(value, null, space). space is absent when the
// call gives none, and otherwise a number or a string, which the Go test
// turns into a gap with JSONGapNumber or JSONGapString, as compiled code
// does. Values are encoded as in gen-inspect-goldens.mjs, with one form
// more, for a string holding a lone surrogate (a string literal can write
// "\uD800"): {"u":[<UTF-16 code unit>...]}. A line has, in place of out,
//   "outU":[<code unit>...]   when the output holds a lone surrogate, which
//                             only a gap cut through a pair puts there;
//   "throws":"<String(err)>"  when JSON.stringify threw: a cycle.
// A line of ERROR or UNKNOWN (M6) may also say "any":true, when its value
// is an unknown: those sections hold Errors and unknowns (golden-codec.mjs).
//
// The subset's constraints are checked on every entry, as for the inspect
// corpus (golden-codec.mjs, validate with forJSON).
//
// The INSPECT section is the values of the inspect corpus, each argument
// of an entry on its own, with a space picked at random: every value of its
// deliberate sections, and of a third of its random one, which keeps this
// file to a few MB. It reads inspect-goldens.jsonl and its index, so
// regenerate those first if they change.
//
// The index file maps each section to its 1-based line range. ERROR and
// UNKNOWN come last, so the lines before them stay as they were.

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { codeUnits, decodeArgs, encodeArgs, numText, validate, stripAny, ERROR_KINDS } from './golden-codec.mjs';

if (process.version !== 'v24.18.0') {
  throw new Error(`recorded against Node v24.18.0, this is ${process.version}`);
}

const HERE = path.dirname(fileURLToPath(import.meta.url));
const OUT = path.join(HERE, 'json-goldens.jsonl');
const INDEX = path.join(HERE, 'json-goldens-index.json');
const INSPECT = path.join(HERE, 'inspect-goldens.jsonl');
const INSPECT_INDEX = path.join(HERE, 'inspect-goldens-index.json');
const RANDOM_EVERY = 3;
const SEED = 0x2026_1015;

// ---------------------------------------------------------------------------
// Descriptions

const N = (x) => ({ n: numText(x) });
const S = (s) => (s.isWellFormed() ? { s } : { u: codeUnits(s) });
const B = (b) => ({ b });
const A = (items, id) => (id === undefined ? { a: items } : { a: items, id });
const O = (pairs, id) => (id === undefined ? { o: pairs } : { o: pairs, id });
const R = (id) => ({ ref: id });
const E = (x) => ({ e: x === null ? 'null' : 'undefined' });

// v describes an acyclic plain JS value.
function v(x) {
  switch (typeof x) {
    case 'number': return N(x);
    case 'string': return S(x);
    case 'boolean': return B(x);
    case 'undefined': return E(undefined);
    case 'object':
      if (x === null) return E(null);
      if (Array.isArray(x)) return A(x.map(v));
      return O(Object.keys(x).map((k) => [k, v(x[k])]));
    default:
      throw new Error(`cannot describe ${typeof x}`);
  }
}

// record is what Node makes of one entry: its output, or what it threw.
function record(value, space) {
  validate([value], { forJSON: true });
  if (space !== undefined) {
    const keys = Object.keys(space);
    if (keys.length !== 1 || !['n', 's', 'u'].includes(keys[0])) throw new Error('bad space');
  }
  const [x] = decodeArgs([value]);
  const again = JSON.stringify(encodeArgs([x]));
  if (again !== JSON.stringify(stripAny([value]))) throw new Error(`not canonical:\n${JSON.stringify([value])}\n${again}`);
  const sp = space === undefined ? undefined : decodeArgs([space])[0];
  let out;
  try {
    out = JSON.stringify(x, null, sp);
  } catch (err) {
    if (!(err instanceof TypeError) || !err.message.startsWith('Converting circular structure to JSON\n')) throw err;
    return { throws: String(err) };
  }
  if (typeof out !== 'string') throw new Error('JSON.stringify gave no string');
  return out.isWellFormed() ? { out } : { outU: codeUnits(out) };
}

// ---------------------------------------------------------------------------
// The corpus being built

const entries = []; // { section, value, space, any }
const addD = (section, value, space) => entries.push({ section, value, space });
// addU adds an entry whose value is an unknown.
const addU = (section, value, space) => entries.push({ section, value, space, any: true });
const add = (section, x, space) => addD(section, v(x), space);

// Spaces: none, numbers that clamp, truncate or give nothing, and strings
// that are cut at 10 code units, one of them through a surrogate pair. V8
// adds two quirks: a number from 0 to 1 breaks lines without indenting, and
// a string's gap ends at a NUL.
const SPACES = [
  undefined, N(0), N(1), N(2), N(3), N(4), N(8), N(10), N(11), N(100),
  N(1.5), N(9.99), N(0.5), N(0.999), N(0.9999), N(1e-9), N(-1), N(-0), N(-0.5),
  N(-11), N(NaN), N(Infinity),
  N(-Infinity), S(''), S(' '), S('\t'), S('--'), S('\n'), S('\r\n'), S('\x00'),
  S('abcdefghijklmn'), S('0123456789'), S('0123456789x'), S('123456789😀'),
  S('12345678😀'), S('1234567890😀'), S('😀'.repeat(6)), S('日本語'),
  S('日本語'.repeat(4)), S('a😀b'), S('"\\'), S('\ud800'), S('123456789\ud800x'),
  S('\udc00'.repeat(11)), S('a\x00b'), S('\x00ab'), S('0123456789\x00'),
  S('日\x00本'), N(1e-300),
];

// ---------------------------------------------------------------------------
// STR: escapes, in values and keys.

const LONE = [
  '\ud800', '\udbff', '\udc00', '\udfff', 'a\ud800', '\ud800a', '\udc00\ud800',
  '\ud83d\ud83d\ude00', '\ude00\ude00', 'x\ud83dy\ude00z', '\ud800'.repeat(5),
  '😀\ud800😀', '\ud83d', '\ude00', 'end\udbff', '\udbff\udfff\udbff',
];

function sectionSTR() {
  const r = 'STR';
  const controls = [];
  for (let c = 0x00; c <= 0x1f; c++) controls.push(String.fromCharCode(c));
  for (const ch of controls) {
    add(r, ch);
    add(r, 'a' + ch + 'b');
    add(r, [ch, ch + ch]);
    add(r, { ['k' + ch]: ch });
  }
  const all = controls.join('');
  const samples = [
    all, all + all, '"', '\\', '/', "'", '`', '"\\"', '\\"', '\\\\', 'say "hi"',
    'C:\\path\\to\\file', '\\u0041', '\x7f', '\x80', '\x85', '\x9f', '\u00a0',
    '\u00ad', '\u2028', '\u2029', '\u2028\u2029', 'a\u2028b', '\ufeff', '\ufffd',
    '\u200b\u200c\u200d', '😀', '👨‍👩‍👧', '\u{103ff}', '\u{10000}', '\u{10ffff}',
    'café', 'e\u0301', '日本語', '<script>alert(1)</script>', '&amp;', '${x}',
    '%s %d', '', ' ', 'tab\there', 'line\r\nbreak', '\b\f\n\r\t', '\v',
    ...LONE,
  ];
  for (const s of samples) {
    addD(r, S(s));
    addD(r, A([S(s)]));
    addD(r, A([S(s), S('x' + s + 'y')]));
    addD(r, O([['v', S(s)]]), N(2));
    if (s.isWellFormed() && s !== '') {
      addD(r, O([[s, S(s)]]));
      addD(r, O([[s, N(1)], ['b', A([S(s)])]]), S('\t'));
    }
  }
  addD(r, A(LONE.map(S)));
  addD(r, A(LONE.map(S)), N(2));
  addD(r, O(LONE.map((s, i) => ['k' + i, S(s)])), N(4));
  // Long strings: the fast path for an unescaped run, and runs of escapes.
  for (const n of [1, 16, 100, 1000]) {
    add(r, 'x'.repeat(n));
    add(r, '\n'.repeat(n));
    add(r, '"'.repeat(n));
    add(r, 'ab\x01'.repeat(n));
    add(r, '😀'.repeat(n));
    addD(r, S('\ud800x'.repeat(n)));
  }
  add(r, 'x'.repeat(10000));
  add(r, ['\\"\n'.repeat(3000)]);
}

// ---------------------------------------------------------------------------
// NUM: numbers, which -0 and the non-finite ones are the reason for.

const NUMBERS = [
  0, -0, 1, -1, 42, -42, 0.5, -0.5, 0.1 + 0.2, 1 / 3, 2 / 3, 1e21, 1e-7, 1e-6,
  123456789012345680000, Number.MAX_SAFE_INTEGER, Number.MIN_SAFE_INTEGER,
  2 ** 53, 2 ** 53 + 2, Number.MAX_VALUE, Number.MIN_VALUE, -Number.MIN_VALUE,
  NaN, Infinity, -Infinity, 1e20, 1e100, 1.5e-10, 123.456, 100, 1e6, 0.000001,
  0.0000001, 2 ** 31, 2 ** 32, 4294967295, -2147483648, 9007199254740993,
  0.1, 255, 3.14159, 1e21 - 65536, 999999999999999900000, 1.7976931348623157e308,
  -1e-7, 1.0000000000000002, 4.35, 0.07, 2 ** -1074, 5e-324, 1.2e-322,
  12345678901234567890, 0.30000000000000004, 1e15, 1e16, 1e17, 123e-20,
  -123456.789, -1e21, -5e-324, 1e-300, -1e300,
];

function sectionNUM() {
  const r = 'NUM';
  for (const x of NUMBERS) {
    add(r, x);
    add(r, [x]);
    add(r, { n: x });
    add(r, [x, x], N(2));
  }
  add(r, NUMBERS);
  add(r, NUMBERS, N(2));
  add(r, { nums: NUMBERS }, S('\t'));
  add(r, { zero: 0, negZero: -0, nan: NaN, inf: Infinity, ninf: -Infinity });
  add(r, [[-0, NaN], [Infinity, -Infinity]], N(1));
  add(r, true);
  add(r, false);
  add(r, [true, false, true]);
  add(r, { t: true, f: false }, N(2));
}

// ---------------------------------------------------------------------------
// KEYS and ORDER: keys quoted as strings are, in each object's own order.

const KEYS = [
  'a', 'A', '_', '_y', 'abc123', 'snake_case', 'camelCase', 'UPPER', 'if',
  'class', 'constructor', 'toString', 'hasOwnProperty', 'valueOf', 'prototype',
  'null', 'undefined', 'true', 'NaN', 'Infinity', 'await', 'let', '$x', '$', 'a$',
  'x y', ' ', '', 'a-b', "it's", 'say "hi"', 'back\\slash', 'new\nline',
  'tab\tkey', 'café', 'naïve', 'Ünïcödé', '日本', '😀', 'key😀', 'a.b', 'a:b',
  '1a', '01', '-1', '1.5', '1e3', '4294967295', '9007199254740993', '+1', ' 1',
  '1 ', 'é', 'e\u0301', '\u00a0', '\u200b', 'ß', 'ﬁ', '\x7f', '\x00', '%s',
  '${a}', '`', '\'"`', 'a'.repeat(100), 'Symbol(x)', '[a]', '__proto',
  '__proto__x', 'x__proto__', '_proto_', '__', '0x10', 'a\u0301', 'ab-',
  '-', '@', '#hash', 'a/b', 'π', 'Ω', 'z9_', '_9', '9_', '\u2028', '"', '\\',
  'content-type', 'x-request-id',
];

function permutations(xs) {
  if (xs.length <= 1) return [xs];
  const out = [];
  xs.forEach((x, i) => {
    for (const p of permutations([...xs.slice(0, i), ...xs.slice(i + 1)])) out.push([x, ...p]);
  });
  return out;
}

function sectionKEYS() {
  const r = 'KEYS';
  for (const k of KEYS) {
    addD(r, O([[k, N(1)]]));
    addD(r, O([[k, S('v')], ['b', N(2)]]), N(2));
    addD(r, O([['inner', O([[k, B(true)]])]]), S('--'));
  }
  addD(r, O(KEYS.map((k, i) => [k, N(i)])));
  addD(r, O(KEYS.map((k, i) => [k, N(i)])), N(2));

  const o = 'ORDER';
  const vals = { a: N(1), b: S('two'), c: B(true), d: A([N(4)]) };
  for (const p of permutations(['a', 'b', 'c', 'd'])) {
    addD(o, O(p.map((k) => [k, vals[k]])));
    addD(o, O([['x', O(p.map((k) => [k, vals[k]]))]]), N(2));
  }
  const five = ['id', 'name', 'email', 'active', 'score'];
  const fv = { id: N(7), name: S('Ann'), email: S('ann@example.com'), active: B(false), score: N(9.5) };
  const orders = [five, [...five].reverse(), ['email', 'id', 'score', 'name', 'active'], ['active', 'score', 'id', 'email', 'name']];
  const users = A(orders.map((p) => O(p.map((k) => [k, fv[k]]))));
  addD(o, users);
  addD(o, users, N(2));
  addD(o, O([['users', users], ['total', N(4)]]), N(2));
}

// ---------------------------------------------------------------------------
// NEST: empty containers, nesting and depth.

function nestObj(levels, leaf) {
  let x = leaf;
  for (let i = levels - 1; i >= 0; i--) x = { ['l' + i]: x };
  return x;
}
function nestArr(levels, leaf) {
  let x = leaf;
  for (let i = 0; i < levels; i++) x = [x];
  return x;
}

function sectionNEST() {
  const r = 'NEST';
  const values = [
    {}, [], { a: {} }, { a: [] }, [[]], [{}], [[], []], [{}, {}], { a: { b: {} } },
    { a: {}, b: [], c: '' }, [[[]]], [{ a: [] }, { a: [{}] }], { a: [[], [[]]] },
    { e: Array.from({ length: 8 }, () => []) }, Array.from({ length: 30 }, () => ({})),
    [[1, 2], [3, 4], []], { a: { b: { c: { d: { e: 1 } } } } },
  ];
  for (const x of values) {
    for (const sp of [undefined, N(2), S('\t'), N(10)]) add(r, x, sp);
  }
  for (const levels of [1, 2, 3, 5, 10, 20, 50]) {
    for (const sp of [undefined, N(1), S('ab')]) {
      add(r, nestObj(levels, 1), sp);
      add(r, nestArr(levels, 'leaf'), sp);
      add(r, nestObj(levels, []), sp);
      add(r, nestArr(levels, {}), sp);
      add(r, nestObj(levels, [nestObj(2, true)]), sp);
    }
  }
  for (const levels of [100, 200, 500]) {
    add(r, nestObj(levels, 1));
    add(r, nestArr(levels, 1));
  }
}

// ---------------------------------------------------------------------------
// SPACE: one set of values under every space.

function sectionSPACE() {
  const r = 'SPACE';
  const values = [
    { a: [1, 2], b: [{ c: 3 }], d: 'x' },
    { users: [{ id: 1, name: 'Ann', tags: ['a', 'b'] }, { id: 2, name: 'Bob', tags: [] }], total: 2 },
    [[[1], [2, 3]], [[]]],
    { empty: {}, none: [], deep: { deeper: { deepest: [{}] } } },
    ['a', 'b\nc'],
    {},
    [],
    1,
    'top\nlevel',
    true,
  ];
  for (const sp of SPACES) {
    for (const x of values) add(r, x, sp);
    addD(r, O([['s', S('\ud800')], ['t', A([S('\ude00x')])]]), sp);
  }
}

// ---------------------------------------------------------------------------
// SHARED and CIRC: a value reached twice is written twice; a cycle throws.

// renumberIds copies a description and renumbers its ids 1.. in pre-order,
// as in gen-inspect-goldens.mjs.
function renumberIds(desc) {
  const copy = JSON.parse(JSON.stringify(desc));
  const latest = new Map();
  let next = 1;
  const walk = (d) => {
    if ('ref' in d) {
      if (!latest.has(d.ref)) throw new Error(`ref ${d.ref} before its container`);
      d.ref = latest.get(d.ref);
      return;
    }
    if ('id' in d) { latest.set(d.id, next); d.id = next++; }
    if ('a' in d) d.a.forEach(walk);
    if ('o' in d) d.o.forEach((p) => walk(p[1]));
  };
  walk(copy);
  return copy;
}

// chain is a path of n steps from a container back to it: an object or an
// array at each step, as kinds says, the last closing the cycle. An object
// steps on by its key k<i>, or closeKey for the last, and an array by its
// only element.
function chain(kinds, closeKey) {
  const make = (i) => {
    const kind = kinds[i];
    const inner = i + 1 < kinds.length ? make(i + 1) : R(1);
    const key = i + 1 < kinds.length ? 'k' + i : closeKey;
    const node = kind === 'o' ? O([['pad', N(i)], [key, inner]]) : A([inner]);
    if (i === 0) node.id = 1;
    return node;
  };
  return make(0);
}

function sectionCIRC() {
  const s = 'SHARED';
  const d = (section, desc, space) => addD(section, renumberIds(desc), space);
  for (const sp of [undefined, N(2)]) {
    d(s, O([['a', O([['x', N(1)]], 1)], ['b', R(1)]]), sp);
    d(s, A([O([['x', N(1)]], 1), R(1), R(1)]), sp);
    d(s, A([A([N(1), N(2)], 1), A([R(1), R(1)])]), sp);
    d(s, O([['m', A([S('a')], 1)], ['n', R(1)], ['o', O([['p', R(1)]])]]), sp);
    d(s, A([O([['inner', A([O([['leaf', B(true)]], 1)])]]), O([['inner', A([R(1), R(1)])]])]), sp);
    d(s, O([['x', O([['y', A([], 1)]], 2)], ['again', R(2)], ['empty', R(1)]]), sp);
  }

  const c = 'CIRC';
  // Every chain of objects and arrays up to five steps long, closed by a
  // property or an index: the message lists two steps, then `...`, then one.
  const kindsOf = (n) => (n === 0 ? [[]] : kindsOf(n - 1).flatMap((k) => [[...k, 'o'], [...k, 'a']]));
  for (let n = 1; n <= 5; n++) {
    for (const kinds of kindsOf(n)) d(c, chain(kinds, 'back'));
  }
  for (const n of [6, 7, 10, 20]) {
    d(c, chain(Array(n).fill('o'), 'back'));
    d(c, chain(Array.from({ length: n }, (_, i) => (i % 2 ? 'a' : 'o')), 'back'));
  }
  // The keys a message names, as they are.
  for (const k of ['self', '', "it's", 'say "hi"', 'new\nline', 'tab\tkey', '日本😀', 'back\\slash', '\u2028', 'x'.repeat(100)]) {
    d(c, O([[k, R(1)]], 1));
    d(c, O([['a', O([[k, R(1)]])]], 1));
    d(c, O([[k, A([R(1)])]], 1));
  }
  // Starting below the top, after output has begun, at a later index.
  d(c, O([['first', N(1)], ['inner', O([['pad', S('p')], ['me', R(1)]], 1)]]));
  d(c, A([A([]), A([]), A([A([]), R(1)], 1)]));
  d(c, A([A([A([]), A([]), A([]), A([]), A([]), R(1)], 1)]));
  d(c, O([['list', A(Array.from({ length: 12 }, (_, i) => (i === 11 ? R(1) : A([]))), 1)]]));
  d(c, O([['wrap', A([O([['pad', N(1)], ['start', O([['k0', A([O([['back', R(1)]])])]], 1)]])])]]));
  // The first cycle met is the one named.
  d(c, O([['a', O([['self', R(1)]], 1)], ['b', O([['self', R(2)]], 2)]]));
  d(c, O([['a', O([['self', R(2)]], 2)], ['top', R(1)]], 1));
  // A shared value before the cycle, and the cycle's own container shared.
  d(c, O([['s', O([['v', N(1)]], 2)], ['t', R(2)], ['me', R(1)]], 1));
  d(c, A([O([['self', R(1)]], 1), R(1)]));
  // A cycle throws whatever the space.
  for (const sp of [N(2), S('\t'), S('123456789😀')]) {
    d(c, O([['name', S('root')], ['items', A([R(1)])]], 1), sp);
    d(c, A([R(1)], 1), sp);
  }
}

// ---------------------------------------------------------------------------
// INSPECT: the inspect corpus's values, with a space picked at random.

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

function sectionINSPECT() {
  const r = 'INSPECT';
  const rnd = mulberry32(SEED);
  const pick = (xs) => xs[Math.floor(rnd() * xs.length)];
  const space = () => {
    const x = rnd();
    if (x < 0.4) return undefined;
    if (x < 0.65) return N(2);
    if (x < 0.75) return N(4);
    if (x < 0.85) return S('\t');
    return pick(SPACES);
  };
  const seen = new Set();
  const lines = fs.readFileSync(INSPECT, 'utf8').split('\n');
  if (lines.pop() !== '') throw new Error('the inspect corpus has no trailing newline');
  const random = JSON.parse(fs.readFileSync(INSPECT_INDEX, 'utf8')).sections.RANDOM;
  if (random.last > lines.length) throw new Error('the inspect index is not of this corpus');
  for (const [i, text] of lines.entries()) {
    const line = i + 1;
    // The sections after RANDOM (MAYBE) are this corpus's own MAYBE's
    // business, and hold top-level undefined, which has no JSON.
    if (line > random.last) break;
    if (line >= random.first && (line - random.first) % RANDOM_EVERY !== 0) continue;
    // An argument may be a ref to an earlier one, so the entry is rebuilt
    // whole, and each value described again on its own.
    for (const x of decodeArgs(JSON.parse(text).args)) {
      const [desc] = encodeArgs([x]);
      const key = JSON.stringify(desc);
      if (seen.has(key)) continue;
      seen.add(key);
      addD(r, desc, space());
    }
  }
}

// ---------------------------------------------------------------------------
// MAYBE: null, and undefined, which an object leaves out and an array writes
// as null (M5 plan D8), under the gaps that change the layout. An object
// whose every property is undefined is "{}", gap or not. Top-level
// undefined is left out: JSON.stringify gives undefined, not a string, and
// lowering refuses it.

function sectionMAYBE() {
  const r = 'MAYBE';
  const spaces = [undefined, N(2), S('\t'), N(0.5), S('--')];
  for (const x of [
    null, [undefined], [null], [undefined, null], [1, undefined, 3], ['a', null], { a: undefined }, { a: null },
    { a: undefined, b: 1 }, { a: 1, b: undefined }, { a: 1, b: undefined, c: null, d: 'x' }, { a: undefined, b: undefined },
    { x: { y: undefined } }, { x: { y: undefined }, z: [undefined] }, [{ a: undefined }], [{ a: 1 }, { a: undefined }, {}],
    { list: [undefined, null, 1], o: { p: null, q: undefined } }, [[undefined], [null, undefined]], { k: [] },
  ]) {
    for (const space of spaces) addD(r, v(x), space);
  }
}

// ---------------------------------------------------------------------------
// ERROR: an Error is its own enumerable properties (R27): {} for every one
// the program makes, and {"code":…} for Node's own, wherever it is, under
// the gaps that change the layout. UNKNOWN: unknowns of every kind, an
// unknown[] mixing them, and undefined left out of an object and null in an
// array, as for a maybe; and a cycle through unknowns, which throws.
// Top-level undefined is left out: JSON.stringify gives undefined, not a
// string, and lowering refuses a bare unknown as it refuses a bare maybe.

const ERR = (kind, m, code) => ({ err: kind, ...(m === undefined ? {} : { m }), ...(code === undefined ? {} : { code }) });
const UA = (items, id) => ({ ...A(items, id), any: true });
const UO = (pairs, id) => ({ ...O(pairs, id), any: true });
const M6_SPACES = [undefined, N(2), S('\t'), N(0.5), S('--')];
const CODED = [
  ERR('Error', 'Cannot set headers after they are sent to the client', 'ERR_HTTP_HEADERS_SENT'),
  ERR('TypeError', 'Header name must be a valid HTTP token ["bad name"]', 'ERR_INVALID_HTTP_TOKEN'),
  ERR('TypeError', 'Invalid character in header content ["x-a"]', 'ERR_INVALID_CHAR'),
  ERR('RangeError', 'Invalid status code: 1000', 'ERR_HTTP_INVALID_STATUS_CODE'),
  ERR('RangeError', 'options.port should be >= 0 and < 65536. Received type number (70000).', 'ERR_SOCKET_BAD_PORT'),
];

function sectionERROR() {
  const r = 'ERROR';
  const values = [
    ...ERROR_KINDS.map((k) => ERR(k, 'm')), ERR('Error'), ERR('Error', ''), ERR('TypeError', 'say "hi"\n'), ...CODED,
    O([['e', ERR('Error', 'x')]]), A([ERR('Error', 'x')]), A([ERR('Error', 'a'), ERR('RangeError', 'b'), ERR('Error')]),
    O([['data', E(null)], ['error', ERR('Error', 'failed')]]), O([['a', N(1)], ['e', CODED[0]], ['b', S('x')]]),
    A([CODED[1], CODED[3]]), A([ERR('Error', 'x'), E(null), E(undefined)]), O([['nested', O([['deeper', A([CODED[4]])]])]]),
  ];
  for (const x of values) {
    for (const space of M6_SPACES) addD(r, x, space);
  }
}

function sectionUNKNOWN() {
  const r = 'UNKNOWN';
  const each = [
    E(undefined), E(null), N(0), N(-0), N(1.5), N(NaN), N(Infinity), S(''), S('a"b'), B(true), ERR('Error', 'm'), CODED[0],
    A([]), A([N(1), N(2)]), O([]), O([['a', N(1)]]), UA([N(1), S('a'), E(null), E(undefined), B(false)]),
    UO([['u', E(undefined)], ['n', E(null)], ['s', S('x')]]),
  ];
  for (const x of each) {
    if (x.e === 'undefined') continue;
    for (const space of M6_SPACES) addU(r, x, space);
  }
  for (const space of M6_SPACES) {
    addU(r, UA([E(undefined)]), space);
    addU(r, UA([E(undefined), N(1), E(undefined)]), space);
    addU(r, UA(each), space);
    addU(r, UO(each.map((x, i) => [`k${i}`, x])), space);
    addU(r, UO([['u', E(undefined)]]), space);
    addU(r, UA([UA([E(undefined)]), UO([['e', ERR('RangeError', 'r')], ['u', E(undefined)]])]), space);
  }
  addU(r, UA([N(1), R(1)], 1));
  addU(r, UO([['self', R(1)]], 1));
  addU(r, UA([UO([['up', R(1)]])], 1), N(2));
}

// ---------------------------------------------------------------------------

sectionSTR();
sectionNUM();
sectionKEYS();
sectionNEST();
sectionSPACE();
sectionCIRC();
const deliberate = entries.length;
sectionINSPECT();
sectionMAYBE();
sectionERROR();
sectionUNKNOWN();

const lines = [];
const index = {};
entries.forEach(({ section, value, space, any }, i) => {
  let result;
  try {
    result = record(value, space);
  } catch (err) {
    err.message = `${section} entry ${i}: ${err.message}`;
    throw err;
  }
  lines.push(JSON.stringify({ value, space, ...(any ? { any } : {}), ...result }));
  const line = i + 1;
  if (!index[section]) index[section] = { first: line, last: line, count: 0, throws: 0 };
  if (index[section].last < line - 1) throw new Error(`section ${section} is not contiguous`);
  index[section].last = line;
  index[section].count++;
  if (result.throws) index[section].throws++;
});
fs.writeFileSync(OUT, lines.join('\n') + '\n');
fs.writeFileSync(INDEX, JSON.stringify({ node: process.version, seed: SEED, sections: index }, null, 2) + '\n');

// Self-check: re-read the file, rebuild every value, stringify again.
const reread = fs.readFileSync(OUT, 'utf8').split('\n');
if (reread.pop() !== '') throw new Error('no trailing newline');
let mismatches = 0;
for (const [i, text] of reread.entries()) {
  const { value, space, any, ...result } = JSON.parse(text);
  if (JSON.stringify(record(value, space)) !== JSON.stringify(result)) {
    mismatches++;
    if (mismatches <= 5) console.error(`line ${i + 1}: output differs`);
  }
}

console.log(`wrote ${OUT}: ${reread.length} lines, ${fs.statSync(OUT).size} bytes`);
console.log(`deliberate ${deliberate}, from the inspect corpus ${reread.length - deliberate}`);
for (const [s, { first, last, count, throws }] of Object.entries(index)) {
  console.log(`  ${s}: ${count} (lines ${first}-${last}), ${throws} throw`);
}
console.log(`self-check: ${reread.length} lines re-read, ${mismatches} mismatches`);
if (mismatches) process.exit(1);
