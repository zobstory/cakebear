// gen-str-goldens.mjs: a golden corpus of the string methods, recorded from
// Node 24.18.0, for cakebear's runtime (str*.go).
//
//   node gen-str-goldens.mjs
//
// writes str-goldens.jsonl and str-goldens-index.json beside this file, then
// re-reads the corpus and checks every line, and checks the LOG section's
// outputs against what a child process's console.log really writes. No
// dependencies, and deterministic: the random section runs on a seeded
// mulberry32, so a rerun writes a byte-identical file.
//
// Each line is {"fn":"<name>","args":[<value>...],"out":<value>}, or, where
// Node threw, "throws":"<String(err)>" in place of out. Values are encoded
// as in golden-codec.mjs: {"n":"<text>"}, {"s":"..."}, {"u":[<code unit>...]}
// for a string holding a lone surrogate, {"b":true}, {"e":"undefined"} and
// {"e":"null"}, {"a":[...]} and {"o":[...]}. fn is the method, with args[0]
// its receiver and the rest its arguments; an optional argument the call
// leaves out is absent. Besides the methods:
//   compare  args a, b: -1, 0 or 1 as a < b, a === b, a > b (StrCompare)
//   concat   the args joined by + (StrConcat)
//   int      args [{"s":"<decimal>"}], an i64: String() of it (IntToString)
//   uint     the same for a u64 (UintToString)
//   log      console.log(...args): the bytes Node writes, without "\n"
//   inspect  util.inspect(args[0])
//   join     args[0].join(args[1]), an array of strings, or of arrays of
//            them, and a separator (Array.Join)
//   index    args[0][args[1]], a string's code unit or undefined
//            (StrIndexOrUndefined, and StrIndex where it isn't undefined)
//   arrayAt  args[0].at(args[1]), an array of numbers, strings or maybes
//            (Array.RelativeAt)
//   arrayIndex  args[0][args[1]], the same arrays (Array.Lookup)
//
// The index file maps each section to its 1-based line range, and records
// JavaScript's whitespace, every code point trim removes, found by trying
// them all. The CASE_UPPER and CASE_LOWER sections hold every code point
// whose toUpperCase or toLowerCase isn't itself, so the Go test can hold
// every other code point to mapping to itself.

import util from 'node:util';
import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { codeUnits, decodeArgs, encodeArgs, numText } from './golden-codec.mjs';

if (process.version !== 'v24.18.0') {
  throw new Error(`recorded against Node v24.18.0, this is ${process.version}`);
}

const HERE = path.dirname(fileURLToPath(import.meta.url));
const OUT = path.join(HERE, 'str-goldens.jsonl');
const INDEX = path.join(HERE, 'str-goldens-index.json');
const SEED = 0x2026_1015;
const RANDOM = 12000;

const N = (x) => ({ n: numText(x) });
const S = (s) => (s.isWellFormed() ? { s } : { u: codeUnits(s) });

// enc describes a result or an argument: a number, a string, a boolean,
// undefined or null, or split's array of strings.
function enc(x) {
  switch (typeof x) {
    case 'number': return N(x);
    case 'string': return S(x);
    case 'boolean': return { b: x };
    case 'undefined': return { e: 'undefined' };
    case 'object':
      if (x === null) return { e: 'null' };
      if (Array.isArray(x)) return { a: x.map(enc) };
  }
  throw new Error(`cannot describe ${typeof x}`);
}

const ch = (cp) => (cp >= 0xd800 && cp <= 0xdfff ? String.fromCharCode(cp) : String.fromCodePoint(cp));

// ---------------------------------------------------------------------------
// Running one entry

const METHODS = new Set([
  'charAt', 'charCodeAt', 'includes', 'startsWith', 'endsWith', 'indexOf', 'lastIndexOf',
  'slice', 'substring', 'toLowerCase', 'toUpperCase', 'trim', 'trimStart', 'trimEnd',
  'split', 'replace', 'replaceAll', 'padStart', 'padEnd', 'repeat', 'at', 'codePointAt',
]);

