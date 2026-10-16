// gen-num-goldens.mjs: golden corpora of number formatting and parsing, and
// of the URI functions, recorded from Node 24.18.0, for cakebear's
// NumberToFixed, StringToNumber, ParseInt, ParseFloat, NumberIsFinite,
// NumberIsNaN, NumberIsInteger, Mod, EncodeURIComponent and
// DecodeURIComponent.
//
//   node gen-num-goldens.mjs
//
// writes num-goldens.jsonl and uri-goldens.jsonl beside this file, with an
// index for each, then re-reads both and checks every line. No
// dependencies, and deterministic: the random sections are drawn from a
// seeded mulberry32, so a rerun writes byte-identical files.
//
// Each line is {"fn":"<name>","args":[<value>...],"out":<value>}, where
// out is fn(...args), or has "throws":"<String(err)>" in place of out when
// Node threw. Values are encoded as in golden-codec.mjs: {"n":"<number>"}
// (keeping -0 and NaN), {"s":"<string>"}, {"u":[<code unit>...]} for a
// string holding a lone surrogate, and {"b":<boolean>}. The functions:
//   toFixed            [x, digits]   (x).toFixed(digits)
//   Number             [s]           Number(s)
//   parseInt           [s] [s, r]    parseInt(s), parseInt(s, r)
//   parseFloat         [s]           parseFloat(s)
//   isFinite isNaN isInteger [x]     Number.isFinite(x) and so on
//   mod                [x, y]        x % y
//   encodeURIComponent decodeURIComponent [s]
//
// The number index also lists the code points the parsers skip as
// whitespace, every one tested, so the Go test can check its set against
// all of Unicode. Each index maps a section to its 1-based line range.

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { codeUnits, decodeArgs, numText } from './golden-codec.mjs';

if (process.version !== 'v24.18.0') {
  throw new Error(`recorded against Node v24.18.0, this is ${process.version}`);
}

const HERE = path.dirname(fileURLToPath(import.meta.url));
const SEED = 0x2026_1015;

const N = (x) => ({ n: numText(x) });
const S = (s) => (s.isWellFormed() ? { s } : { u: codeUnits(s) });

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
const int = (n) => Math.floor(rnd() * n);
const pick = (xs) => xs[int(xs.length)];

// The functions, and the type of each argument.
const FNS = {
  toFixed: { call: (x, d) => x.toFixed(d), args: ['n', 'n'], error: RangeError },
  Number: { call: (s) => Number(s), args: ['s'] },
  parseInt: { call: (...a) => parseInt(...a), args: ['s', 'n'], optional: 1 },
  parseFloat: { call: (s) => parseFloat(s), args: ['s'] },
  isFinite: { call: (x) => Number.isFinite(x), args: ['n'] },
  isNaN: { call: (x) => Number.isNaN(x), args: ['n'] },
  isInteger: { call: (x) => Number.isInteger(x), args: ['n'] },
  mod: { call: (x, y) => x % y, args: ['n', 'n'] },
  encodeURIComponent: { call: (s) => encodeURIComponent(s), args: ['s'], error: URIError },
  decodeURIComponent: { call: (s) => decodeURIComponent(s), args: ['s'], error: URIError },
};

function encodeResult(v) {
  switch (typeof v) {
    case 'number': return N(v);
    case 'string': return S(v);
    case 'boolean': return { b: v };
    default: throw new Error(`a result of type ${typeof v}`);
  }
}

// record is what Node makes of one entry: its result, or what it threw.
function record(fn, args) {
  const f = FNS[fn];
  if (!f) throw new Error(`no function ${fn}`);
  const min = f.args.length - (f.optional ?? 0);
  if (args.length < min || args.length > f.args.length) throw new Error(`${fn} of ${args.length} arguments`);
  args.forEach((a, i) => {
    const kind = f.args[i] === 'n' ? ['n'] : ['s', 'u'];
    if (Object.keys(a).length !== 1 || !kind.includes(Object.keys(a)[0])) {
      throw new Error(`${fn} argument ${i}: ${JSON.stringify(a)}`);
    }
  });
  const values = decodeArgs(args);
  values.forEach((v, i) => {
    if (JSON.stringify(f.args[i] === 'n' ? N(v) : S(v)) !== JSON.stringify(args[i])) {
      throw new Error(`${fn} argument ${i} is not canonical: ${JSON.stringify(args[i])}`);
    }
  });
  try {
    return { out: encodeResult(f.call(...values)) };
  } catch (err) {
    if (!f.error || !(err instanceof f.error)) throw err;
    return { throws: String(err) };
  }
}

