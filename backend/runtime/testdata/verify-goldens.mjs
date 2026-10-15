// verify-goldens.mjs: the generator's self-check, on its own. Re-parses every
// line, holds it to the contract, rebuilds the values and formats them again.
//
//   node verify-goldens.mjs [inspect-goldens.jsonl]

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { checkEntry } from './golden-codec.mjs';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const corpus = process.argv[2] ?? path.join(HERE, 'inspect-goldens.jsonl');
const lines = fs.readFileSync(corpus, 'utf8').split('\n');
if (lines.pop() !== '') throw new Error('no trailing newline');

let bad = 0;
for (const [i, text] of lines.entries()) {
  try {
    const { args, out } = JSON.parse(text);
    if (checkEntry(args) !== out) throw new Error('output differs');
  } catch (err) {
    bad++;
    if (bad <= 5) console.error(`line ${i + 1}: ${err.message}`);
  }
}
console.log(`verify: ${lines.length} lines checked, ${bad} failures (node ${process.version})`);
if (bad) process.exit(1);
