// Writes backend/runtime/str_case_table.go: every code point whose
// toUpperCase or toLowerCase in Node differs from Go's unicode.ToUpper or
// unicode.ToLower, and the two classes of character the final-sigma rule
// reads (M4 strings plan S6).
//
//   node scripts/gen-case-table.mjs | gofmt > backend/runtime/str_case_table.go
//
// Run it with the Node cakebear is compared against: the header records it,
// and the Go version. It runs a small Go program from the repository root,
// so that `go` picks the toolchain go.mod names, whose unicode package may
// be newer than the Go installed (Go 1.26 is Unicode 15, Go 1.27 Unicode
// 17). Rerun it when either changes: the mappings are ICU's and Go's, and
// both follow Unicode. A Go test fails when the toolchain's Unicode version
// is no longer the one recorded here.
//
// The final-sigma classes are found by asking V8, code point by code point,
// what a sigma beside it becomes, and checked against \p{Case_Ignorable} and
// \p{Cased}. Before writing, the table is checked against Node on strings
// that mix every kind of character: mapping each code point on its own, and
// a sigma by its context, must give what Node gives.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

if (process.version !== 'v24.18.0') {
  throw new Error(`recorded against Node v24.18.0, this is ${process.version}`);
}

// Go's mappings: "<unicode.Version>", then "<rune> <upper> <lower>" in hex
// for each code point that either changes.
const GO = `package main

import (
	"fmt"
	"unicode"
)

func main() {
	fmt.Println(unicode.Version)
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if u, l := unicode.ToUpper(r), unicode.ToLower(r); u != r || l != r {
			fmt.Printf("%x %x %x\\n", r, u, l)
		}
	}
}
`;
const ROOT = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'cakebear-case-'));
let goOut;
try {
  fs.writeFileSync(path.join(dir, 'main.go'), GO);
  goOut = execFileSync('go', ['run', path.join(dir, 'main.go')], { cwd: ROOT, encoding: 'utf8', maxBuffer: 1 << 26 });
} finally {
  fs.rmSync(dir, { recursive: true, force: true });
}
const [goUnicode, ...goLines] = goOut.trim().split('\n');
const goMap = new Map();
for (const line of goLines) {
  const [r, u, l] = line.split(' ').map((h) => parseInt(h, 16));
  goMap.set(r, [u, l]);
}
const goUpper = (cp) => (goMap.get(cp) ?? [cp, cp])[0];
const goLower = (cp) => (goMap.get(cp) ?? [cp, cp])[1];

const isSurrogate = (cp) => cp >= 0xd800 && cp <= 0xdfff;
const ch = (cp) => (isSurrogate(cp) ? String.fromCharCode(cp) : String.fromCodePoint(cp));

const upper = [];
const lower = [];
for (let cp = 0; cp <= 0x10ffff; cp++) {
  if (isSurrogate(cp)) continue; // a half has no case, in either
  const c = String.fromCodePoint(cp);
  if (c.toUpperCase() !== String.fromCodePoint(goUpper(cp))) upper.push([cp, c.toUpperCase()]);
  if (c.toLowerCase() !== String.fromCodePoint(goLower(cp))) lower.push([cp, c.toLowerCase()]);
}

// The final-sigma classes. A sigma after "A" then x is final unless x is
// neither ignorable nor cased; after x alone, only if x is cased.
const SIGMA = 'Σ';
const classOf = (x) => {
  if ((x + SIGMA).toLowerCase().endsWith('ς')) return 'cased';
  if (('A' + x + SIGMA).toLowerCase().endsWith('ς')) return 'ignorable';
  return 'none';
};
const reIgnorable = /^\p{Case_Ignorable}$/u;
const reCased = /^\p{Cased}$/u;
const ignorable = [];
const cased = [];
const add = (ranges, cp) => {
  const last = ranges[ranges.length - 1];
  if (last && last[1] === cp - 1) last[1] = cp;
  else ranges.push([cp, cp]);
};
for (let cp = 0; cp <= 0x10ffff; cp++) {
  const x = ch(cp);
  const k = classOf(x);
  const want = reIgnorable.test(x) ? 'ignorable' : reCased.test(x) ? 'cased' : 'none';
  if (k !== want) throw new Error(`U+${cp.toString(16)}: V8 reads it as ${k}, its properties say ${want}`);
  // After the sigma, the same class must decide: "AΣ" then x stays final
  // unless x is cased, and "AΣ" x "A" unless x is ignorable or cased.
  const after = !('AΣ' + x).toLowerCase().startsWith('aς') ? 'cased'
    : !('AΣ' + x + 'A').toLowerCase().startsWith('aς') ? 'ignorable' : 'none';
  if (after !== k) throw new Error(`U+${cp.toString(16)}: ${k} before a sigma, ${after} after one`);
  if (isSurrogate(cp)) {
    if (k !== 'none') throw new Error(`a half, U+${cp.toString(16)}, is ${k}`);
    continue;
  }
  if (k === 'ignorable') add(ignorable, cp);
  if (k === 'cased') add(cased, cp);
}