// ---------------------------------------------------------------------------
// Doubles by their bits

const view = new DataView(new ArrayBuffer(8));
function bitsOf(x) { view.setFloat64(0, x); return view.getBigUint64(0); }
function fromBits(b) { view.setBigUint64(0, BigInt.asUintN(64, b)); return view.getFloat64(0); }
const nextUp = (x) => (x === 0 ? Number.MIN_VALUE : x > 0 ? fromBits(bitsOf(x) + 1n) : fromBits(bitsOf(x) - 1n));
const nextDown = (x) => -nextUp(-x);

// mantExp is x, finite and positive, as mant × 2^exp.
function mantExp(x) {
  const b = bitsOf(x);
  const e = Number((b >> 52n) & 0x7ffn);
  const m = b & ((1n << 52n) - 1n);
  return e === 0 ? [m, -1074] : [m | (1n << 52n), e - 1075];
}

// exactDecimal is m × 2^e, a positive number, as its exact decimal string.
function exactDecimal(m, e) {
  if (e >= 0) return (m << BigInt(e)).toString();
  const k = -e;
  const digits = (m * 5n ** BigInt(k)).toString().padStart(k + 1, '0');
  return `${digits.slice(0, digits.length - k)}.${digits.slice(digits.length - k)}`.replace(/0+$/, '').replace(/\.$/, '');
}

// randomDouble is a double of random bits, finite, below 2^e in magnitude.
function randomDouble(maxExp = 1024) {
  for (;;) {
    const hi = BigInt(int(2 ** 20)), mid = BigInt(int(2 ** 16)), lo = BigInt(int(2 ** 16));
    const exp = BigInt(int(Math.min(2047, maxExp + 1023)));
    const x = fromBits((exp << 52n) | (hi << 32n) | (mid << 16n) | lo);
    if (Number.isFinite(x)) return rnd() < 0.5 ? x : -x;
  }
}

// ---------------------------------------------------------------------------
// The corpus being built

const corpora = { num: [], uri: [] };
let target = corpora.num;
const add = (section, fn, ...args) => target.push({ section, fn, args });

// ---------------------------------------------------------------------------
// FIXED: toFixed.

const FIXED_EDGE = [
  0, -0, 0.5, 1.5, 2.5, 3.5, -0.5, -1.5, -2.5, 0.05, 0.15, 0.25, 0.35, 0.45, 1.005, 1.25, 1.35,
  1.45, 2.675, 8.345, 10.235, 1.0000000000000002, 0.9999999999999999, 0.1, 0.2, 0.3, 0.7,
  0.1 + 0.2, 1 / 3, 2 / 3, -1 / 3, 123.456, -123.456, 0.000001, 1e-7, -1e-7, 0.0001, -0.0001,
  -0.004, -0.005, -0.006, 0.0049999999999999994, 0.125, 0.375, 0.625, 0.875, 9.5, 99.5, 999.5,
  9.995, 99.995, 0.95, 0.995, 0.9995, 19.99, 1234.5678, 42, -42, 1, -1, 100, 1e15, 1e16, 1e17,
  1e20, 1e20 + 65536, 999999999999999900000, 1e21 - 65536 * 2, 1e21, -1e21, nextDown(1e21),
  -nextDown(1e21), nextUp(1e21), 1e22, 1e100, 1.7976931348623157e308, -1.7976931348623157e308,
  Number.MAX_SAFE_INTEGER, Number.MAX_SAFE_INTEGER + 2, 2 ** 53, 2 ** 64, 2 ** 69, 4294967295,
  Number.MIN_VALUE, -Number.MIN_VALUE, 2.2250738585072014e-308, 2 ** -1022 - 2 ** -1074,
  1e-300, 5e-101, 4e-101, 6e-101, 5e-100, 1e-100, 0.5e-99, NaN, Infinity, -Infinity,
  Math.PI, Math.E, -Math.PI, 0.30000000000000004, 1.1, 2.2, 3.3, 4.35, 0.07, 1.15, 1.55,
  2 ** -1, 2 ** -2, 2 ** -10, 2 ** -50, 2 ** -60, 2 ** -101, 3 * 2 ** -101, 2 ** -102,
  1 + 2 ** -52, 1 - 2 ** -53, 255.5, 65535.5, 0.5 - 2 ** -54, 0.5 + 2 ** -53,
];