function run(fn, args) {
  if (METHODS.has(fn)) {
    if (typeof args[0] !== 'string') throw new Error(`${fn} of a ${typeof args[0]}`);
    return String.prototype[fn].apply(args[0], args.slice(1));
  }
  switch (fn) {
    case 'length': return args[0].length;
    case 'compare': {
      const [a, b] = args;
      const c = a < b ? -1 : a > b ? 1 : 0;
      if ((c === 0) !== (a === b) || (a <= b) !== (c <= 0) || (a >= b) !== (c >= 0)) throw new Error('inconsistent order');
      return c;
    }
    case 'concat': return args.reduce((x, y) => x + y, '');
    case 'int': return BigInt.asIntN(64, BigInt(args[0])).toString();
    case 'uint': return BigInt.asUintN(64, BigInt(args[0])).toString();
    case 'log': return util.format(...args).toWellFormed();
    case 'inspect': return util.inspect(args[0]);
    case 'join': return args[0].join(args[1]);
    case 'index': {
      if (typeof args[0] !== 'string') throw new Error(`index of a ${typeof args[0]}`);
      return args[0][args[1]];
    }
    case 'arrayAt': return args[0].at(args[1]);
    case 'arrayIndex': return args[0][args[1]];
  }
  throw new Error(`unknown fn ${fn}`);
}

// record is what Node makes of one entry: its result, or the RangeError it
// threw.
function record(fn, argsDesc) {
  const args = decodeArgs(argsDesc);
  const again = JSON.stringify(encodeArgs(args));
  if (again !== JSON.stringify(argsDesc)) throw new Error(`not canonical:\n${JSON.stringify(argsDesc)}\n${again}`);
  if (fn === 'int' || fn === 'uint') {
    const t = args[0];
    const v = BigInt(t);
    if (String(v) !== t || (fn === 'int' ? BigInt.asIntN(64, v) : BigInt.asUintN(64, v)) !== v) throw new Error(`${fn} ${t} is out of range`);
  }
  let out;
  try {
    out = run(fn, args);
  } catch (err) {
    if (!(err instanceof RangeError)) throw err;
    return { throws: String(err) };
  }
  return { out: enc(out) };
}

// ---------------------------------------------------------------------------
// The corpus being built

const entries = []; // { section, fn, args }
const seen = new Set();
function add(section, fn, ...args) {
  const desc = encodeArgs(args);
  const key = fn + JSON.stringify(desc);
  if (seen.has(key)) return;
  seen.add(key);
  entries.push({ section, fn, args: desc });
}

// Strings of each kind: ASCII, the BMP, astral, lone surrogates, empty.
const POOL = [
  '', 'a', 'ab', 'abc', 'hello world', 'Hello, World!', 'aaa', 'abcabc', 'a-b-c', 'a,b,,c,', ' x ',
  '\t\n x \r\n', 'ABC xyz 123', '$&$', "it's", '%s %d',
  'é', 'café', 'naïve', '日本語', '日本', 'ΑΣ', 'ß', 'ﬁ', 'İ', 'e\u0301',
  '😀', 'a😀b', '😀😀', '😀a😀', '𝒳y𝒳', '👨\u200D👩\u200D👧', '\u{10FFFF}', '\uFFFF', '\uE000',
  '\uD800', '\uDC00', 'a\uD83Db', 'x\uDE00', '\uDE00\uD83D', '\uD83D\uD83D', '\uDBFF', 'a\uDFFFb',
];
const SHORT = ['', 'a', 'abc', 'hello world', 'aaa', 'café', '日本語', '😀', 'a😀b', '😀😀', '\uD800', 'a\uD83Db', '\uDE00\uD83D', 'x\uDE00'];

// Positions: absent, edges, negatives, fractions, NaN, the infinities, and
// past the end.
const NUMS = [
  0, 1, 2, 3, 4, 5, -1, -2, -3, 0.5, 1.5, 2.9999999, -0.5, -0, NaN, Infinity, -Infinity,
  100, 2 ** 31, 2 ** 32, 2 ** 32 + 1, 2 ** 53, 1e21, -1e21, 1e300, 5e-324, -5e-324,
];
const FEW = [0, 1, 2, -1, 0.5, -0.5, NaN, Infinity, -Infinity, 100];

// Every search string worth trying in s: the pool's short ones, and pieces
// of s, which may be halves.
function searches(s) {
  const xs = ['', 'a', 'b', 'ab', 'x', 'é', '😀', '\uD83D', '\uDE00', s, s + 'x'];
  for (let i = 0; i < Math.min(s.length, 4); i++) {
    xs.push(s.slice(i, i + 1), s.slice(i, i + 2), s.slice(-i - 1));
  }
  return [...new Set(xs)];
}

function sectionLENGTH() {
  for (const s of POOL) {
    add('LENGTH', 'length', s);
    for (const p of NUMS) {
      add('LENGTH', 'charAt', s, p);
      add('LENGTH', 'charCodeAt', s, p);
    }
  }
}

