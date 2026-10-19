// gen-inspect-goldens.mjs: a golden corpus of util.format output, recorded
// from Node 24.18.0, for cakebear's port of util.inspect.
//
//   node gen-inspect-goldens.mjs
//
// writes inspect-goldens.jsonl and inspect-goldens-index.json beside this
// file, then re-reads the corpus and checks every line. No dependencies, and
// deterministic: the random section runs on a seeded mulberry32, so a rerun
// writes a byte-identical file.
//
// Each line is {"args":[<value>...],"out":"<exact output>"}, where out is
// util.format(...args) with no trailing newline (console.log on a non-TTY
// stdout prints exactly out + "\n"). The value encoding:
//   number     {"n":"<text>"}, text = Object.is(x,-0) ? "-0" : String(x)
//   string     {"s":"..."}, never a lone surrogate
//   boolean    {"b":true}
//   array      {"a":[<value>...]}, plus an optional "id":<int>
//   object     {"o":[["key",<value>],...]}, keys in creation order, plus an
//              optional "id":<int>
//   undefined  {"e":"undefined"}, and null {"e":"null"} (M5)
//   reference  {"ref":<int>}, naming a container with that id earlier in a
//              depth-first pre-order walk of the args (an ancestor: a cycle;
//              anything else: a shared reference)
// Ids appear only on containers that are referenced, numbered 1.. in
// pre-order, so a description survives decode-then-encode byte for byte.
//
// Every entry is built from its description: generate the description,
// decode it into JS values, then format those. The subset's constraints are
// checked on every entry: arrays homogeneous (one kind; arrays of objects
// share one key set), no integer-like or __proto__ keys, no lone
// surrogates, and a first argument that is a string has no '%'.
//
// The index file maps each section (R1..R13, CIRC, RANDOM, MAYBE) to its
// 1-based line range in the corpus. MAYBE, null and undefined (M5), comes
// after RANDOM, so that the lines before it, and RANDOM's seeded draws, stay
// as they were.

import util from 'node:util';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { checkEntry as check, decodeArgs, numText } from './golden-codec.mjs';

if (process.version !== 'v24.18.0') {
  throw new Error(`recorded against Node v24.18.0, this is ${process.version}`);
}

const HERE = path.dirname(fileURLToPath(import.meta.url));
const OUT = path.join(HERE, 'inspect-goldens.jsonl');
const INDEX = path.join(HERE, 'inspect-goldens-index.json');
const TOTAL = 20000;
const SEED = 0x2026_1014;
const LINE_BUDGET = 480;

// ---------------------------------------------------------------------------
// Descriptions

const N = (x) => ({ n: numText(x) });
const S = (s) => ({ s });
const B = (b) => ({ b });
const A = (items, id) => (id === undefined ? { a: items } : { a: items, id });
const O = (pairs, id) => (id === undefined ? { o: pairs } : { o: pairs, id });
const R = (id) => ({ ref: id });
const E = (x) => ({ e: x === null ? 'null' : 'undefined' });

// v describes an acyclic plain JS value. Its objects' keys must not be
// integer-like, or Object.keys would reorder them.
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

// renumberIds copies a description and renumbers its ids 1.. in pre-order.
// A ref goes to the latest container defined with its old id, so a subtree
// used twice while building, self-references included, comes out with ids
// of its own in each place.
function renumberIds(args) {
  const copy = JSON.parse(JSON.stringify(args));
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
  copy.forEach(walk);
  return copy;
}

const fmt = (args) => util.format(...decodeArgs(args));

// ---------------------------------------------------------------------------
// The corpus being built

const entries = []; // { section, args }
const addD = (section, ...args) => entries.push({ section, args });
const add = (section, ...values) => addD(section, ...values.map(v));
const notes = []; // threshold findings, printed at the end

// ---------------------------------------------------------------------------
// R1: the top level. Strings print raw, several args join with a space.

function sectionR1() {
  const r = 'R1';
  add(r, 'hello');
  add(r, '');
  add(r, 'a', 'b');
  add(r, 'a', 'b', 'c', 'd');
  add(r, '', '');
  add(r, ' ', ' ');
  add(r, 'a', '');
  add(r, '\n');
  add(r, '\n', '\n');
  add(r, 'x', 1);
  add(r, 1, 'x');
  add(r, 1, 2);
  add(r, 1, 2, 3, 4);
  add(r, true);
  add(r, false);
  add(r, true, false);
  add(r, 'flag', true);
  add(r, 1, 'a', true, [1]);
  add(r, 'count:', 5);
  add(r, 'obj:', { a: 1 });
  add(r, { a: 1 }, 'after');
  add(r, [1, 2], 'mid', { b: 'x' });
  add(r, [1], [2], [3], [4]);
  add(r, { a: 1 }, { b: 2 });
  add(r, "it's \"quoted\" `tick` ${x}");
  add(r, 'tab\there', 'nl\nhere');
  add(r, 'back\\slash');
  add(r, '\x00\x01\x07\x1b[31mred\x1b[0m\x7f\x85\x9f');
  add(r, 'café 日本 😀 e\u0301 \u00a0 \u200b \u2028 \ufeff');
  add(r, 1, '%s %d %% %o %j');
  add(r, 'a', '100%');
  add(r, [1], '%o', 'x');
  add(r, { pct: '%s' }, '%d');
  add(r, 'line1\nline2', { k: 'v' });
  add(r, -0);
  add(r, 0);
  add(r, NaN);
  add(r, Infinity);
  add(r, -Infinity);
  add(r, 1e21);
  add(r, 0.1);
  add(r, -0, 0);
  add(r, 'neg', -0);
  add(r, 1.5, 'and', -2.25);
  add(r, []);
  add(r, {});
  add(r, [], {});
  add(r, '', []);
  add(r, {}, '');
  add(r, 'x'.repeat(200));
  add(r, 'a\n'.repeat(50));
  add(r, 'q', 'x'.repeat(120) + '\n' + 'y'.repeat(30));
  add(r, { a: 1 }, "it's");
  add(r, [1], 'say "hi"', 'back`tick');
  add(r, { a: "it's" }, "it's");
  add(r, ['x'], 'x');
  add(r, 'multi', { a: 'x'.repeat(70) }, 'arg');
  add(r, [1, 2, 3, 4, 5, 6, 7], 'grouped then string');
  add(r, '😀', '日本', 'e\u0301');
  add(r, 'trailing space ', ' leading');
  add(r, 'x'.repeat(10), 1, 'y'.repeat(10), 2);
  add(r, ['a'], 'b', ['c'], 'd');
  add(r, 'end with newline\n', 1);
  add(r, 'tabs\t\t', [1, 2]);
  add(r, { s: 'nested' }, 'raw', ['nested']);
  add(r, true, 'x', false, 'y');
  add(r, 'é', 'e\u0301');
  add(r, '\u1100\u1161', '가');
}

// ---------------------------------------------------------------------------
// R2: the quote a nested string or key gets.

const QUOTE_SAMPLES = [
  'plain', "it's", 'say "hi"', `it's "x"`, "it's \"x\" `", "it's \"x\" ${",
  "it's \"x\" $", "it's \"x\" {", "it's \"x\" $ {", "it's \"x\" {$", '${', '`',
  "'", '"', `'"`, "'\"`", "''", '""', '``', "it's\n", "it's \\ back",
  "it's \"x\"\t", '`${x}`', "'${'", "\"'", "a'b'c", 'a"b"c', "a'b\"c`d",
  "a'b\"c$", "a'b\"c${d}", "$'{\"", "'\"$\u007b", 'it\u2019s', "'".repeat(5),
  '"'.repeat(5), '`'.repeat(5), "'\"".repeat(60),
  "it's \"x\" " + 'y'.repeat(100), "it's " + 'z'.repeat(110),
  "it's \"x\" `" + 'w'.repeat(100), "it's \"x\" ${" + 'v'.repeat(100),
  "'\\'", '"\\"', "'\n\"", "'\t\"\n`", "\\'", '\\"', '\\`',
];