function sectionFIXED() {
  const r = 'FIXED_EDGE';
  for (const x of FIXED_EDGE) {
    for (let d = 0; d <= 100; d++) add(r, 'toFixed', N(x), N(d));
  }

  // Ties: m × 2^-(f+1) with m odd lies exactly halfway at f places, and
  // its neighbours just below and above don't. At f+1 places a tie is
  // exact, and nothing rounds, so f and f-1 are the counts that matter.
  const t = 'FIXED_TIES';
  for (let f = 0; f <= 100; f++) {
    const ms = [1n, 3n, 5n, 255n, (1n << 53n) - 1n, (1n << 52n) + 1n];
    for (let i = 0; i < 3; i++) ms.push(BigInt(int(2 ** 30)) * 2n ** BigInt(int(23)) * 2n + 1n);
    for (const m of ms) {
      const x = Number(m) * 2 ** -(f + 1);
      if (!(Math.abs(x) < 1e21) || x === 0) continue;
      if (BigInt(Number(m)) !== m) throw new Error('an inexact tie');
      for (const v of [x, -x, nextDown(x), nextUp(x)]) {
        add(t, 'toFixed', N(v), N(f));
        if (f > 0) add(t, 'toFixed', N(v), N(f - 1));
      }
    }
  }

  // Digit counts: ToIntegerOrInfinity, then 0 to 100, before x is read.
  const d = 'FIXED_DIGITS';
  const counts = [
    -1, 101, -0.5, -0.9999, 100.5, 100.99, NaN, Infinity, -Infinity, 1e300, -1e300, 2.9, -0,
    0.5, 1e-300, -1e-300, 2 ** 31, -(2 ** 31), 2 ** 32 + 2, 101.5, -1.5, 99.9999,
  ];
  for (const x of [1.5, 0, -0, NaN, Infinity, -Infinity, 1e21, -1e25, 123.456, 2.5, -0.0001]) {
    for (const c of counts) add(d, 'toFixed', N(x), N(c));
  }

  // Random: doubles of random bits, decimals of a few places such as
  // prices, and integers, at random digit counts.
  const rs = 'FIXED_RANDOM';
  for (let i = 0; i < 4000; i++) {
    let x;
    switch (int(4)) {
      case 0: x = randomDouble(int(80) - 10); break;
      case 1: x = Number((int(2e6) / 10 ** int(5)).toFixed(int(6))) * (rnd() < 0.3 ? -1 : 1); break;
      case 2: x = int(2 ** 31) * (rnd() < 0.5 ? 1 : 2 ** int(40)); break;
      default: x = randomDouble(int(2100) - 1075);
    }
    const digits = rnd() < 0.6 ? int(4) : rnd() < 0.5 ? int(21) : int(101);
    add(rs, 'toFixed', N(x), N(digits));
  }
}

// ---------------------------------------------------------------------------
// Parsing: Number, parseFloat and parseInt over the same strings.

// Every code point, the astral ones included, and the code units of lone
// surrogates.
const SPACES = [];
for (let c = 0; c <= 0x10ffff; c++) {
  const ch = String.fromCodePoint(c);
  if (Number(`${ch}1${ch}`) === 1) SPACES.push(c);
}
const NOT_SPACES = [0x85, 0x180e, 0x200b, 0x200c, 0x200d, 0x2060, 0xfffe, 0xad, 0x1c, 0x1d, 0x1e, 0x1f, 0x7f, 0x0, 0x8, 0xe, 0x3164, 0xa0a0];

