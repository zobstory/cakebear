// gen-async-goldens.mjs: what Node 24.18.0 does with rejections, through
// async functions, await, executors, a top-level await, spawn and request
// handlers, for the runtime's half of M6 step 4 (M6 plan D6, D7, D8).
//
//   node gen-async-goldens.mjs
//
// runs every program in async/ with node, and writes what it printed and how
// it ended to async-goldens.json beside this file. Deterministic: the paths
// are written as /CASES, and nothing else in the output varies (m6-node
// R40). Every run is made three times, and the generator stops if the three
// differ. The Go tests hold programs written as the emitter writes them to
// each, 20 runs apiece for the orderings.
//
// As in gen-uncaught-goldens.mjs, a program runs under
// --stack-trace-limit=0 ("limit0"), which is how cakebear prints an error,
// and as it is ("frames"), whose report takes its excerpt from the error's
// top frame. The program's first line names the rule the test builds
// cakebear's report by:
//   ordering             no report: the program ends normally, and only its
//                        output and exit code count;
//   limit0, header-from-frames, throw-site
//                        as in gen-uncaught-goldens.mjs;
//   rejection-value      R13: a rejection of something that isn't an Error.
//                        Node's excerpt is from its own code; cakebear's is
//                        the throw's, which the test says itself, or none.
// A line `// shim` runs the program with shim.mjs, which defines spawn. A
// line `// variants: a b …` runs it once for each, with VARIANT set, and a
// server program (one with process.env.PORT) once for each path in its
// paths line, each the only request, since the first failure ends Node's
// process; the client's result is recorded too.

import fs from 'node:fs';
import http from 'node:http';
import net from 'node:net';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { isDeepStrictEqual } from 'node:util';

if (process.version !== 'v24.18.0') {
  throw new Error(`recorded against Node v24.18.0, this is ${process.version}`);
}

const HERE = path.dirname(fileURLToPath(import.meta.url));
const CASES = path.join(HERE, 'async');
const OUT = path.join(HERE, 'async-goldens.json');
const SHIM = pathToFileURL(path.join(CASES, 'shim.mjs')).href;
const RULES = ['ordering', 'limit0', 'header-from-frames', 'throw-site', 'rejection-value'];

const normalise = (s) => s.replaceAll(pathToFileURL(CASES).href, 'file:///CASES').replaceAll(CASES, '/CASES');

function freePort() {
  return new Promise((resolve) => {
    const s = net.createServer().listen(0, '127.0.0.1', () => {
      const { port } = s.address();
      s.close(() => resolve(port));
    });
  });
}

function get(port, p) {
  return new Promise((resolve) => {
    const req = http.get({ host: '127.0.0.1', port, path: p, agent: false }, (res) => {
      let body = '';
      res.setEncoding('utf8');
      res.on('data', (c) => (body += c));
      res.on('end', () => resolve(`${res.statusCode} ${JSON.stringify(body)}`));
      res.on('error', (e) => resolve(`response error ${e.code}`));
    });
    req.on('error', (e) => resolve(`request error ${e.code}`));
  });
}

// run runs file with flags and the environment env, and, for a server,
// requests p once it listens.
function run(file, flags, env, p) {
  return new Promise(async (resolve) => {
    const port = p === undefined ? 0 : await freePort();
    const child = spawn(process.execPath, [...flags, file], {
      cwd: CASES,
      env: { PATH: process.env.PATH, PORT: String(port), ...env },
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    let stdout = '';
    let stderr = '';
    let client;
    child.stdout.setEncoding('utf8');
    child.stderr.setEncoding('utf8');
    child.stdout.on('data', (c) => {
      stdout += c;
      if (p !== undefined && client === undefined && stdout.includes('listening\n')) client = get(port, p);
    });
    child.stderr.on('data', (c) => (stderr += c));
    const timer = setTimeout(() => child.kill(), 10000);
    child.on('close', async (exit) => {
      clearTimeout(timer);
      const r = { stdout, stderr: normalise(stderr), exit };
      if (client !== undefined) r.client = await client;
      resolve(r);
    });
  });
}

// steady is run, three times, which must agree.
async function steady(key, ...args) {
  const first = await run(...args);
  for (let i = 0; i < 2; i++) {
    const again = await run(...args);
    if (!isDeepStrictEqual(first, again)) {
      throw new Error(`${key}: two runs differ:\n${JSON.stringify(first)}\n${JSON.stringify(again)}`);
    }
  }
  return first;
}

const cases = {};
for (const name of fs.readdirSync(CASES).filter((f) => f.endsWith('.ts')).sort()) {
  const file = path.join(CASES, name);
  const src = fs.readFileSync(file, 'utf8');
  const rule = /^\/\/ rule: (\S+)/.exec(src)?.[1];
  if (!RULES.includes(rule)) throw new Error(`${name}: no rule`);
  const flags = /^\/\/ shim$/m.test(src) ? ['--import', SHIM] : [];
  const paths = /^\/\/ paths: (.+)$/m.exec(src)?.[1].split(' ');
  const variants = /^\/\/ variants: (.+)$/m.exec(src)?.[1].split(' ');
  for (const p of paths ?? [undefined]) {
    for (const v of variants ?? [undefined]) {
      const key = name.slice(0, -3) + (p ?? '') + (v === undefined ? '' : `/${v}`);
      const env = v === undefined ? {} : { VARIANT: v };
      cases[key] = {
        rule,
        limit0: await steady(key, file, ['--stack-trace-limit=0', ...flags], env, p),
        frames: await steady(key, file, flags, env, p),
      };
    }
  }
}
fs.writeFileSync(OUT, JSON.stringify({ node: process.version, cases }, null, 2) + '\n');
console.log(`wrote ${OUT}: ${Object.keys(cases).length} cases`);
for (const [k, c] of Object.entries(cases)) console.log(`  ${k}: ${c.rule}, exit ${c.limit0.exit}${c.limit0.client ? `, client ${c.limit0.client}` : ''}`);