function sectionR2() {
  const r = 'R2';
  for (const s of QUOTE_SAMPLES) {
    add(r, [s]);
    addD(r, O([['q', S(s)]]));
    addD(r, O([[s, N(1)]]));
  }
  add(r, QUOTE_SAMPLES.slice(0, 12));
  add(r, QUOTE_SAMPLES.slice(12, 30));
  addD(r, O([["it's", S('say "hi"')], ['say "hi"', S("it's")], ['`', S("it's \"x\"")]]));
  add(r, { a: ["it's", 'plain'], b: { "c'": '"d"' } });
}

// ---------------------------------------------------------------------------
// R3: escapes, in nested strings and in keys.

function sectionR3() {
  const r = 'R3';
  const controls = [];
  for (let c = 0x00; c <= 0x1f; c++) controls.push(c);
  for (let c = 0x7f; c <= 0x9f; c++) controls.push(c);
  for (const c of controls) {
    const ch = String.fromCharCode(c);
    add(r, ['a' + ch + 'b']);
    addD(r, O([['k' + ch, N(1)]]));
  }
  const all = controls.map((c) => String.fromCharCode(c)).join('');
  const samples = [
    '\b\t\n\f\r\\', '\v', '\0', 'x\u00a0y', '\u00a0', '\u00ad', '\u00a1',
    '\u2028\u2029', '\ufeff', '\u200b\u200c\u200d', '😀', '👨‍👩‍👧',
    '\x1b[31mred\x1b[0m', 'C:\\path\\to\\file', '\\n literal',
    "mixed ' and \\ and \n", all, all + all, '\\'.repeat(10),
    'é\u0301', 'tab\tin\tthe\tmiddle', '\r\n', 'end\\', '\\start',
    "it's\t", "it's \"x\"\t", "it's \"x\" `\t", "it's \"x\" ${\\",
  ];
  for (const s of samples) {
    add(r, [s]);
    addD(r, O([['v', S(s)]]));
    addD(r, O([[s, S(s)]]));
  }
  // strEscape takes a loop up to 100 UTF-16 units, a regex replace above it,
  // and skips its no-escape fast path at 5000 and over. One output for all.
  for (const n of [99, 100, 101, 4999, 5000, 5001]) {
    add(r, ['\t' + 'x'.repeat(n - 1)]);
    add(r, ["'" + 'x'.repeat(n - 2) + '\n']);
    add(r, ['x'.repeat(n)]);
  }
  add(r, { 'a\tb': 'c\nd', 'e\\f': 'g\x00h' });
  add(r, [all.slice(0, 16), all.slice(16, 32), all.slice(32)]);
}

// ---------------------------------------------------------------------------
// R4: numbers.

const NUMBERS = [
  0, -0, 1, -1, 42, -42, 0.5, -0.5, 0.1 + 0.2, 1 / 3, 2 / 3, 1e21, 1e-7, 1e-6,
  123456789012345680000, Number.MAX_SAFE_INTEGER, Number.MIN_SAFE_INTEGER,
  2 ** 53, 2 ** 53 + 2, Number.MAX_VALUE, Number.MIN_VALUE, -Number.MIN_VALUE,
  NaN, Infinity, -Infinity, 1e20, 1e100, 1.5e-10, 123.456, 100, 1e6, 0.000001,
  0.0000001, 2 ** 31, 2 ** 32, 4294967295, -2147483648, 9007199254740993,
  0.1, 255, 3.14159, 1e21 - 65536, 999999999999999900000, 1.7976931348623157e308,
  -1e-7, 1.0000000000000002, 4.35, 0.07, 1e300 * 10, -(1e300 * 10), 2 ** -1074,
  5e-324, 1.2e-322, 12345678901234567890, 0.30000000000000004, 1e15, 1e16, 1e17,
  123e-20, -123456.789,
];

function sectionR4() {
  const r = 'R4';
  for (const x of NUMBERS) {
    add(r, x);
    add(r, [x]);
    add(r, { n: x });
  }
  add(r, NUMBERS);
  add(r, { nums: NUMBERS });
  add(r, NUMBERS.slice(0, 7));
  add(r, NUMBERS.slice(20, 40));
  add(r, { zero: 0, negZero: -0, nan: NaN, inf: Infinity, ninf: -Infinity });
  add(r, [-0, -0, -0, -0, -0, -0, -0]);
  add(r, -0, [-0], { z: -0 });
}

// ---------------------------------------------------------------------------
// R5: keys, bare or quoted.

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
  '-', '@', '#hash', 'a/b', 'π', 'Ω', 'z9_', '_9', '9_',
];

function sectionR5() {
  const r = 'R5';
  for (const k of KEYS) {
    addD(r, O([[k, N(1)]]));
    addD(r, O([[k, S('v')], ['b', N(2)]]));
    addD(r, O([['inner', O([[k, B(true)]])]]));
  }
  addD(r, O(KEYS.map((k, i) => [k, N(i)])));
  addD(r, O(KEYS.slice(0, 10).map((k) => [k, N(0)])));
  const shape = ['$x', 'x y', 'if', 'café'];
  addD(r, A([0, 1, 2].map((i) => O(shape.map((k) => [k, N(i)])))));
}

// ---------------------------------------------------------------------------
// R6: one key set in different creation orders.

function permutations(xs) {
  if (xs.length <= 1) return [xs];
  const out = [];
  xs.forEach((x, i) => {
    for (const p of permutations([...xs.slice(0, i), ...xs.slice(i + 1)])) out.push([x, ...p]);
  });
  return out;
}

function sectionR6() {
  const r = 'R6';
  const vals = { a: N(1), b: S('two'), c: B(true) };
  const perms = permutations(['a', 'b', 'c']);
  for (const p of perms) addD(r, O(p.map((k) => [k, vals[k]])));
  addD(r, A(perms.map((p) => O(p.map((k) => [k, vals[k]])))));
  for (const p of perms) addD(r, O([['x', O(p.map((k) => [k, vals[k]]))]]));
  const five = ['id', 'name', 'email', 'active', 'score'];
  const fv = { id: N(7), name: S('Ann'), email: S('ann@example.com'), active: B(false), score: N(9.5) };
  const orders = [five, [...five].reverse(), ['email', 'id', 'score', 'name', 'active'], ['active', 'score', 'id', 'email', 'name']];
  for (const p of orders) addD(r, O(p.map((k) => [k, fv[k]])));
  addD(r, A(orders.map((p) => O(p.map((k) => [k, fv[k]])))));
  addD(r, O([['users', A(orders.map((p) => O(p.map((k) => [k, fv[k]]))))]]));
  for (const p of perms) addD(r, O(p.map((k) => [k + k + k, S(k.repeat(20))])));
}

// ---------------------------------------------------------------------------
// R7: empty containers, also past the depth limit.

function sectionR7() {
  const r = 'R7';
  add(r, {});
  add(r, []);
  add(r, { a: {} });
  add(r, { a: [] });
  add(r, [[]]);
  add(r, [{}]);
  add(r, [[], []]);
  add(r, [{}, {}]);
  add(r, { a: { b: {} } });
  add(r, { a: { b: { c: {} } } });
  add(r, { a: { b: { c: [] } } });
  add(r, { a: { b: { c: { d: {} } } } });
  add(r, { a: { b: { c: { d: [] } } } });
  add(r, [[[]]]);
  add(r, [[[[]]]]);
  add(r, [[[[[]]]]]);
  add(r, [[[{}]]]);
  add(r, [[[[{}]]]]);
  add(r, { a: [[[]]] });
  add(r, { a: [{ b: [] }] });
  add(r, { a: [{ b: [{}] }] });
  add(r, { a: {}, b: [], c: '' });
  add(r, { a: {}, b: [], c: 'x'.repeat(70) });
  add(r, [[], [], [], [], [], [], []]);
  add(r, [{}, {}, {}, {}, {}, {}, {}]);
  add(r, Array.from({ length: 30 }, () => []));
  add(r, Array.from({ length: 101 }, () => ({})));
  add(r, { e: Array.from({ length: 8 }, () => []) });
  add(r, [[[], []], [[], []]]);
  add(r, { a: { b: { c: {}, d: [], e: 1 } } });
  add(r, { a: { b: { c: { x: 1 }, d: {} } } });
  add(r, [], [], {});
  add(r, '', {}, []);
}