const PARSE_STRINGS = [
  '', ' ', '  ', '0', '-0', '+0', '00', '000', '-00', '0.0', '-0.0', '.0', '-.0', '0.', '-0.',
  '1', '-1', '+1', '12', '123', '1234567890', '01', '007', '-007', '1.', '.5', '+.5', '-.5',
  '1.5', '-1.5', '1.25', '3.14159', '0.1', '0.30000000000000004', '1e3', '1E3', '1e+3', '1e-3',
  '1e', '1e+', '1e-', '1E', '1.e5', '.e5', '.5e1', '5.e-1', '1e5x', '1e5.5', '1ee5', '1e+-5',
  '1e05', '1e0005', '1e400', '-1e400', '1e-400', '-1e-400', '1e308', '1.8e308', '1.7976931348623157e308',
  '1.7976931348623158e308', '1.7976931348623159e308', '5e-324', '2e-324', '3e-324', '2.4703282292062327e-324',
  '2.4703282292062328e-324', '4.9e-324', '1e-323', '1e21', '1e-7', '123e-20', '9007199254740993',
  '9007199254740992', '9007199254740991', '18446744073709551616', '1' + '0'.repeat(25), '0.' + '0'.repeat(30) + '1',
  '-', '+', '.', '-.', '+.', '+-1', '-+1', '--1', '++1', '- 1', '+ 1', '1 2', '1-2', '1+2', '1,5', '1_000',
  '1__0', '_1', '1_', '0_1', 'Infinity', '-Infinity', '+Infinity', 'infinity', 'INFINITY', 'Inf', '-Inf',
  'Infinit', 'Infinityx', 'Infinity1', '-Infinityx', 'InfinityInfinity', 'NaN', '-NaN', 'nan', 'inf', 'Infinity.5',
  'I', 'In', '+I', '0x10', '0X10', '-0x10', '+0x10', '0x', '0X', '0x0', '0x00', '0xg', '0x1g', '0x1fffffffffffff',
  '0x20000000000001', '0x20000000000003', '0x1fffffffffffff1', '0x1fffffffffffff8', '0x1fffffffffffff9',
  '0x' + 'f'.repeat(256), '0x' + 'f'.repeat(257), '0x' + '1' + '0'.repeat(255), '0xFFFFFFFFFFFFF800', '0xFFFFFFFFFFFFFC00',
  '0xABCDEF', '0xabcdef', '0xAbCdEf', '00x10', '0x0x10', '0x.5', '0x1.5', '0x1p3', '0b', '0b0', '0b1', '0b101', '0B101',
  '0b2', '0b1' + '0'.repeat(60), '0b' + '1'.repeat(54), '0b' + '1'.repeat(53) + '0', '-0b1', '0o', '0o7', '0o17', '0O17',
  '0o8', '0o777', '0o' + '7'.repeat(30), '-0o7', '0z1', '0e1', '0e', '0.e1', '0x1_0', '1x', 'x1', 'abc', '12abc',
  'a12', '١٢', '１２', '1\u00a02', '\u00a0', '\ufeff', '\ufeff1', '1\ufeff', '\u180e', '\u180e1', '\u00851', '1\u0085',
  '1.2.3', '1..2', '..1', '1.2e3.4', '1e1e1', '0.0000001', '0.000001', '1e-6', '123456789012345678901234567890',
  '0.1e-0', '-0e0', '-0e-0', '+0e+0', '0e400', '-0e400', '0.0e-400', '1' + '0'.repeat(308), '1' + '0'.repeat(309),
  '9'.repeat(309), '9'.repeat(400), '0'.repeat(400) + '1', '0.' + '0'.repeat(400) + '1e400', '1' + '0'.repeat(400) + 'e-400',
  '0.' + '0'.repeat(320) + '1', '1e-9999999999', '1e9999999999', '1e+9999999999', '0e9999999999',
  '0.' + '0'.repeat(10010) + '1e10011', '1' + '0'.repeat(10010) + 'e-10010', '\ud800', '1\ud800', '\ud8001', '😀', '1😀',
  '😀1', '\u20001', '1\u2028', '\t\n\v\f\r 1', '1\t\n\v\f\r ', 'true', 'false', 'null', 'undefined', '[]', '{}',
];

function sectionPARSE() {
  const r = 'PARSE';
  for (const s of PARSE_STRINGS) {
    add(r, 'Number', S(s));
    add(r, 'parseFloat', S(s));
    add(r, 'parseInt', S(s));
  }

  // Every whitespace character, alone, around a literal and inside one,
  // and characters that look like whitespace but aren't.
  const w = 'SPACE';
  const forms = (ch) => [
    ch, ch + ch, `${ch}1`, `1${ch}`, `${ch}1${ch}`, `${ch}-1.5e3${ch}`, `${ch}0x1F${ch}`, `${ch}Infinity${ch}`,
    `1${ch}2`, `-${ch}1`, `${ch}+.5`, `0x${ch}1`, `${ch}${ch}12ab`, `${ch}-0`,
  ];
  for (const c of [...SPACES, ...NOT_SPACES]) {
    for (const s of forms(String.fromCodePoint(c))) {
      add(w, 'Number', S(s));
      add(w, 'parseFloat', S(s));
      add(w, 'parseInt', S(s));
    }
  }

  // Decimal strings at and around the halfway point between two doubles,
  // where only an exact reading rounds right.
  const h = 'HALFWAY';
  const xs = [1, 0.1, 1e23, 9007199254740992, 1.7976931348623157e308, Number.MIN_VALUE, 2.2250738585072014e-308,
    2 ** -1022 - 2 ** -1074, 1e-300, 123.456, 5e-324 * 3];
  for (let i = 0; i < 60; i++) xs.push(Math.abs(randomDouble(int(2000) - 1000)));
  for (const x of xs) {
    const [m, e] = mantExp(x);
    const mid = exactDecimal(2n * m + 1n, e - 1);
    const below = mid.replace(/(\d)$/, (dg) => String(Number(dg) - 1));
    const sig = mid.replace('.', '').replace(/^0+/, '');
    for (const s of [mid, below, `${mid}0000000001`, `${mid}${mid.includes('.') ? '' : '.'}000`]) {
      add(h, 'Number', S(s));
      add(h, 'parseFloat', S(s));
    }
    // The same digits with an exponent, and cut short.
    const point = mid.includes('.') ? mid.indexOf('.') : mid.length;
    const lead = mid.replace('.', '').length - sig.length;
    const sci = `${sig[0]}.${sig.slice(1)}e${point - lead - 1}`;
    for (const s of [sci, `-${sci}`, sig.slice(0, 17) + 'e' + (point - lead - 17), sig.slice(0, 40) + 'e' + (point - lead - 40)]) {
      add(h, 'Number', S(s));
      add(h, 'parseFloat', S(s));
    }
  }
}

