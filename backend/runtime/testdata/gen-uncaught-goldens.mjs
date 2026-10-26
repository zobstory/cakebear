// gen-uncaught-goldens.mjs: what Node 24.18.0 prints when a throw goes
// uncaught, for the runtime's report of one (M6 plan D6, uncaught.go).
//
//   node gen-uncaught-goldens.mjs
//
// runs every program in uncaught/ with node, and writes what it printed and
// how it ended to uncaught-goldens.json beside this file. Deterministic: the
// paths are written as /CASES, and nothing else in the output varies (m6-node
// R40). The Go test holds a program written as the emitter writes it to each.
//
// A program runs twice: under --stack-trace-limit=0 ("limit0"), which is how
// cakebear prints an error, with no frames, and as it is ("frames"), since
// with no frames Node takes the source excerpt at an ES module's top level
// from its own code, and with them from the error's top frame (R6, R8). The
// program's first line names the rule the test builds cakebear's report by:
//   limit0               Node's limit0 report, with cakebear's version for
//                        Node's, and without the --trace-uncaught advice;
//   header-from-frames   the excerpt of the frames report, then the limit0
//                        report from its first blank line;
//   throw-site           none: Node points into its own code, and cakebear
//                        at the throw, which the test says itself.
// A server program (one with process.env.PORT) is run once for each path
// in its paths line, each the only request, since the first failure ends
// Node's process; the client's result is recorded too.

import fs from 'node:fs';
import http from 'node:http';
import net from 'node:net';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';

if (process.version !== 'v24.18.0') {
  throw new Error(`recorded against Node v24.18.0, this is ${process.version}`);
}

const HERE = path.dirname(fileURLToPath(import.meta.url));
const CASES = path.join(HERE, 'uncaught');
const OUT = path.join(HERE, 'uncaught-goldens.json');

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

// run runs file with flags, and, for a server, requests p once it listens.
function run(file, flags, p) {
  return new Promise(async (resolve) => {
    const port = p === undefined ? 0 : await freePort();
    const child = spawn(process.execPath, [...flags, file], {
      cwd: CASES,
      env: { PATH: process.env.PATH, PORT: String(port) },
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

const cases = {};
for (const name of fs.readdirSync(CASES).filter((f) => f.endsWith('.ts')).sort()) {
  const file = path.join(CASES, name);
  const src = fs.readFileSync(file, 'utf8');
  const rule = /^\/\/ rule: (\S+)/.exec(src)?.[1];
  if (!['limit0', 'header-from-frames', 'throw-site'].includes(rule)) throw new Error(`${name}: no rule`);
  const paths = /^\/\/ paths: (.+)$/m.exec(src)?.[1].split(' ');
  for (const p of paths ?? [undefined]) {
    const key = name.slice(0, -3) + (p === undefined ? '' : p);
    cases[key] = { rule, limit0: await run(file, ['--stack-trace-limit=0'], p), frames: await run(file, [], p) };
  }
}
fs.writeFileSync(OUT, JSON.stringify({ node: process.version, cases }, null, 2) + '\n');
console.log(`wrote ${OUT}: ${Object.keys(cases).length} cases`);
for (const [k, c] of Object.entries(cases)) console.log(`  ${k}: ${c.rule}, exit ${c.limit0.exit}${c.limit0.client ? `, client ${c.limit0.client}` : ''}`);