// ---------------------------------------------------------------------------
// R8: depth. Past depth 2, [Object] and [Array].

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

function sectionR8() {
  const r = 'R8';
  for (const levels of [1, 2, 3, 4, 5, 6]) {
    add(r, nestObj(levels, 1));
    add(r, nestObj(levels, 'leaf'));
    add(r, nestArr(levels, 1));
    add(r, nestArr(levels, 'leaf'));
    add(r, nestObj(levels, { x: 1, y: [2] }));
    add(r, nestArr(levels, { x: 1 }));
    add(r, nestObj(levels, [1, 2]));
  }
  add(r, { a: [{ b: [1] }] });
  add(r, [{ a: [{ b: 1 }] }]);
  add(r, { a: { b: [1, 2, 3] }, c: { d: { e: 1 } } });
  add(r, { a: { b: { c: [1], d: { e: 1 } } }, f: 1 });
  add(r, [[[1, 2], [3, 4]], [[5, 6], [7, 8]]]);
  add(r, [[[[1, 2]], [[3]]]]);
  add(r, { a: [[[{ deep: 1 }]]] });
  add(r, { a: 1, b: { c: 2, d: { e: 3, f: { g: 4, h: { i: 5 } } } } });
  add(r, { config: { server: { tls: { cert: 'x', key: 'y' } } } });
  add(r, { config: { server: { tls: { cert: 'x', key: 'y' }, port: 443 } } });
  add(r, Array.from({ length: 8 }, (_, i) => ({ id: i, nested: { deep: { deeper: i } } })));
  add(r, { list: Array.from({ length: 8 }, (_, i) => ({ id: i, tags: ['a'] })) });
  add(r, { a: { b: Array.from({ length: 8 }, () => ({ c: 1 })) } });
  add(r, { a: { b: Array.from({ length: 8 }, () => []) } });
  add(r, [nestObj(3, 1), nestObj(3, 2)]);
  add(r, nestObj(3, 1), nestArr(3, 1));
}

// ---------------------------------------------------------------------------
// R9: one line or several, at and around the threshold.
//
// Each family grows one string (or number) by k units until the container
// holding it breaks onto several lines, then records k-1, k and k+1 around
// the last single-line k. Wrapping containers are forced onto several lines
// by a long sibling, so the inner container decides on its own at its own
// indentation.

const P75 = 'p'.repeat(75);

function wrapLevel(kind, [child, childSib]) {
  if (kind === 'o') {
    return [O([['o', child], ['pad', S(P75)]]), O([['o', childSib], ['pad', S(P75)]])];
  }
  return [A([child, childSib]), A([childSib, childSib])];
}

const R9_SHAPES = {
  obj1: (s) => [O([['zz', S(s)]]), O([['zz', S('q'.repeat(90))]])],
  obj3: (s) => [
    O([['a', N(1)], ['b', S('bb')], ['zz', S(s)]]),
    O([['a', N(1)], ['b', S('bb')], ['zz', S('q'.repeat(90))]]),
  ],
  arr1: (s) => [A([S(s)]), A([S('q'.repeat(90))])],
  arr3: (s) => [A([S('a'), S('bb'), S(s)]), A([S('a'), S('bb'), S('q'.repeat(90))])],
  objref: (s) => [
    O([['zz', S(s)], ['me', R(1)]], 1),
    O([['zz', S('q'.repeat(90))], ['me', R(2)]], 2),
  ],
};
const R9_FILLS = [['x', 'x'], ['cjk', '日'], ['emoji', '😀'], ['latin1', 'é'], ['comb', 'e\u0301'], ['tab', '\t'], ['nl', '\n']];
const R9_NESTINGS = [[], ['o'], ['a'], ['o', 'o'], ['o', 'a'], ['a', 'o'], ['a', 'a']];

function markerSingle(open, close, marker = 'MRK') {
  return (out) => {
    const line = out.split('\n').find((l) => l.includes(marker));
    const i = line.indexOf(marker);
    return line.lastIndexOf(open, i) !== -1 && line.indexOf(close, i) !== -1;
  };
}

function lastSingle(make, isSingle, kmax) {
  let last = -1;
  let first = -1;
  for (let k = 0; k <= kmax; k++) {
    const single = isSingle(fmt(make(k)));
    if (single && first !== -1) throw new Error('non-monotone threshold');
    if (single) last = k;
    else if (first === -1) first = k;
  }
  if (last < 1 || first !== last + 1) throw new Error(`no threshold in 0..${kmax}`);
  return last;
}

function sectionR9() {
  const r = 'R9';
  const build = (nesting, pair) => {
    let p = pair;
    for (let i = nesting.length - 1; i >= 0; i--) p = wrapLevel(nesting[i], p);
    return renumberIds([p[0]]);
  };
  for (const nesting of R9_NESTINGS) {
    for (const [shapeName, shape] of Object.entries(R9_SHAPES)) {
      const isArr = shapeName.startsWith('arr');
      const fills = shapeName === 'objref' ? R9_FILLS.slice(0, 3) : R9_FILLS;
      for (const [fillName, fill] of fills) {
        const make = (k) => build(nesting, shape('MRK' + fill.repeat(k)));
        const isSingle = markerSingle(isArr ? '[ ' : '{ ', isArr ? ' ]' : ' }');
        const k = lastSingle(make, isSingle, 140);
        notes.push(`R9 ${nesting.join('') || '-'} ${shapeName} ${fillName}: last single-line k=${k}, line ${fmt(make(k)).split('\n').find((l) => l.includes('MRK')).length} chars`);
        for (const kk of [k - 1, k, k + 1]) addD(r, ...make(kk));
      }
    }
    // Numbers: three 18-character fractions and one integer of k+1 digits.
    const num = (k) => [
      A([N(0.7777777777777778), N(0.1111111111111111), N(0.1111111111111111), N(Number('1'.repeat(k + 1)))]),
      A([1, 2, 3, 4].map(() => N(1.2345678901234567e300))),
    ];
    const makeNum = (k) => build(nesting, num(k));
    const kn = lastSingle(makeNum, markerSingle('[ ', ' ]', '0.7777777777777778'), 14);
    for (const kk of [kn - 1, kn, kn + 1]) addD(r, ...makeNum(kk));
    notes.push(`R9 ${nesting.join('') || '-'} num4: last single-line k=${kn}`);
  }
  // Objects of n entries `kNN: 1`: the threshold in entry count.
  const many = (n) => [O(Array.from({ length: n }, (_, i) => ['k' + String(i).padStart(2, '0'), N(1)]))];
  const kmany = lastSingle(many, (out) => !out.includes('\n'), 30);
  for (const n of [kmany - 1, kmany, kmany + 1]) addD(r, ...many(n));
  // A real newline in an entry forces several lines however short.
  add(r, { a: [1, 2, 3, 4, 5, 6, 7] });
  add(r, [[1, 2, 3, 4, 5, 6, 7]]);
  add(r, { a: 1, b: [1, 2, 3, 4, 5, 6, 7] });
  add(r, { a: { b: [1, 2, 3, 4, 5, 6, 7] } });
  add(r, { a: 'x\ny' });
  add(r, { a: 'x\n'.repeat(40) });
  add(r, ['x\n'.repeat(40), 'y']);
  add(r, { s: 'short', t: 'line one\nline two '.repeat(5) });
}