// parseInt's radices: ToInt32, so wrapping and truncation, and 0 to 37.
const RADICES = [];
for (let r = 0; r <= 37; r++) RADICES.push(r);
RADICES.push(-0, -1, -16, 2.5, 2.999, 16.9, 36.1, 36.9, 37.5, 1.9, 0.5, -0.5, NaN, Infinity, -Infinity,
  2 ** 32, 2 ** 32 + 16, 2 ** 32 + 37, -(2 ** 32) + 16, 2 ** 31, 2 ** 31 + 10, -(2 ** 31), 2 ** 53, 1e10, 1e300, -1e300,
  4294967312.5, Number.MIN_VALUE);

const INT_STRINGS = [
  '', '0', '-0', '1', '-1', '10', '11', '12', '19', '1a', '1z', 'z', 'Z', 'zz', 'ZZ', '10z', '7', '8', '9', 'f', 'F',
  'g', 'ff', 'FF', '0x10', '0X10', '-0x10', '0x', 'x10', '0b11', '0o17', '012', '  42  ', '\n-42', '+42', '-+42', '- 42',
  '1.9', '1e3', '-1.9e3', 'Infinity', '-Infinity', 'NaN', '1_000', '0x1_0', '123456789', '2147483647', '2147483648',
  '4294967296', '9007199254740993', '18446744073709551615', '1' + '0'.repeat(25), 'zzzzzzzzzzzzzzzz', 'ZZZZZZZZZZZZZZZZZZZZ',
  '1'.repeat(64), '7'.repeat(30), 'f'.repeat(20), '0'.repeat(30) + '1', '\u00a01', '١', '１', '😀', '1😀', '\ud8001',
];

function sectionINT() {
  const r = 'RADIX';
  for (const s of INT_STRINGS) {
    for (const radix of RADICES) add(r, 'parseInt', S(s), N(radix));
  }

  // Long runs of each radix's digits: exact for 10 and the powers of two,
  // accumulated in doubles for the rest, so their rounding is V8's own.
  const l = 'RADIX_LONG';
  const DIGITS = '0123456789abcdefghijklmnopqrstuvwxyz';
  for (let radix = 2; radix <= 36; radix++) {
    for (let i = 0; i < 40; i++) {
      const len = 1 + int(i < 20 ? 30 : 120);
      let s = '';
      for (let j = 0; j < len; j++) {
        const dg = DIGITS[int(radix)];
        s += rnd() < 0.5 ? dg.toUpperCase() : dg;
      }
      if (rnd() < 0.2) s = '0'.repeat(1 + int(12)) + s;
      if (rnd() < 0.2) s += pick(['', ' ', 'g', '.5', 'Z!', '-1']);
      if (rnd() < 0.2) s = '-' + s;
      add(l, 'parseInt', S(s), N(radix));
      if (radix === 10 || radix === 16) add(l, 'parseInt', S(s));
    }
    // The largest values of a few lengths, all one digit.
    for (const len of [10, 11, 12, 13, 14, 15, 16, 20, 40]) {
      add(l, 'parseInt', S(DIGITS[radix - 1].repeat(len)), N(radix));
    }
  }
}