// The check: the runtime's algorithm, in JavaScript, against Node.
const inRanges = (ranges, cp) => ranges.some(([lo, hi]) => cp >= lo && cp <= hi);
const upperMap = new Map(upper);
const lowerMap = new Map(lower);
const cpsOf = (s) => {
  const out = [];
  for (let i = 0; i < s.length; i++) {
    const c = s.codePointAt(i);
    out.push(c);
    if (c > 0xffff) i++;
  }
  return out;
};
const simulate = (s, toLower) => {
  const cps = cpsOf(s);
  return cps.map((cp, i) => {
    if (isSurrogate(cp)) return String.fromCharCode(cp);
    if (toLower && cp === 0x3a3) {
      let before = false;
      for (let j = i - 1; j >= 0; j--) {
        if (inRanges(ignorable, cps[j])) continue;
        before = inRanges(cased, cps[j]);
        break;
      }
      let after = false;
      for (let j = i + 1; j < cps.length; j++) {
        if (inRanges(ignorable, cps[j])) continue;
        after = inRanges(cased, cps[j]);
        break;
      }
      return before && !after ? 'ς' : 'σ';
    }
    const table = toLower ? lowerMap : upperMap;
    if (table.has(cp)) return table.get(cp);
    return String.fromCodePoint(toLower ? goLower(cp) : goUpper(cp));
  }).join('');
};
let a = 0x2026_1015;
const rnd = () => {
  a = (a + 0x6d2b79f5) >>> 0;
  let t = a;
  t = Math.imul(t ^ (t >>> 15), t | 1);
  t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
};
const kinds = [
  ...upper.map(([cp]) => cp), ...lower.map(([cp]) => cp),
  ...[...goMap.keys()].filter((_, i) => i % 7 === 0),
  0x3a3, 0x3a3, 0x3a3, 0x27, 0x2e, 0x3a, 0x301, 0x345, 0x2b0, 0xaa, 0x20, 0x31, 0x65e5, 0x1f600, 0xd800, 0xdc00,
];
const randomCp = () => (rnd() < 0.5 ? kinds[Math.floor(rnd() * kinds.length)] : Math.floor(rnd() * 0x10000));
let checked = 0;
for (let n = 0; n < 200000; n++) {
  const len = 1 + Math.floor(rnd() * 8);
  let s = '';
  for (let k = 0; k < len; k++) s += ch(randomCp());
  for (const toLower of [false, true]) {
    const want = toLower ? s.toLowerCase() : s.toUpperCase();
    if (simulate(s, toLower) !== want) {
      throw new Error(`${JSON.stringify(s)}.to${toLower ? 'Lower' : 'Upper'}Case(): the table gives ${JSON.stringify(simulate(s, toLower))}, Node ${JSON.stringify(want)}`);
    }
    checked++;
  }
}
process.stderr.write(`gen-case-table: ${upper.length} upper and ${lower.length} lower exceptions, ` +
  `${ignorable.length} ignorable and ${cased.length} cased ranges, ${checked} random strings checked\n`);

const hex = (n) => '0x' + n.toString(16).toUpperCase();
const goString = (s) => '"' + cpsOf(s).map((cp) => {
  if (cp >= 0x20 && cp < 0x7f && cp !== 0x22 && cp !== 0x5c) return String.fromCharCode(cp);
  return cp > 0xffff ? '\\U' + cp.toString(16).padStart(8, '0') : '\\u' + cp.toString(16).padStart(4, '0');
}).join('') + '"';
const map = (name, entries) =>
  `var ${name} = map[rune]string{\n${entries.map(([cp, s]) => `\t${hex(cp)}: ${goString(s)},`).join('\n')}\n}\n`;
const table = (name, ranges) => {
  const lines = [];
  for (let i = 0; i < ranges.length; i += 4) {
    lines.push('\t' + ranges.slice(i, i + 4).map(([lo, hi]) => `{${hex(lo)}, ${hex(hi)}}`).join(', ') + ',');
  }
  return `var ${name} = []caseRange{\n${lines.join('\n')}\n}\n`;
};

process.stdout.write(`// Code generated by scripts/gen-case-table.mjs. DO NOT EDIT.

// Case mapping from Node ${process.version} (ICU ${process.versions.icu}, Unicode ${process.versions.unicode}),
// against Go's unicode package at Unicode ${goUnicode}.

package runtime

// caseTableUnicode is the unicode.Version the exceptions were found against.
const caseTableUnicode = "${goUnicode}"

// caseUpper and caseLower hold each code point whose toUpperCase or
// toLowerCase differs from unicode.ToUpper or unicode.ToLower, with what
// Node makes of it.
${map('caseUpper', upper)}
${map('caseLower', lower)}
// caseIgnorable and cased are the final-sigma rule's classes, as V8 reads
// them: Case_Ignorable, and Cased less what is also Case_Ignorable.
${table('caseIgnorable', ignorable)}
${table('cased', cased)}`);