// ---------------------------------------------------------------------------
// R10: grouping arrays of more than six entries.

const WORDS = [
  'alpha', 'beta', 'gamma', 'delta', 'epsilon', 'zeta', 'eta', 'theta', 'iota',
  'kappa', 'lambda', 'mu', 'nu', 'xi', 'omicron', 'pi', 'rho', 'sigma', 'tau',
  'upsilon', 'phi', 'chi', 'psi', 'omega', 'red', 'green', 'blue', 'cyan',
  'magenta', 'yellow', 'black', 'white', 'apple', 'banana', 'cherry', 'date',
  'elderberry', 'fig', 'grape', 'honeydew', 'kiwi', 'lemon', 'mango', 'nectarine',
  'orange', 'papaya', 'quince', 'raspberry', 'strawberry', 'tangerine', 'ugli',
  'vanilla', 'watermelon', 'xigua', 'yam', 'zucchini', 'get', 'set', 'put',
  'post', 'delete', 'patch', 'head', 'options', 'user', 'admin', 'guest', 'root',
  'id', 'name', 'value', 'key', 'item', 'list', 'map', 'node', 'tree', 'leaf',
];

const WIDE = [
  '日本', '中文字', '한국어', '😀', '👍🏽', '👨‍👩‍👧', '🇯🇵', '❤️', 'e\u0301',
  'cafe\u0301', 'é', '\u1100\u1161', '\u1100\u1161\u11a8', '가', 'ｱｲｳ', 'ＡＢＣ',
  'a\u200bb', '\u0915\u093c', '\u09c7\u09be', 'ß', 'Ω', '\u2126', '\u212b',
  '\uf900', 'ﬁ', '…', '—', '€', 'x\u00ady', '\u0301abc', 'x\u20dd', '🏳️‍🌈',
  '1\ufe0f\u20e3', '¼', '\u3000', '〜', 'Ｑ', '\u0b47\u0b3e', '\u0d46\u0d3e',
  '\u{1d15e}', '\u{1d157}\u{1d165}', '\u0344', '\u0958', '\u304b\u3099', '\u304c',
  '\u30ab\u3099', '\u3099', '\u{1d160}', '\u302a',
];

function sectionR10() {
  const r = 'R10';
  const gens = {
    digit: (i) => N(i % 10),
    mixedNum: (i) => N([1, 22, 333, -4, 5.5, 4444, -0, 0.25, 100, 7][i % 10]),
    bigNum: (i) => N([123456789, 1e21, -1.5e-7, 9007199254740991, NaN, -Infinity, 0.1 + 0.2][i % 7]),
    shortStr: (i) => S(String.fromCharCode(97 + (i % 26))),
    word: (i) => S(WORDS[i % WORDS.length]),
    varStr: (i) => S('x'.repeat(1 + ((i * 7) % 13))),
    bool: (i) => B(i % 3 === 0),
    emptyArr: () => A([]),
    smallArr: (i) => A([N(i)]),
    pairArr: (i) => A([N(i), N(i * 2)]),
    emptyObj: () => O([]),
    smallObj: (i) => O([['a', N(i)]]),
    wide: (i) => S(WIDE[i % WIDE.length]),
    wideMix: (i) => S(i % 2 ? WIDE[(i * 5) % WIDE.length] : WORDS[i % WORDS.length]),
  };
  const arrOf = (name, n) => A(Array.from({ length: n }, (_, i) => gens[name](i)));
  for (const n of [6, 7, 8, 16, 26, 27, 50]) {
    for (const name of Object.keys(gens)) addD(r, arrOf(name, n));
  }
  // Nested, at indentations 2 and 4.
  for (const n of [7, 16, 27]) {
    for (const name of ['digit', 'word', 'wide', 'mixedNum', 'bool', 'smallObj']) {
      addD(r, O([['list', arrOf(name, n)]]));
      addD(r, O([['a', O([['list', arrOf(name, n)]])]]));
      addD(r, A([arrOf(name, n)]));
      addD(r, A([A([arrOf(name, n)])]));
      addD(r, A([arrOf(name, n), arrOf(name, n)]));
      addD(r, O([['x', N(1)], ['list', arrOf(name, n)], ['y', S('z')]]));
    }
  }
  // maxLength <= 6: one entry of width W, the rest as narrow as they come.
  for (const W of [5, 6, 7, 8]) {
    for (const n of [7, 10, 12, 13, 14, 15]) {
      addD(r, A([N(Number('1'.repeat(W))), ...Array.from({ length: n - 1 }, () => N(1))]));
      addD(r, A([S('x'.repeat(W - 2)), ...Array.from({ length: n - 1 }, () => S(''))]));
    }
  }
  // totalLength / actualMax > 5: exactly 5 is not enough.
  for (const [M, w, n] of [[10, 6, 7], [10, 7, 7], [20, 12, 7], [20, 13, 7], [10, 3, 11], [10, 3, 12], [10, 4, 10], [10, 4, 11]]) {
    addD(r, A([N(Number('9'.repeat(M))), ...Array.from({ length: n - 1 }, () => N(Number('5'.repeat(w))))]));
    addD(r, A([S('m'.repeat(M - 2)), ...Array.from({ length: n - 1 }, () => S('s'.repeat(w - 2)))]));
    // One entry a unit wider than the rest tips the sum past the boundary.
    addD(r, A([N(Number('9'.repeat(M))), N(Number('5'.repeat(w + 1))), ...Array.from({ length: n - 2 }, () => N(Number('5'.repeat(w))))]));
  }
  // actualMax * 3 + indentation < 80, at indentations 0, 2 and 4.
  for (const W of [22, 23, 24, 25]) {
    for (const n of [7, 16, 21, 22, 27, 36]) {
      const arr = () => A(Array.from({ length: n }, (_, i) => S(String.fromCharCode(97 + (i % 26)).repeat(W - 2))));
      addD(r, arr());
      addD(r, O([['list', arr()]]));
      addD(r, O([['a', O([['list', arr()]])]]));
    }
    for (const n of [36, 50]) {
      addD(r, A([S('L'.repeat(W - 2)), ...Array.from({ length: n - 1 }, () => S('s'))]));
      addD(r, O([['list', A([S('L'.repeat(W - 2)), ...Array.from({ length: n - 1 }, () => S('s'))])]]));
    }
  }
  // The column count, entry count by entry count.
  for (let n = 7; n <= 45; n++) {
    addD(r, arrOf('digit', n));
    addD(r, A(Array.from({ length: n }, (_, i) => S(WORDS[(i * 3) % WORDS.length].slice(0, 4)))));
    addD(r, A(Array.from({ length: n }, (_, i) => N(i * 37 % 1000))));
  }
  // Display width: wide, zero-width and composing characters.
  for (const n of [7, 8, 16, 27]) {
    addD(r, A(Array.from({ length: n }, () => S('日本'))));
    addD(r, A(Array.from({ length: n }, () => S('😀'))));
    addD(r, A(Array.from({ length: n }, () => S('e\u0301'))));
    addD(r, A(Array.from({ length: n }, () => S('\u1100\u1161'))));
    addD(r, A(Array.from({ length: n }, (_, i) => S(i % 2 ? '\u1100\u1161\u11a8' : 'abc'))));
    addD(r, A(Array.from({ length: n }, (_, i) => S(i % 2 ? '\u09c7\u09be' : 'ab'))));
    addD(r, A(Array.from({ length: n }, (_, i) => S(i % 3 ? 'x' : '👨‍👩‍👧'))));
    addD(r, A(Array.from({ length: n }, (_, i) => S(i % 2 ? '\u304b\u3099' : 'ka'))));
  }
  for (let start = 0; start < WIDE.length; start += 8) {
    addD(r, A(WIDE.slice(start, start + 9).map(S)));
  }
  addD(r, A(WIDE.map(S)));
  addD(r, O([['wide', A(WIDE.slice(0, 12).map(S))]]));
  // Entries of several lines whose display width is small.
  for (const n of [7, 16, 27]) {
    addD(r, A(Array.from({ length: n }, () => S('x\n' + '\u0301'.repeat(80)))));
  }
}