function sectionSEARCH() {
  const AT = [1, -1, 0.5, NaN, Infinity, 100];
  for (const s of POOL) {
    for (const w of searches(s)) {
      for (const fn of ['includes', 'startsWith', 'endsWith', 'indexOf', 'lastIndexOf']) {
        add('SEARCH', fn, s, w);
        if (SHORT.includes(s)) for (const p of AT) add('SEARCH', fn, s, w, p);
      }
    }
  }
  for (const fn of ['includes', 'startsWith', 'endsWith', 'indexOf', 'lastIndexOf']) {
    for (const p of NUMS) {
      add('SEARCH', fn, 'abcabc', 'bc', p);
      add('SEARCH', fn, 'a😀b😀', '😀', p);
      add('SEARCH', fn, 'a😀b😀', '\uDE00', p);
      add('SEARCH', fn, 'abc', '', p);
    }
  }
}

function sectionSLICE() {
  for (const s of POOL) {
    for (const fn of ['slice', 'substring']) {
      add('SLICE', fn, s);
      for (const a of NUMS) add('SLICE', fn, s, a);
    }
  }
  for (const s of SHORT) {
    for (const a of FEW) {
      for (const b of FEW) {
        add('SLICE', 'slice', s, a, b);
        add('SLICE', 'substring', s, a, b);
      }
    }
  }
}

function sectionSPLIT() {
  const LIMITS = [0, 1, 2, 3, -1, -0, NaN, Infinity, -Infinity, 1.5, 2 ** 32, 2 ** 32 + 1, 2 ** 32 - 1, -(2 ** 32) + 1, 1e300];
  for (const s of POOL) {
    for (const sep of ['', ',', ' ', 'a', 'b', 'ab', 'aa', '😀', '\uD83D', '\uDE00', 'x', s, '$']) {
      add('SPLIT', 'split', s, sep);
      add('SPLIT', 'split', s, sep, 2);
    }
  }
  for (const [s, sep] of [['a,b,,c,', ','], ['😀😀', ''], ['abc', ''], ['', ''], ['', ','], ['a😀b', '\uDE00'], ['aaa', 'a']]) {
    for (const lim of LIMITS) add('SPLIT', 'split', s, sep, lim);
  }
}

// Every $ pattern, those a string pattern replaces and those it doesn't.
const TEMPLATES = [
  '', 'x', '$', '$$', '$&', '$`', "$'", '$1', '$0', '$01', '$10', '$99', '$<name>', '$<', '$<>', 'a$', '$$$',
  '$$&', '[$&]', "<$`|$'>", '$&$&', '😀', '\uDE00', '\uD83D', '$\uD83D', 'é$&é', '$`$`', "$'$'", '$$$$', '\uDE00$&\uD83D',
];

function sectionREPLACE() {
  for (const s of POOL) {
    const patterns = ['', 'a', 'b', 'ab', 'x', 'é', '😀', '\uD83D', '\uDE00', s, s.slice(0, 1), s.slice(-1)];
    for (const p of [...new Set(patterns)]) {
      for (const fn of ['replace', 'replaceAll']) {
        for (const r of ['-', '$&', "[$`|$&|$']", '']) add('REPLACE', fn, s, p, r);
      }
    }
  }
  for (const [s, p] of [['abcabc', 'b'], ['aaa', 'a'], ['aaa', 'aa'], ['abc', ''], ['', ''], ['a😀b😀', '😀'], ['😀😀', '\uDE00'], ['😀😀', ''], ['x\uD83Dy', 'y'], ['x\uDE00y', 'x']]) {
    for (const r of TEMPLATES) {
      add('REPLACE', 'replace', s, p, r);
      add('REPLACE', 'replaceAll', s, p, r);
    }
  }
}

function sectionPAD() {
  const LENGTHS = [0, -1, NaN, 1, 2, 3, 5, 8, 10, 2.5, -Infinity, Infinity, 1e9, 2 ** 29 - 23, 2 ** 53];
  const FILLS = [undefined, '', ' ', '-', 'ab', '😀', '\uD83D', '\uDE00', 'a\uD83D', '\uDE00b', 'é日', '\uDE00\uD83D'];
  for (const s of ['', 'a', 'abc', 'café', '😀', 'a😀b', '\uD800', 'x\uDE00']) {
    for (const n of LENGTHS) {
      for (const f of FILLS) {
        for (const fn of ['padStart', 'padEnd']) {
          if (f === undefined) add('PAD', fn, s, n);
          else add('PAD', fn, s, n, f);
        }
      }
    }
  }
}

