// check-console-log.mjs: console.log on a non-TTY stdout prints exactly
// util.format(...args) + "\n". Holds that against every line of the corpus.
//
//   node check-console-log.mjs [inspect-goldens.jsonl]
//
// The parent runs itself as a child with stdout redirected to a file, the
// child console.logs every entry's rebuilt values, and the parent compares
// the file with the concatenation of every out + "\n".

import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { decodeArgs } from './golden-codec.mjs';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const corpus = process.argv[2] ?? path.join(HERE, 'inspect-goldens.jsonl');
const lines = fs.readFileSync(corpus, 'utf8').split('\n');
lines.pop();

if (process.env.GOLDEN_CHILD === '1') {
  for (const text of lines) console.log(...decodeArgs(JSON.parse(text).args));
} else {
  const outFile = path.join(HERE, 'console-log.out');
  const fd = fs.openSync(outFile, 'w');
  const env = { ...process.env, GOLDEN_CHILD: '1' };
  for (const k of ['FORCE_COLOR', 'NO_COLOR', 'NODE_DISABLE_COLORS', 'NODE_OPTIONS']) delete env[k];
  const res = spawnSync(process.execPath, [fileURLToPath(import.meta.url), corpus], {
    stdio: ['ignore', fd, 'inherit'],
    env,
  });
  fs.closeSync(fd);
  if (res.status !== 0) throw new Error(`child exited ${res.status}`);
  const got = fs.readFileSync(outFile, 'utf8');
  const outs = lines.map((t) => JSON.parse(t).out + '\n');
  const want = outs.join('');
  if (got === want) {
    console.log(`console.log equivalence: ${lines.length} entries, ${Buffer.byteLength(got)} bytes, identical`);
  } else {
    let i = 0;
    while (i < got.length && got[i] === want[i]) i++;
    let pos = 0;
    let entry = 0;
    while (entry < outs.length && pos + outs[entry].length <= i) pos += outs[entry++].length;
    console.log(`console.log equivalence: MISMATCH at byte ${i}, in entry ${entry + 1}`);
    process.exit(1);
  }
}