// ---------------------------------------------------------------------------
// R11: more than 100 items.

function sectionR11() {
  const r = 'R11';
  const gens = {
    digit: (i) => N(i % 10),
    mixedNum: (i) => N([1, 22, 333, -4, 5.5, 4444, -0, 0.25, 100, 7][i % 10]),
    shortStr: (i) => S(String.fromCharCode(97 + (i % 26))),
    longStr: (i) => S('s'.repeat(28) + (i % 10)),
    smallObj: (i) => O([['id', N(i)]]),
    bool: (i) => B(i % 2 === 0),
    emptyArr: () => A([]),
    wide: (i) => S(WIDE[i % WIDE.length]),
  };
  for (const n of [99, 100, 101, 102, 250]) {
    for (const g of Object.values(gens)) addD(r, A(Array.from({ length: n }, (_, i) => g(i))));
    addD(r, O([['data', A(Array.from({ length: n }, (_, i) => gens.digit(i)))]]));
    addD(r, O([['data', A(Array.from({ length: n }, (_, i) => gens.longStr(i)))]]));
  }
}

// ---------------------------------------------------------------------------
// R12: long strings containing newlines, split into lines joined by ` +`.

function sectionR12() {
  const r = 'R12';
  // [wrap, the indentation of the string's own value]
  const contexts = [
    [(s) => A([S(s)]), 2],
    [(s) => O([['k', S(s)]]), 2],
    [(s) => O([['a', O([['k', S(s)]])]]), 4],
    [(s) => O([['a', A([S(s)])]]), 4],
    [(s) => A([A([A([S(s)])])]), 6],
    [(s) => O([['a', O([['b', O([['k', S(s)]])]])]]), 6],
  ];
  const patterns = [
    (L) => 'a\n' + 'x'.repeat(L - 2),
    (L) => 'x'.repeat(L - 1) + '\n',
    (L) => '\n' + 'x'.repeat(L - 1),
    (L) => 'x'.repeat(L >> 1) + '\n' + 'y'.repeat(L - (L >> 1) - 1),
    (L) => Array.from({ length: L }, (_, i) => (i % 10 === 9 ? '\n' : 'l')).join(''),
    (L) => 'ab\n\n' + 'c'.repeat(L - 4),
    (L) => '\n'.repeat(L),
    (L) => ("it's\n" + 'say "x"\n' + '`both` \' "\n' + 'z'.repeat(L)).slice(0, L),
    (L) => ('tab\there\n' + 'back\\slash\n' + 'w'.repeat(L)).slice(0, L),
    (L) => '😀\n' + '😀'.repeat((L - 3) >> 1) + ((L - 3) % 2 ? 'x' : ''),
    (L) => '日本\n' + '語'.repeat(L - 3),
  ];
  for (const [wrap, indent] of contexts) {
    const limit = 80 - indent - 4;
    for (const L of [limit - 1, limit, limit + 1, limit + 2]) {
      for (const p of patterns) {
        const s = p(L);
        if (s.length !== L) throw new Error(`R12 pattern made ${s.length}, wanted ${L}`);
        addD(r, wrap(s));
      }
    }
    // Short strings with newlines never split.
    for (const L of [15, 16, 17, 18]) addD(r, wrap('a\n' + 'b'.repeat(L - 2)));
  }
  const long = 'first line\n' + 'second line is longer than the first\n' + 'x'.repeat(80) + '\nlast';
  add(r, [long, long]);
  add(r, { a: long, b: 'short', c: long });
  add(r, [long, 'tiny']);
  add(r, { list: [long] });
  add(r, { ['k\n'.repeat(50)]: 1 });
  add(r, ["it's\n".repeat(20)]);
  add(r, ['say "x"\n'.repeat(12) + "it's"]);
  add(r, ["'\"`\n".repeat(30)]);
  add(r, ['x'.repeat(200) + '\n' + 'y'.repeat(200)]);
}

// ---------------------------------------------------------------------------
// R13: strings at and around maxStringLength, 10,000 UTF-16 units.

function sectionR13() {
  const r = 'R13';
  const x = (n) => 'x'.repeat(n);
  add(r, [x(9999)]);
  add(r, [x(10000)]);
  add(r, [x(10001)]);
  add(r, [x(10002)]);
  add(r, [x(15000)]);
  add(r, [x(9999) + '😀']); // the pair straddles the cut: its high half stays
  add(r, [x(9998) + '😀']);
  add(r, [x(9998) + '😀' + 'y']);
  add(r, [x(10000) + '😀']);
  add(r, { k: ('line\n' + x(95)).repeat(100) });
  add(r, { k: ('line\n' + x(95)).repeat(100) + 'z' });
  add(r, ["it's" + x(10000) + '"']); // the only " is past the cut
  add(r, ["it's \"x\"" + x(10000) + '`']);
  add(r, x(10001));
  add(r, 'first', x(10001));
  addD(r, O([[x(10001), N(1)]]));
  add(r, [x(5000), x(10001)]);
  add(r, [('ab\n' + x(97)).repeat(99) + x(99) + '😀\n']);
}

// ---------------------------------------------------------------------------
// CIRC: cycles and shared references.

function sectionCIRC() {
  const r = 'CIRC';
  const d = (...args) => addD(r, ...renumberIds(args));
  // An object whose array holds the object itself.
  d(O([['name', S('root')], ['items', A([R(1)])]], 1));
  d(O([['self', R(1)]], 1));
  d(O([['a', N(1)], ['self', R(1)], ['b', S('two')]], 1));
  // Two separate cycles in one value, then the outer cycle found last.
  d(O([['a', O([['self', R(1)]], 1)], ['b', O([['self', R(2)]], 2)]]));
  d(O([['a', O([['self', R(2)]], 2)], ['top', R(1)]], 1));
  d(O([['a', O([['b', O([['self', R(3)]], 3)], ['up', R(2)]], 2)], ['top', R(1)]], 1));
  // Through several levels: the seen check runs before the depth check.
  d(O([['b', O([['c', O([['d', R(1)]])]])]], 1));
  d(O([['b', O([['c', O([['d', O([['e', R(1)]])]])]])]], 1));
  d(O([['a', A([O([['b', A([R(1)])]])])]], 1));
  // In arrays.
  d(A([R(1)], 1));
  d(A([A([]), R(1)], 1));
  d(A([O([['owner', R(1)]])], 1));
  d(A([R(1), R(1), R(1), R(1), R(1), R(1), R(1)], 1));
  d(O([['list', A([R(1), R(1), R(1), R(1), R(1), R(1), R(1), R(1)])]], 1));
  // Parent pointers.
  d(O([['name', S('root')], ['kids', A([
    O([['name', S('a')], ['parent', R(1)]]),
    O([['name', S('b')], ['parent', R(1)]]),
  ])]], 1));
  // Shared, not circular: printed in full each time.
  d(O([['a', O([['x', N(1)]], 1)], ['b', R(1)]]));
  d(A([O([['x', N(1)]], 1), R(1), R(1)]));
  d(O([['x', N(1)]], 1), R(1));
  d(A([N(1), N(2)], 1), O([['again', R(1)]]));
  d(O([['m', A([S('a')], 1)], ['n', R(1)], ['o', O([['p', R(1)]])]]));
  // A cyclic object reached twice: the same index both times.
  d(A([O([['self', R(2)]], 2), R(2)]));
  d(O([['self', R(1)]], 1), R(1));
  // Each argument numbers its own cycles.
  d(O([['self', R(1)]], 1), O([['self', R(2)]], 2));
  d(O([['a', O([['self', R(2)]], 2)], ['top', R(1)]], 1), O([['self', R(3)]], 3));
  // A cycle seen only where the object is shallow enough.
  d(O([['first', O([['w', O([['s', O([['t', R(1)]])]], 1)]])], ['second', R(1)]]));
  // Three cycles, numbered in the order they are found.
  d(O([
    ['x', O([['me', R(2)]], 2)],
    ['y', O([['me', R(3)], ['up', R(1)]], 3)],
    ['z', O([['me', R(4)]], 4)],
  ], 1));
  // The <ref *N> prefix near the single-line threshold, and in a grouped array.
  for (const n of [40, 45, 50, 55, 60]) d(O([['s', S('x'.repeat(n))], ['me', R(1)]], 1));
  d(A(Array.from({ length: 8 }, () => O([['me', R(2)]], 2))));
  d(O([['name', S('node')], ['children', A([O([['name', S('leaf')], ['children', A([R(1)])]])])]], 1));
  d(A([A([R(1)], 2), R(2)], 1));
}