// RANDOM: strings of the characters the grammars care about.
function sectionRANDOM() {
  const r = 'RANDOM';
  const ALPHABET = ['0', '0', '1', '5', '9', '7', '.', '.', 'e', 'E', '+', '-', '-', 'x', 'X', 'o', 'b', 'B', 'a', 'f', 'F',
    'g', 'z', 'I', 'Infinity', 'n', ' ', '\t', '\u00a0', '\ufeff', '\u2028', '\u0085', '_', '😀', '\ud800', '0x', '1e', '.5'];
  for (let i = 0; i < 3000; i++) {
    let s = '';
    const len = int(9);
    for (let j = 0; j < len; j++) s += pick(ALPHABET);
    add(r, 'Number', S(s));
    add(r, 'parseFloat', S(s));
    add(r, 'parseInt', S(s));
    add(r, 'parseInt', S(s), N(pick(RADICES)));
  }
  // Numbers as JavaScript writes them, read back.
  for (let i = 0; i < 1500; i++) {
    const x = randomDouble(int(2100) - 1075);
    let s;
    switch (int(4)) {
      case 0: s = String(x); break;
      case 1: s = x.toExponential(int(21)); break;
      case 2: s = Math.abs(x) < 1e21 ? x.toFixed(int(30)) : String(x); break;
      default: s = x.toPrecision(1 + int(30));
    }
    add(r, 'Number', S(s));
    add(r, 'parseFloat', S(s));
    add(r, 'parseInt', S(s));
  }
}

// PREDICATES: Number.isFinite, isNaN and isInteger.
function sectionPREDICATES() {
  const r = 'PREDICATES';
  const xs = [0, -0, 1, -1, 0.5, -0.5, 1.5, NaN, Infinity, -Infinity, Number.MAX_VALUE, -Number.MAX_VALUE,
    Number.MIN_VALUE, -Number.MIN_VALUE, Number.MAX_SAFE_INTEGER, Number.MAX_SAFE_INTEGER + 2, 2 ** 53 + 1, 2 ** 52 + 0.5,
    1e21, 1e300, 1 - 2 ** -53, 1 + 2 ** -52, 4503599627370495.5, -4503599627370495.5, 0.1 + 0.2, 3, 1e-300];
  for (let i = 0; i < 200; i++) xs.push(randomDouble(int(2100) - 1075));
  for (const x of xs) {
    for (const fn of ['isFinite', 'isNaN', 'isInteger']) add(r, fn, N(x));
  }
}

// MOD: %. Every pair of the edge values, then random pairs: any two
// doubles, and two of a size, where the remainder takes the fewest steps.
// It is written last, after the URI sections, so the seeded draws before it,
// and every earlier line, stay as they were.
function sectionMOD() {
  const r = 'MOD';
  const xs = [0, -0, 1, -1, 2, -2, 0.5, -0.5, 1.5, -1.5, 3, -3, 5.5, -5.5, 7, 0.1, 0.3, 1e-7, 2 ** -52, 2 ** 53 + 2,
    4503599627370495.5, 1e21, Number.MAX_VALUE, -Number.MAX_VALUE, Number.MIN_VALUE, -Number.MIN_VALUE, NaN, Infinity,
    -Infinity];
  for (const x of xs) {
    for (const y of xs) add(r, 'mod', N(x), N(y));
  }
  const t = 'MOD_RANDOM';
  for (let i = 0; i < 3000; i++) add(t, 'mod', N(randomDouble()), N(randomDouble(int(2047) - 1023)));
  for (let i = 0; i < 1000; i++) add(t, 'mod', N(randomDouble(int(60))), N(randomDouble(int(60))));
}

// ---------------------------------------------------------------------------
// The URI functions.

const LONE = ['\ud800', '\udbff', '\udc00', '\udfff', '\ud83d', '\ude00', 'a\ud800', '\ud800a', '\udc00\ud800',
  '\ud83d\ud83d\ude00', '\ude00\ude00', 'x\ud83dy\ude00z', '😀\ud800😀', 'end\udbff', '\udbff\udfff\udbff', '%\ud800'];