function sectionREPEAT() {
  const COUNTS = [0, 1, 2, 3, 5, -0, 0.5, 1.9, -0.5, -0.9999, NaN, -1, -1.5, -Infinity, Infinity, 1e9, 2 ** 29, 1e300, -1e21, 2 ** 28];
  for (const s of [...SHORT, '\uDE00', '\uD83D', '\uDE00x\uD83D']) {
    for (const k of COUNTS) {
      if (s !== '' && k >= 2 ** 28 && k < 2 ** 29) continue; // would succeed, and be 256 MB
      add('REPEAT', 'repeat', s, k);
    }
  }
}

// Every code point whose case mapping isn't itself, on its own.
function sectionCASE() {
  for (const [section, fn] of [['CASE_UPPER', 'toUpperCase'], ['CASE_LOWER', 'toLowerCase']]) {
    for (let cp = 0; cp <= 0x10ffff; cp++) {
      if (cp >= 0xd800 && cp <= 0xdfff) continue;
      const c = String.fromCodePoint(cp);
      if (c[fn]() !== c) add(section, fn, c);
    }
  }
}

// The final sigma: a capital sigma between every pair of contexts, and
// runs of them.
function sectionSIGMA() {
  const CONTEXTS = [
    '', 'A', 'a', '1', ' ', "'", '.', ':', '\u0301', '\u0345', '\u00AD', '\u200D', 'A\u0301', "A'", "1'", "'A",
    '𐐀', '𐐨', '😀', '\uD800', '\uDC00', 'Σ', 'ǅ', 'ʰ', 'ª', 'ς', 'İ', 'ß', "A'\u0301", '\u0301\u0301',
  ];
  for (const before of CONTEXTS) {
    for (const after of CONTEXTS) {
      add('SIGMA', 'toLowerCase', before + 'Σ' + after);
    }
  }
  for (const s of ['ΣΣ', 'ΣΣΣ', 'ΑΣΣ', 'ΑΣΣΑ', 'ΑΣ ΑΣ', 'ὈΔΥΣΣΕΎΣ', 'ΌΣΟΣ', 'Α.Σ.', "Α'Σ'", 'ΑΣ😀', 'Σ𐐨', '𐐨Σ', 'aΣ\uD83D', '\uDE00Σ']) {
    add('SIGMA', 'toLowerCase', s);
    add('SIGMA', 'toUpperCase', s);
  }
  for (const s of POOL) {
    add('SIGMA', 'toLowerCase', s);
    add('SIGMA', 'toUpperCase', s);
  }
}

// JavaScript's whitespace, found by trying every code point.
const WHITESPACE = [];
for (let cp = 0; cp <= 0x10ffff; cp++) {
  if (cp >= 0xd800 && cp <= 0xdfff) continue;
  if (String.fromCodePoint(cp).trim() === '') WHITESPACE.push(cp);
}

function sectionTRIM() {
  const NEAR = [0x85, 0x180e, 0x200b, 0x200c, 0x200d, 0x2060, 0xfffe, 0x1c, 0x1d, 0x1e, 0x1f, 0x0, 0xad, 0x2800, 0x3164];
  for (const cp of [...WHITESPACE, ...NEAR]) {
    const c = String.fromCodePoint(cp);
    for (const s of [c, c + 'x' + c, 'x' + c, c + 'x', c + c + '😀' + c, c + '\uD800' + c]) {
      for (const fn of ['trim', 'trimStart', 'trimEnd']) add('TRIM', fn, s);
    }
  }
  for (const s of POOL) {
    for (const fn of ['trim', 'trimStart', 'trimEnd']) add('TRIM', fn, s);
  }
}

// UTF-16 order: the BMP above U+E000 against astral characters, and halves
// against the pairs they start.
function sectionCOMPARE() {
  const XS = [
    '', 'a', 'b', 'ab', 'abc', 'z', '\u007F', '\u0080', 'é', 'e', '\u07FF', '\u0800', '日', '\uD7FF', '\uE000',
    '\uFFFD', '\uFFFF', '😀', '😁', '\u{10000}', '\u{10FFFF}', '\uD800', '\uDBFF', '\uDC00', '\uDFFF', '\uD83D',
    '\uD83D\uFFFF', '\uD83Dz', 'a😀', 'a\uFFFF', 'a\uDE00', '😀a', '\uDE00\uD83D',
  ];
  for (const a of XS) for (const b of XS) add('COMPARE', 'compare', a, b);
}