// ---------------------------------------------------------------------------
// RANDOM: seeded random values of realistic and arbitrary shapes.

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

function sectionRandom(count) {
  const rnd = mulberry32(SEED);
  const ri = (lo, hi) => lo + Math.floor(rnd() * (hi - lo + 1));
  const pick = (xs) => xs[Math.floor(rnd() * xs.length)];
  const shuffle = (xs) => {
    for (let i = xs.length - 1; i > 0; i--) {
      const j = Math.floor(rnd() * (i + 1));
      [xs[i], xs[j]] = [xs[j], xs[i]];
    }
    return xs;
  };
  const weighted = (pairs) => {
    let total = 0;
    for (const [, w] of pairs) total += w;
    let x = rnd() * total;
    for (const [val, w] of pairs) { x -= w; if (x < 0) return val; }
    return pairs[pairs.length - 1][0];
  };

  const IDENT_KEYS = [
    'id', 'name', 'type', 'value', 'count', 'items', 'data', 'meta', 'tags', 'url',
    'path', 'method', 'status', 'code', 'message', 'enabled', 'size', 'width',
    'height', 'x', 'y', 'z', 'label', 'title', 'description', 'price', 'qty',
    'user', 'users', 'config', 'options', 'settings', 'children', 'parent', 'next',
    'prev', 'first', 'last', 'total', 'page', 'limit', 'offset', 'created',
    'updated', 'ts', 'level', 'msg', 'host', 'port', 'timeout', 'retries',
    'version', 'env', 'mode', 'debug', 'flags', 'roles', 'email', 'active',
    'score', 'rank', 'lat', 'lon', 'city', 'country', 'zip', 'street', 'color',
    'theme', 'lang', 'locale', 'currency', 'amount', 'balance', 'owner', 'group',
    'kind', 'format', 'encoding', 'headers', 'body', 'query', 'params', 'session',
    'token', 'expires', 'ok', 'error', 'errors', 'warnings', 'result', 'results',
    'stats', 'min', 'max', 'avg', 'sum', 'p50', 'p99', 'a', 'b', 'c', 'k1', 'k2',
    'isValid', 'hasMore', 'nextCursor', 'createdAt', 'updatedAt', '_id', '_meta',
    'snake_case_key', 'veryLongPropertyNameForTesting', 'if', 'for', 'default',
  ];
  const SPECIAL_KEYS = KEYS.filter((k) => !/^[a-zA-Z_][a-zA-Z_0-9]*$/.test(k));
  const CITIES = ['London', 'Paris', 'São Paulo', '東京', 'Zürich', 'Kraków', 'Reykjavík', 'New York', 'Δελφοί', 'Москва', 'Seoul 서울', 'Cairo', 'Lagos'];
  const NAMES = ['Ann', 'Bob', 'José', 'Zoë', '李雷', 'Øyvind', 'Siobhán', 'Nguyễn', 'O\'Brien', 'Anne-Marie', 'Ō', 'Ali', 'محمد', 'Владимир', 'Chloé'];
  const ROLES = ['admin', 'editor', 'viewer', 'owner', 'guest', 'billing', 'support'];
  const METHODS = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'];
  const LEVELS = ['debug', 'info', 'warn', 'error', 'fatal'];
  const UNI = [
    'àéîõü', 'çñßøå', 'æœÿ', '日本語', '中文字', '東京大阪', '한국어', '서울',
    '😀', '🎉', '👍🏽', '🇺🇸', '👨‍👩‍👧', '❤️', '✨', '🚀', 'e\u0301', 'n\u0303',
    'a\u030a', 'שלום', 'مرحبا', '€£¥', '©®™', '…—–•', '¶§', '\u200b', '\u200d',
    '\ufeff', '\u00ad', 'ｶﾀｶﾅ', 'ＦＵＬＬ', '\u1100\u1161', 'Ω', 'π≈3.14',
  ];
  const SPECIALS = ["'", '"', '`', '${', '\\', '\n', '\t', '\r', '\b', '\f', '\x00', '\x1b', '\x7f', '\x85', '\u2028', '%', '\u00a0', '$', '{', '}'];

  const NUM_FLAVORS = ['int', 'id', 'float', 'any', 'ts', 'neg', 'ratio', 'small'];
  const STR_FLAVORS = ['word', 'word', 'word', 'sentence', 'ident', 'path', 'email', 'unicode', 'special', 'long', 'empty', 'numstr', 'name', 'city', 'mixed'];

  function genNum(fl) {
    switch (fl) {
      case 'int': return weighted([[ri(0, 9), 4], [ri(10, 99), 3], [ri(100, 9999), 2], [ri(10000, 2 ** 31), 1]]);
      case 'id': return ri(1, 99999);
      case 'float': return Math.round(rnd() * 10 ** ri(2, 6)) / 10 ** ri(0, 3);
      case 'ts': return 1.6e12 + ri(0, 1e11);
      case 'neg': return -ri(1, 1000) / pick([1, 1, 10, 100]);
      case 'ratio': return rnd();
      case 'small': return ri(0, 9);
      default: {
        if (rnd() < 0.08) {
          return pick([NaN, Infinity, -Infinity, -0, 1e21, 1e-7, 5e-324, Number.MAX_VALUE, 2 ** 53, 0.1 + 0.2, 1e300, -1e-300, 123456789012345680000, 1e-6]);
        }
        return genNum(pick(['int', 'float', 'neg', 'ratio', 'id']));
      }
    }
  }

  function words(n) {
    return Array.from({ length: n }, () => pick(WORDS)).join(' ');
  }

  function genStr(fl) {
    switch (fl) {
      case 'word': return pick(WORDS);
      case 'sentence': return words(ri(2, 9));
      case 'ident': return pick(WORDS) + pick(WORDS).replace(/^./, (c) => c.toUpperCase());
      case 'path': return '/' + Array.from({ length: ri(1, 5) }, () => pick([...WORDS, ':id', 'v1', 'api'])).join('/');
      case 'email': return pick(WORDS) + '.' + pick(WORDS) + '@example.' + pick(['com', 'org', 'io']);
      case 'unicode': return Array.from({ length: ri(1, 4) }, () => pick([...UNI, pick(WORDS)])).join(pick(['', ' ', '-']));
      case 'special': return Array.from({ length: ri(1, 6) }, () => (rnd() < 0.5 ? pick(SPECIALS) : pick(WORDS))).join('');
      case 'long': {
        const parts = [];
        const target = weighted([[ri(30, 80), 5], [ri(81, 200), 3], [ri(201, 500), 1]]);
        let len = 0;
        while (len < target) {
          const p = rnd() < 0.15 ? '\n' : rnd() < 0.05 ? pick([...UNI, ...SPECIALS]) : pick(WORDS) + ' ';
          parts.push(p);
          len += p.length;
        }
        return parts.join('');
      }
      case 'empty': return '';
      case 'numstr': return String(genNum('any'));
      case 'name': return pick(NAMES) + (rnd() < 0.5 ? ' ' + pick(NAMES) : '');
      case 'city': return pick(CITIES);
      case 'method': return pick(METHODS);
      case 'role': return pick(ROLES);
      case 'level': return pick(LEVELS);
      default: return genStr(pick(STR_FLAVORS.filter((f) => f !== 'mixed')));
    }
  }

  // Types. A value is generated from a type, so arrays are homogeneous and
  // every object of one type has one key set.
  const tn = (fl) => ({ k: 'n', fl });
  const ts = (fl) => ({ k: 's', fl });
  const tb = () => ({ k: 'b' });
  const ta = (el) => ({ k: 'a', el });
  const to = (fields) => ({ k: 'o', fields });

  function sig(t) {
    switch (t.k) {
      case 'a': return `a<${sig(t.el)}>`;
      case 'o': return `o{${t.fields.map(([k, f]) => JSON.stringify(k) + ':' + sig(f)).sort().join(',')}}`;
      default: return t.k;
    }
  }

  function genKey(used) {
    for (;;) {
      const k = rnd() < 0.07 ? pick(SPECIAL_KEYS) : pick(IDENT_KEYS);
      if (!used.has(k)) { used.add(k); return k; }
    }
  }

  function genPrim() {
    return weighted([[tn(pick(NUM_FLAVORS)), 4], [ts(pick(STR_FLAVORS)), 4.5], [tb(), 1.5]]);
  }

  function genType(d, maxD) {
    if (d >= maxD) return genPrim();
    const kind = weighted([['p', d === 0 ? 0 : 45], ['a', 25], ['o', 30]]);
    if (kind === 'p') return genPrim();
    if (kind === 'a') return ta(genType(d + 1, maxD));
    const used = new Set();
    const n = weighted([[1, 2], [2, 3], [3, 3], [4, 2], [5, 1], [6, 1], [8, 0.4], [12, 0.15]]);
    return to(Array.from({ length: n }, () => [genKey(used), genType(d + 1, maxD)]));
  }

  const TEMPLATES = [
    () => to([['method', ts('method')], ['path', ts('path')], ['handler', ts('ident')], ['middlewares', ta(ts('ident'))], ['auth', tb()], ['rateLimit', tn('int')]]),
    () => ta(to([['method', ts('method')], ['path', ts('path')], ['handler', ts('ident')]])),
    () => to([['id', tn('id')], ['name', ts('name')], ['email', ts('email')], ['roles', ta(ts('role'))], ['active', tb()], ['profile', to([['age', tn('small')], ['city', ts('city')], ['tags', ta(ts('word'))]])]]),
    () => ta(to([['id', tn('id')], ['name', ts('name')], ['active', tb()]])),
    () => to([['server', to([['host', ts('word')], ['port', tn('int')], ['tls', to([['enabled', tb()], ['cert', ts('path')]])]])], ['db', to([['url', ts('path')], ['pool', to([['min', tn('small')], ['max', tn('int')]])]])], ['features', ta(ts('ident'))]]),
    () => to([['ts', tn('ts')], ['level', ts('level')], ['msg', ts('sentence')], ['meta', to([['requestId', ts('ident')], ['durationMs', tn('float')]])]]),
    () => ta(ta(tn(pick(['small', 'int', 'float', 'neg'])))),
    () => to([['lat', tn('ratio')], ['lon', tn('ratio')], ['name', ts('city')]]),
    () => ta(to([['sku', ts('ident')], ['qty', tn('small')], ['price', tn('float')], ['dims', ta(tn('float'))]])),
    () => to([['en', ts('sentence')], ['ja', ts('unicode')], ['emoji', ts('unicode')]]),
    () => ta(ts(pick(['word', 'unicode', 'special', 'empty', 'name', 'numstr']))),
    () => ta(tn(pick(NUM_FLAVORS))),
    () => ta(tb()),
    () => to([['status', tn('int')], ['ok', tb()], ['headers', to([['content-type', ts('word')], ['x-request-id', ts('ident')]])], ['body', ts('long')]]),
    () => ({ k: 'tree' }),
    () => ta({ k: 'tree' }),
    () => ({ k: 'graph' }),
  ];

  function pickType() {
    if (rnd() < 0.35) return pick(TEMPLATES)();
    const maxD = weighted([[1, 15], [2, 30], [3, 30], [4, 15], [5, 10]]);
    return genType(0, maxD);
  }

  function genLen(el, d) {
    const prim = el.k === 'n' || el.k === 's' || el.k === 'b';
    if (prim) {
      return weighted([[ri(0, 4), 55], [ri(5, 12), 25], [ri(13, 30), 13], [ri(31, 110), 5], [ri(111, 260), 2]]);
    }
    if (d >= 3) return ri(0, 2);
    if (d >= 2) return weighted([[ri(0, 3), 8], [ri(4, 6), 2]]);
    return weighted([[ri(0, 3), 6], [ri(4, 8), 3], [ri(9, 16), 1]]);
  }

  // Generation state for one entry. reg maps a type's signature to the
  // containers of that type made so far, ancestors included, for refs.
  let ctx;

  function container(s, desc) {
    ctx.nodes++;
    if (!ctx.reg.has(s)) ctx.reg.set(s, []);
    ctx.reg.get(s).push(desc);
    return desc;
  }

  function maybeRef(s, p) {
    const cands = ctx.reg.get(s);
    if (cands && cands.length && rnd() < p) return { ref: pick(cands) };
    return null;
  }

  function genVal(t, d) {
    ctx.nodes++;
    switch (t.k) {
      case 'n': return N(genNum(t.fl));
      case 's': return S(genStr(t.fl));
      case 'b': return B(rnd() < 0.5);
      case 'a': {
        const s = sig(t);
        const ref = maybeRef(s, 0.03);
        if (ref) return ref;
        const desc = container(s, { a: [] });
        const len = genLen(t.el, d);
        for (let i = 0; i < len; i++) desc.a.push(genVal(t.el, d + 1));
        return desc;
      }
      case 'o': {
        const s = sig(t);
        const ref = maybeRef(s, 0.03);
        if (ref) return ref;
        const desc = container(s, { o: [] });
        const fields = rnd() < 0.12 ? shuffle([...t.fields]) : t.fields;
        for (const [k, ft] of fields) desc.o.push([k, genVal(ft, d + 1)]);
        return desc;
      }
      case 'tree': {
        // { name: string, children: Tree[] }: children may point back up.
        const ref = d > 0 ? maybeRef('tree', 0.1) : null;
        if (ref) return ref;
        const desc = container('tree', { o: [] });
        desc.o.push(['name', S(genStr(pick(['word', 'ident', 'unicode'])))]);
        const kids = container('a<tree>', { a: [] });
        desc.o.push(['children', kids]);
        // Children only while the child's own children array is within
        // five levels of nesting.
        const n = d > 1 ? 0 : weighted([[0, 3], [1, 3], [2, 2], [3, 1]]);
        for (let i = 0; i < n; i++) kids.a.push(genVal(t, d + 2));
        return desc;
      }
      case 'graph': {
        // { id: number, label: string, edges: Graph[] }: edges share nodes.
        const ref = d > 0 ? maybeRef('graph', 0.35) : null;
        if (ref) return ref;
        const desc = container('graph', { o: [] });
        desc.o.push(['id', N(ri(1, 999))]);
        desc.o.push(['label', S(genStr('word'))]);
        const edges = container('a<graph>', { a: [] });
        desc.o.push(['edges', edges]);
        const n = d > 1 ? 0 : weighted([[0, 2], [1, 3], [2, 3], [3, 1]]);
        for (let i = 0; i < n; i++) edges.a.push(genVal(t, d + 2));
        return desc;
      }
      default: throw new Error(`type ${t.k}`);
    }
  }

  // resolveRefs turns { ref: <desc> } into ids numbered 1.. in pre-order.
  function resolveRefs(args) {
    const targets = new Set();
    const collect = (d) => {
      if ('ref' in d) { targets.add(d.ref); return; }
      if ('a' in d) d.a.forEach(collect);
      if ('o' in d) d.o.forEach((p) => collect(p[1]));
    };
    args.forEach(collect);
    let next = 1;
    const walk = (d) => {
      if ('ref' in d) {
        if (typeof d.ref === 'object') {
          if (d.ref.id === undefined) throw new Error('ref before its container');
          d.ref = d.ref.id;
        }
        return;
      }
      if (targets.has(d)) d.id = next++;
      if ('a' in d) d.a.forEach(walk);
      if ('o' in d) d.o.forEach((p) => walk(p[1]));
    };
    args.forEach(walk);
  }

  let made = 0;
  while (made < count) {
    ctx = { nodes: 0, reg: new Map() };
    const nargs = weighted([[1, 55], [2, 25], [3, 12], [4, 8]]);
    const types = [];
    const args = [];
    for (let i = 0; i < nargs; i++) {
      const kind = weighted([['str', 12], ['prim', 4], ['val', 84]]);
      if (kind === 'str') {
        let s = genStr(pick(STR_FLAVORS));
        if (i === 0) s = s.replaceAll('%', '#');
        args.push(S(s));
      } else if (kind === 'prim') {
        args.push(rnd() < 0.6 ? N(genNum('any')) : B(rnd() < 0.5));
      } else {
        const t = types.length && rnd() < 0.3 ? pick(types) : pickType();
        types.push(t);
        args.push(genVal(t, 0));
      }
    }
    // Most entries are kept small, a few may be large: the file stays
    // around 6 MB.
    const big = rnd() < 0.03;
    if (ctx.nodes > (big ? 260 : 70)) continue;
    resolveRefs(args);
    const bytes = JSON.stringify(args).length + fmt(args).length;
    if (bytes > (big ? 9000 : LINE_BUDGET)) continue;
    addD('RANDOM', ...args);
    made++;
  }
}