function sectionENCODE() {
  const r = 'ENC_ASCII';
  for (let c = 0; c < 0x80; c++) {
    const ch = String.fromCharCode(c);
    add(r, 'encodeURIComponent', S(ch));
    add(r, 'encodeURIComponent', S(`a${ch}b${ch}`));
  }
  let all = '';
  for (let c = 0; c < 0x80; c++) all += String.fromCharCode(c);
  add(r, 'encodeURIComponent', S(all));
  for (const s of ['', 'hello world', 'a+b=c&d', 'name=Ann Lee&id=7', '/path/to?q=1#frag', 'already%20encoded',
    "it's (fine)!*~", 'a-b_c.d', '100%', '\r\n', 'tab\there', 'C:\\dir\\file', '"quoted"', '<tag attr="v">', '{"json":[1,2]}']) {
    add(r, 'encodeURIComponent', S(s));
  }

  // Every code point that is 2 bytes in UTF-8, and a sample of the rest of
  // the BMP and of the astral planes, alone and in context.
  const b = 'ENC_UNICODE';
  const cps = [];
  for (let c = 0x80; c < 0x800; c++) cps.push(c);
  for (let c = 0x800; c <= 0xffff; c += 37) if (c < 0xd800 || c > 0xdfff) cps.push(c);
  cps.push(0x800, 0xfff, 0x1000, 0xd7ff, 0xe000, 0xfeff, 0xfffd, 0xfffe, 0xffff, 0x2028, 0x2029, 0x200b, 0x20ac);
  for (let c = 0x10000; c <= 0x10ffff; c += 4099) cps.push(c);
  cps.push(0x10000, 0x1f600, 0x1f468, 0xe0001, 0x10fffd, 0x10ffff, 0x103ff, 0x10400);
  for (const c of cps) add(b, 'encodeURIComponent', S(String.fromCodePoint(c)));
  for (const s of ['café', 'naïve résumé', '日本語', '😀', '👨‍👩‍👧', 'e\u0301', 'Ünïcödé 😀 x', '—→°', '\u{10ffff}a']) {
    add(b, 'encodeURIComponent', S(s));
  }

  const l = 'ENC_LONE';
  for (const s of LONE) add(l, 'encodeURIComponent', S(s));
  for (let c = 0xd800; c <= 0xdfff; c += 61) add(l, 'encodeURIComponent', S(`q=${String.fromCharCode(c)}`));
}

const HEX = (b) => b.toString(16).toUpperCase().padStart(2, '0');
const esc = (...bytes) => bytes.map((x) => `%${HEX(x)}`).join('');

function sectionDECODE() {
  // Every byte as an escape, in both cases.
  const o = 'DEC_OCTET';
  for (let x = 0; x < 256; x++) {
    add(o, 'decodeURIComponent', S(esc(x)));
    add(o, 'decodeURIComponent', S(esc(x).toLowerCase()));
    add(o, 'decodeURIComponent', S(`a${esc(x)}b`));
  }

  // Two-byte sequences: every lead with every second byte. Three- and
  // four-byte ones: every lead, with second bytes either side of each
  // range's bounds, which is where overlong forms, surrogates and code
  // points past U+10FFFF are.
  const s2 = 'DEC_SEQ';
  for (let lead = 0xc0; lead <= 0xdf; lead++) {
    for (let c = 0; c < 256; c++) add(s2, 'decodeURIComponent', S(esc(lead, c)));
  }
  const seconds = [0x00, 0x7f, 0x80, 0x8f, 0x90, 0x9f, 0xa0, 0xbf, 0xc0, 0xff];
  const ends = [0x7f, 0x80, 0xbf, 0xc0];
  for (let lead = 0xe0; lead <= 0xef; lead++) {
    for (const c2 of seconds) for (const c3 of ends) add(s2, 'decodeURIComponent', S(esc(lead, c2, c3)));
  }
  for (let lead = 0xf0; lead <= 0xff; lead++) {
    for (const c2 of seconds) for (const c3 of [0x80, 0xbf, 0x7f]) for (const c4 of ends) {
      add(s2, 'decodeURIComponent', S(esc(lead, c2, c3, c4)));
    }
  }

  // Malformed escapes: cut short, not hex, and a sequence broken off.
  const m = 'DEC_MALFORMED';
  for (const s of ['%', '%%', '%%%', '%2', '%G0', '%0G', '%g0', '%-1', '%+1', '% 1', '%1 ', 'a%', 'a%2', 'abc%4',
    '%%41', '%4%41', '%41%', '%41%4', '%C3', '%C3%', '%C3%A', '%C3A9', '%C3%%A9', '%C3%41', '%E2%82', '%E2%82%',
    '%E2%82%A', '%E2%82%AC%', '%F0%9F%98', '%F0%9F%98%', '%F0%9F%98%8', '%F0%9F%98%80%', '%80', '%BF', '%C0%80',
    '%C1%BF', '%E0%80%80', '%E0%9F%BF', '%ED%A0%80', '%ED%BF%BF', '%F0%80%80%80', '%F0%8F%BF%BF', '%F4%90%80%80',
    '%F5%80%80%80', '%F8%88%80%80%80', '%FC%84%80%80%80%80', '%FE', '%FF', '%é', '%4é', '%C3é', '%\ud800', '%4\ud800',
    '%C3%\ud800', 'ok%E2%82%ACthen%', 'x'.repeat(50) + '%Z', '%' + '😀', '%e2%82%ac%zz']) {
    add(m, 'decodeURIComponent', S(s));
  }

  // Everything that isn't an escape is kept: reserved characters (nothing
  // is reserved for the component form), raw non-ASCII, and lone
  // surrogates.
  const k = 'DEC_KEPT';
  for (const s of ['', 'plain', 'a+b', '%25', '%2525', '%2F%3F%23%26%3D%2B%24%2C%3A%3B%40', '%20%2B+', 'café',
    'caf%C3%A9', '日本%E8%AA%9E', '😀%F0%9F%98%80😀', '%e2%82%ac', '%E2%82%Ac', '%c3%A9', '\ud800%41', '%41\udc00',
    '\ud83d%41\ude00', '\ud83d\ude00', ...LONE, '%EF%BB%BF', '%EF%BF%BD', '%EF%BF%BF', '%F4%8F%BF%BF', '%F0%90%80%80',
    '%E0%A0%80', '%DF%BF', '%C2%80', '%00', '%00%00', '%7F', 'a%00b', '%0A%0D%09']) {
    add(k, 'decodeURIComponent', S(s));
  }

  // Round trips: random strings encoded, and their encodings decoded.
  const t = 'ROUNDTRIP';
  const parts = ['a', 'Z', '0', ' ', '%', '+', '&', '=', '/', '?', '#', 'é', 'ß', '日', '😀', '👨‍👩‍👧', '\u2028', '\ufeff',
    '\u0000', '\u007f', '\u0080', '\u07ff', '\u0800', '\uffff', '\u{10000}', '\u{10ffff}', '~', "'", '(', '*', '\\', '"'];
  for (let i = 0; i < 1500; i++) {
    let s = '';
    const len = int(12);
    for (let j = 0; j < len; j++) s += pick(parts);
    add(t, 'encodeURIComponent', S(s));
    const e = encodeURIComponent(s);
    add(t, 'decodeURIComponent', S(e));
    add(t, 'decodeURIComponent', S(rnd() < 0.5 ? e.toLowerCase() : s));
    // A cut through an escape is malformed, or decodes to less.
    if (e.length > 1) add(t, 'decodeURIComponent', S(e.slice(0, 1 + int(e.length - 1))));
  }
}