// Halves meeting at a seam, with empty strings between them.
function sectionCONCAT() {
  const H = '\uD83D';
  const L = '\uDE00';
  const CASES = [
    [], [''], ['a'], ['a', 'b'], ['a', 'b', 'c'], [H, L], [H, '', L], [H, '', '', L], [L, H], [H, H, L],
    [H, L, L], ['a' + H, L + 'b'], [H, L + H, L], ['😀', L], [H, '😀'], [H], [L], [H, 'x', L], ['', H, L, ''],
    ['\uDBFF', '\uDFFF'], ['\uD800', '\uDC00'], ['\uDC00', '\uD800'], ['日', '本'], ['é', '\u0301'],
  ];
  for (const parts of CASES) add('CONCAT', 'concat', ...parts);
  const pieces = ['', 'a', H, L, '😀', 'é', H + 'x', 'x' + L, L + H];
  for (const a of pieces) for (const b of pieces) for (const c of ['', L, H]) add('CONCAT', 'concat', a, b, c);
}

function sectionINT() {
  const ints = [0n, 1n, -1n, 9n, 10n, -10n, 2n ** 31n - 1n, -(2n ** 31n), 2n ** 53n + 1n, 2n ** 63n - 1n, -(2n ** 63n), -(2n ** 63n) + 1n];
  const uints = [0n, 1n, 2n ** 32n - 1n, 2n ** 32n, 2n ** 63n, 2n ** 64n - 1n];
  const rnd = mulberry32(SEED ^ 0x1234);
  for (let i = 0; i < 40; i++) {
    const hi = BigInt(Math.floor(rnd() * 2 ** 32));
    const lo = BigInt(Math.floor(rnd() * 2 ** 32));
    uints.push((hi << 32n) | lo);
    ints.push(BigInt.asIntN(64, (hi << 32n) | lo));
  }
  for (const v of ints) add('INT', 'int', String(v));
  for (const v of uints) add('INT', 'uint', String(v));
}

// join, where a pair split between pieces, elements or the separator,
// pairs again.
function sectionJOIN() {
  const H = '\uD83D';
  const L = '\uDE00';
  const PIECES = ['', 'a', H, L, '😀', 'é', 'a' + H, L + 'b', L + H];
  for (const sep of ['', ',', '-', H, L, L + H, '😀']) {
    for (const a of PIECES) {
      add('JOIN', 'join', [a], sep);
      for (const b of PIECES) add('JOIN', 'join', [a, b], sep);
    }
    add('JOIN', 'join', [], sep);
    add('JOIN', 'join', [H, '', L], sep);
    add('JOIN', 'join', [H, '', '', L, H], sep);
    add('JOIN', 'join', [[H], [L]], sep);
    add('JOIN', 'join', [[H, L], [L]], sep);
    add('JOIN', 'join', [[], [L], [H]], sep);
  }
}

// console.log and util.inspect of strings holding halves.
function sectionLOG() {
  const H = '\uD83D';
  const L = '\uDE00';
  const STRS = ['\uD800', 'a\uDC00b', H + L, L + H, 'x' + H, '\u{10FFFF}', "'\uD800\"", 'a`\uDE00\'"${', 'é\uD800日😀'];
  for (const s of STRS) {
    add('LOG', 'log', s);
    add('LOG', 'log', s, s);
    add('LOG', 'log', 'x', s, 1);
    add('LOG', 'log', [s]);
    add('LOG', 'log', 'k', { k: s });
    add('LOG', 'inspect', s);
    add('LOG', 'inspect', [s, 'ok']);
    add('LOG', 'inspect', { name: s, n: 1 });
  }
  // Long strings: past 100 units Node escapes another way, past 5,000 a
  // third, and past 10,000 it cuts, here through a pair.
  for (const n of [99, 101, 120, 4999, 5001, 9999, 10001]) {
    add('LOG', 'inspect', 'x'.repeat(n) + '\uD800');
    add('LOG', 'inspect', '\uDABC' + 'y'.repeat(n));
    add('LOG', 'inspect', ['z'.repeat(n) + '\uDC00\n' + H]);
  }
  add('LOG', 'inspect', 'x'.repeat(9999) + '😀');
  add('LOG', 'inspect', 'x'.repeat(9999) + '😀😀');
  add('LOG', 'inspect', ['line one\n' + H + ' two\n' + 'w'.repeat(80) + L]);
}

// ---------------------------------------------------------------------------
// RANDOM: calls of every function on strings of every kind.

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