// ---------------------------------------------------------------------------
// MAYBE: null and undefined (M5 plan D8), at the top level and inside arrays
// and objects; arrays of maybes long enough to group, which pads from the
// left only when every element it looks at is a number; and arrays of
// objects of one shape that leave optional properties out.

function sectionMAYBE() {
  const r = 'MAYBE';
  for (const args of [[undefined], [null], [undefined, null], [null, undefined], ['a', undefined], ['a', null, 'b'],
    [1, undefined], [undefined, 1], [true, null], [[undefined]], [[null]], [{ a: undefined }], [{ a: null }]]) {
    add(r, ...args);
  }
  const mixes = [
    [undefined], [null], [undefined, null], [1, undefined, 3], [undefined, 1], [null, 1, 2],
    ['a', null, 'b'], ['a', undefined], [true, undefined, false], [null, null, null], [undefined, undefined],
  ];
  for (const xs of mixes) add(r, xs);
  // Grouping: longer arrays, an empty at the start, middle and end, or none.
  const rnd = mulberry32(SEED + 1);
  const empties = [undefined, null];
  for (let len = 6; len <= 40; len++) {
    for (const at of [0, Math.floor(len / 2), len - 1, -1]) {
      const nums = Array.from({ length: len }, (_, i) => Math.floor(rnd() * (i % 3 === 0 ? 1000 : 10)) - (i % 7 === 0 ? 5 : 0));
      const strs = Array.from({ length: len }, (_, i) => 'x'.repeat(1 + ((i * 7) % 5)));
      if (at >= 0) {
        nums[at] = empties[len % 2];
        strs[at] = empties[(len + 1) % 2];
      }
      add(r, nums);
      add(r, strs);
    }
  }
  // Past 100 elements, padding looks at the first 101 only.
  for (const len of [101, 102, 105]) {
    for (const at of [0, 50, 99, 100, 101, 104]) {
      if (at >= len) continue;
      const nums = Array.from({ length: len }, (_, i) => i * 3);
      nums[at] = undefined;
      add(r, nums);
    }
  }
  // Objects: present undefined and null, nested, at the depth limit, and long
  // enough to break lines.
  for (const o of [
    { a: undefined }, { a: null }, { a: 1, b: undefined, c: null }, { a: undefined, b: undefined },
    { x: { y: undefined } }, { x: { y: { z: { w: null } } } }, { a: [undefined, null], b: { c: undefined } },
    { longPropertyName: undefined, anotherLongProperty: null, third: 'some value here', fourth: undefined, fifth: 12345 },
    { list: [1, 2, undefined, 4, 5, 6, 7, 8, 9, 10, null, 12, 13], name: null },
  ]) add(r, o);
  // Arrays of one shape with optional properties left out.
  for (const xs of [
    [{ a: 1, b: 2 }, { a: 3 }], [{ a: 1 }, { a: 2, b: 'x' }, { a: 3, b: undefined }],
    [{ id: 1, tag: 'tea' }, { id: 2 }, { id: 3, tag: null }, { id: 4 }, { id: 5, tag: 'chai' }, { id: 6 }, { id: 7 }],
    [undefined, { a: 1 }, null], [[1, undefined], [null], []],
  ]) add(r, xs);
}