// ---------------------------------------------------------------------------

sectionFIXED();
sectionPARSE();
sectionINT();
sectionRANDOM();
sectionPREDICATES();
target = corpora.uri;
sectionENCODE();
sectionDECODE();
target = corpora.num;
sectionMOD();

function write(name, entries, extra) {
  const out = path.join(HERE, `${name}-goldens.jsonl`);
  const lines = [];
  const index = {};
  const seen = new Set();
  let line = 0;
  for (const { section, fn, args } of entries) {
    const key = JSON.stringify([fn, args]);
    if (seen.has(key)) continue;
    seen.add(key);
    let result;
    try {
      result = record(fn, args);
    } catch (err) {
      err.message = `${section} ${fn}${JSON.stringify(args)}: ${err.message}`;
      throw err;
    }
    lines.push(JSON.stringify({ fn, args, ...result }));
    line++;
    if (!index[section]) index[section] = { first: line, last: line, count: 0, throws: 0 };
    if (index[section].last < line - 1) throw new Error(`section ${section} is not contiguous`);
    index[section].last = line;
    index[section].count++;
    if (result.throws) index[section].throws++;
  }
  fs.writeFileSync(out, lines.join('\n') + '\n');
  fs.writeFileSync(path.join(HERE, `${name}-goldens-index.json`),
    JSON.stringify({ node: process.version, seed: SEED, ...extra, sections: index }, null, 2) + '\n');

  // Self-check: re-read the file and call every entry again.
  const reread = fs.readFileSync(out, 'utf8').split('\n');
  if (reread.pop() !== '') throw new Error('no trailing newline');
  let mismatches = 0;
  for (const [i, text] of reread.entries()) {
    const { fn, args, ...result } = JSON.parse(text);
    if (JSON.stringify(record(fn, args)) !== JSON.stringify(result)) {
      mismatches++;
      if (mismatches <= 5) console.error(`${name} line ${i + 1}: result differs`);
    }
  }
  console.log(`wrote ${out}: ${reread.length} lines, ${fs.statSync(out).size} bytes`);
  for (const [s, { first, last, count, throws }] of Object.entries(index)) {
    console.log(`  ${s}: ${count} (lines ${first}-${last}), ${throws} throw`);
  }
  console.log(`  self-check: ${reread.length} lines re-read, ${mismatches} mismatches`);
  return mismatches;
}

let bad = write('num', corpora.num, { whitespace: SPACES });
bad += write('uri', corpora.uri, {});
if (bad) process.exit(1);