function sectionRANDOM() {
  const rnd = mulberry32(SEED);
  const pick = (xs) => xs[Math.floor(rnd() * xs.length)];
  const ALPHABET = [
    'a', 'b', 'c', 'A', 'Z', ' ', '$', '&', '`', "'", '-', ',', '.', 'é', 'Σ', 'ς', 'σ', 'ß', 'İ', 'ﬁ', '日', '😀',
    '𝒳', '\uD83D', '\uDE00', '\uD800', '\uDFFF', '\uFEFF', '\u00A0', '\u2028', '\n', '\t', '\u0301', '\u0085', '\uFFFF',
  ];
  const str = (max) => {
    let s = '';
    const n = Math.floor(rnd() * (max + 1));
    for (let i = 0; i < n; i++) s += pick(ALPHABET);
    return s;
  };
  // A part of s, or a new string, as the thing to search for.
  const needle = (s) => {
    if (s.length > 0 && rnd() < 0.6) {
      const i = Math.floor(rnd() * s.length);
      return s.slice(i, i + 1 + Math.floor(rnd() * 3));
    }
    return str(2);
  };
  const num = () => {
    const x = rnd();
    if (x < 0.15) return pick(NUMS);
    if (x < 0.85) return Math.floor(rnd() * 16) - 4;
    return Math.round((rnd() * 20 - 6) * 100) / 100;
  };
  const opt = (f) => (rnd() < 0.3 ? [] : [f()]);
  const GEN = {
    length: (s) => [s],
    charAt: (s) => [s, num()],
    charCodeAt: (s) => [s, num()],
    includes: (s) => [s, needle(s), ...opt(num)],
    startsWith: (s) => [s, needle(s), ...opt(num)],
    endsWith: (s) => [s, needle(s), ...opt(num)],
    indexOf: (s) => [s, needle(s), ...opt(num)],
    lastIndexOf: (s) => [s, needle(s), ...opt(num)],
    slice: (s) => (rnd() < 0.2 ? [s] : rnd() < 0.3 ? [s, num()] : [s, num(), num()]),
    substring: (s) => (rnd() < 0.2 ? [s] : rnd() < 0.3 ? [s, num()] : [s, num(), num()]),
    toLowerCase: (s) => [s],
    toUpperCase: (s) => [s],
    trim: (s) => [s],
    trimStart: (s) => [s],
    trimEnd: (s) => [s],
    split: (s) => [s, rnd() < 0.2 ? '' : needle(s), ...opt(num)],
    replace: (s) => [s, rnd() < 0.1 ? '' : needle(s), rnd() < 0.5 ? pick(TEMPLATES) : str(3)],
    replaceAll: (s) => [s, rnd() < 0.1 ? '' : needle(s), rnd() < 0.5 ? pick(TEMPLATES) : str(3)],
    padStart: (s) => [s, num() + 4, ...opt(() => str(3))],
    padEnd: (s) => [s, num() + 4, ...opt(() => str(3))],
    repeat: (s) => [s, Math.floor(rnd() * 5) - (rnd() < 0.1 ? 2 : 0)],
    compare: (s) => [s, rnd() < 0.5 ? str(6) : s.slice(0, Math.floor(rnd() * (s.length + 1))) + str(2)],
    concat: () => Array.from({ length: Math.floor(rnd() * 4) }, () => str(3)),
  };
  const fns = Object.keys(GEN);
  for (let i = 0; entries.length < RANDOM_TARGET && i < RANDOM * 4; i++) {
    const fn = pick(fns);
    add('RANDOM', fn, ...GEN[fn](str(10)));
  }
}

// ---------------------------------------------------------------------------
// ASCII: an ASCII pattern in a string that isn't ASCII, which the runtime
// searches as bytes (str_search_ascii.go). Matches after characters of every
// width, from every position, a pair's low half included; halves beside a
// match, meeting halves in the replacement; split's limits; and strings long
// enough to hold runs of eight ASCII bytes and more. It comes after RANDOM,
// on a seed of its own, so every line before it stays as it was.