// ---------------------------------------------------------------------------

sectionR1();
sectionR2();
sectionR3();
sectionR4();
sectionR5();
sectionR6();
sectionR7();
sectionR8();
sectionR9();
sectionR10();
sectionR11();
sectionR12();
sectionR13();
sectionCIRC();
const deliberate = entries.length;
sectionRandom(TOTAL - deliberate);
sectionMAYBE();

const lines = [];
const index = {};
entries.forEach(({ section, args }, i) => {
  let out;
  try {
    out = check(args);
  } catch (err) {
    err.message = `${section} entry ${i}: ${err.message}`;
    throw err;
  }
  lines.push(JSON.stringify({ args, out }));
  const line = i + 1;
  if (!index[section]) index[section] = { first: line, last: line, count: 0 };
  if (index[section].last < line - 1) throw new Error(`section ${section} is not contiguous`);
  index[section].last = line;
  index[section].count++;
});
fs.writeFileSync(OUT, lines.join('\n') + '\n');
fs.writeFileSync(INDEX, JSON.stringify({ node: process.version, icu: process.versions.icu, unicode: process.versions.unicode, seed: SEED, sections: index }, null, 2) + '\n');

// Self-check: re-read the file, rebuild every value, format again.
const reread = fs.readFileSync(OUT, 'utf8').split('\n');
if (reread.pop() !== '') throw new Error('no trailing newline');
let mismatches = 0;
for (const [i, text] of reread.entries()) {
  const { args, out } = JSON.parse(text);
  if (check(args) !== out) {
    mismatches++;
    if (mismatches <= 5) console.error(`line ${i + 1}: output differs`);
  }
}

if (process.argv.includes('--notes')) for (const n of notes) console.log(n);
console.log(`wrote ${OUT}: ${reread.length} lines, ${fs.statSync(OUT).size} bytes`);
console.log(`deliberate ${deliberate}, random ${reread.length - deliberate}`);
for (const [s, { first, last, count }] of Object.entries(index)) console.log(`  ${s}: ${count} (lines ${first}-${last})`);
console.log(`self-check: ${reread.length} lines re-read, ${mismatches} mismatches`);
if (mismatches) process.exit(1);