function sectionASCII() {
  const H = '\uD83D';
  const L = '\uDE00';
  const WIDE = ['é', '日', '😀', '𝒳', H, L, '\uDBFF', '\uDFFF', '￿', '\u0080'];
  const HALVES = [H, L, '\uDBFF', '\uDFFF'];
  const SEARCH = ['includes', 'startsWith', 'endsWith', 'indexOf', 'lastIndexOf'];
  const short = [];
  const placed = [];
  for (const a of WIDE) {
    const at = [`${a}x${a}`, `x${a}y,x${a}xy`, `${a}${a}xy${a}`];
    placed.push(...at);
    short.push(...at, `${a}x`, `x${a}`, `${a},x,${a},`);
    for (const b of WIDE) if (a !== b) short.push(`${a}xy${b}`);
  }
  for (const s of short) {
    for (const fn of SEARCH) for (const w of ['x', 'xy', 'y', ',', 'x,', 'q']) add('ASCII', fn, s, w);
    for (const sep of [',', 'x', 'xy']) {
      add('ASCII', 'split', s, sep);
      add('ASCII', 'split', s, sep, 1);
      add('ASCII', 'split', s, sep, 2);
    }
    const seams = HALVES.some((h) => s.includes(h));
    const templates = seams
      ? ['-', "[$`|$&|$']", '', H, L, `${L}$&${H}`, `${L}$\`${H}`, '😀']
      : ['-', "[$`|$&|$']", L];
    for (const p of ['x', ',', 'xy']) {
      for (const r of templates) {
        add('ASCII', 'replace', s, p, r);
        add('ASCII', 'replaceAll', s, p, r);
      }
    }
  }
  // Every position, in code units: before, inside and after each character.
  for (const s of placed) {
    for (const fn of SEARCH) {
      for (const w of ['x', 'xy']) {
        for (let p = 0; p <= s.length + 1; p++) add('ASCII', fn, s, w, p);
        for (const p of [-1, 1.5, NaN, Infinity]) add('ASCII', fn, s, w, p);
      }
    }
  }
  for (const s of ['x😀,x😀,x😀,', '😀,,😀,,', 'é,é,é,é']) {
    for (const lim of [0, 1, 2, 3, 4, 5, 100, -1, NaN, 2 ** 32 + 2]) add('ASCII', 'split', s, ',', lim);
  }
  // Longer strings: ASCII runs of up to 24 bytes between wide characters,
  // to 220 bytes, and four to 1,200, searched from random positions.
  const rnd = mulberry32(SEED ^ 0xa5c11);
  const pick = (xs) => xs[Math.floor(rnd() * xs.length)];
  for (let k = 0; k < 44; k++) {
    const long = k >= 40;
    const bytes = long ? 1000 + Math.floor(rnd() * 200) : 20 + Math.floor(rnd() * 200);
    let s = '';
    while (Buffer.byteLength(s) < bytes) {
      const run = Math.floor(rnd() * 25);
      for (let i = 0; i < run; i++) s += pick(['a', 'b', 'x', 'y', ',', ' ']);
      s += pick(WIDE);
    }
    s = s.toWellFormed() === s || rnd() < 0.5 ? s : s.toWellFormed();
    for (const w of long ? ['x', ', '] : ['x', 'ab', ', ', 'xy,']) {
      for (const fn of SEARCH) {
        add('ASCII', fn, s, w);
        add('ASCII', fn, s, w, Math.floor(rnd() * (s.length + 2)));
        if (!long) add('ASCII', fn, s, w, Math.floor(rnd() * (s.length + 2)));
      }
    }
    add('ASCII', 'split', s, ',');
    add('ASCII', 'split', s, ' ', 1 + Math.floor(rnd() * 8));
    add('ASCII', 'replace', s, 'x', '[$&]');
    add('ASCII', 'replaceAll', s, ', ', pick(['|', '$&$&', "$'", L]));
  }
}

// ---------------------------------------------------------------------------
// AT: the reads that may be undefined (M5): a string's at, codePointAt and
// s[i] at every code unit of each string, a pair's lead and trail units
// included, and past both ends, and an array's at and xs[i], over numbers,
// strings and maybes. It comes after ASCII, so every line before it stays
// as it was.

function sectionAT() {
  const MORE = [-4, -5, -6, -7, -10, -100, -1.5, -2.5, -3.5, 1.7, 6, 7, 8, 9, 10];
  const around = (n) => Array.from({ length: 2 * n + 5 }, (_, i) => i - n - 2);
  // A pair's trail before a lone trail, and two lone trails: a trail unit
  // followed by one is no pair, which only these tell from a lead.
  const LONG = ['x😀é\uD800日😀😀yz', '\uDE00\uD83D😀\u{10FFFF}', '😀\uDE00x', '\uDC00\uDFFF'];
  for (const s of [...POOL, ...LONG]) {
    for (const p of [...NUMS, ...MORE, ...around(s.length)]) {
      for (const fn of ['at', 'codePointAt', 'index']) add('AT', fn, s, p);
    }
  }
  const ARRAYS = [
    [], [1], [1, 2, 3], [1.5, -0, NaN, 4], ['a', 'b', 'c', 'd', 'e'], ['😀', '\uD800', ''],
    [1, undefined, 3], [undefined], [null, 'x', undefined], [undefined, undefined, 2], [true, false],
  ];
  for (const xs of ARRAYS) {
    for (const p of [...NUMS, ...MORE, ...around(xs.length)]) {
      add('AT', 'arrayAt', xs, p);
      add('AT', 'arrayIndex', xs, p);
    }
  }
}

// ---------------------------------------------------------------------------

sectionLENGTH();
sectionSEARCH();
sectionSLICE();
sectionSPLIT();
sectionREPLACE();
sectionPAD();
sectionREPEAT();
sectionCASE();
sectionSIGMA();
sectionTRIM();
sectionCOMPARE();
sectionCONCAT();
sectionINT();
sectionJOIN();
sectionLOG();
const deliberate = entries.length;
const RANDOM_TARGET = deliberate + RANDOM;
sectionRANDOM();
sectionASCII();
sectionAT();

const lines = [];
const index = {};
entries.forEach(({ section, fn, args }, i) => {
  let result;
  try {
    result = record(fn, args);
  } catch (err) {
    err.message = `${section} entry ${i} (${fn}): ${err.message}`;
    throw err;
  }
  lines.push(JSON.stringify({ fn, args, ...result }));
  const line = i + 1;
  if (!index[section]) index[section] = { first: line, last: line, count: 0, throws: 0 };
  if (index[section].last < line - 1) throw new Error(`section ${section} is not contiguous`);
  index[section].last = line;
  index[section].count++;
  if (result.throws) index[section].throws++;
});
fs.writeFileSync(OUT, lines.join('\n') + '\n');
fs.writeFileSync(INDEX, JSON.stringify({
  node: process.version,
  icu: process.versions.icu,
  unicode: process.versions.unicode,
  seed: SEED,
  whitespace: WHITESPACE,
  sections: index,
}, null, 2) + '\n');

// Self-check: re-read the file and run every entry again.
const reread = fs.readFileSync(OUT, 'utf8').split('\n');
if (reread.pop() !== '') throw new Error('no trailing newline');
let mismatches = 0;
for (const [i, text] of reread.entries()) {
  const { fn, args, ...result } = JSON.parse(text);
  if (JSON.stringify(record(fn, args)) !== JSON.stringify(result)) {
    mismatches++;
    if (mismatches <= 5) console.error(`line ${i + 1}: output differs`);
  }
}

// And the LOG section's log entries against what console.log writes, in a
// child whose stdout is a file.
const logs = reread.map((t) => JSON.parse(t)).filter((e) => e.fn === 'log');
const env = { ...process.env };
for (const k of ['FORCE_COLOR', 'NO_COLOR', 'NODE_DISABLE_COLORS', 'NODE_OPTIONS']) delete env[k];
const child = spawnSync(process.execPath, ['--input-type=module', '-e', `
  const { decodeArgs } = await import(${JSON.stringify(pathToFileURL(path.join(HERE, 'golden-codec.mjs')).href)});
  const fs = await import('node:fs');
  for (const e of JSON.parse(fs.readFileSync(0, 'utf8'))) console.log(...decodeArgs(e.args));
`], { input: JSON.stringify(logs), env, maxBuffer: 1 << 26 });
if (child.status !== 0) throw new Error(`the console.log child exited ${child.status}: ${child.stderr}`);
const want = Buffer.from(logs.map((e) => e.out.s + '\n').join(''), 'utf8');
const logOK = Buffer.compare(child.stdout, want) === 0;

console.log(`wrote ${OUT}: ${reread.length} lines, ${fs.statSync(OUT).size} bytes`);
console.log(`deliberate ${deliberate}, random ${index.RANDOM.count}, then ${reread.length - deliberate - index.RANDOM.count}`);
for (const [s, { first, last, count, throws }] of Object.entries(index)) {
  console.log(`  ${s}: ${count} (lines ${first}-${last}), ${throws} throw`);
}
console.log(`self-check: ${reread.length} lines re-run, ${mismatches} mismatches`);
console.log(`console.log: ${logs.length} entries, ${child.stdout.length} bytes, ${logOK ? 'identical' : 'DIFFERENT'}`);
if (mismatches || !logOK) process.exit(1);
