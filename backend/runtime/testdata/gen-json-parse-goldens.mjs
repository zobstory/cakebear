// gen-json-parse-goldens.mjs: what Node 24.18's JSON.parse makes of a fixed,
// deterministic set of inputs (S1 plan D3), so the runtime's json_parse.go
// is held to it. Each line is {"in":…,"inspect":…,"json":…} for a value,
// util.inspect of it and JSON.stringify of it, or {"in":…,"err":…} for the
// SyntaxError's message. A string holding a lone surrogate is written as its
// UTF-16 code units, {"u":[…]}, as golden-codec.mjs writes them.
//
// The inputs are the lead's spike's (spikes/s1-go/json): s1-node's cases,
// seeds of every kind, every prefix of each, and each position deleted or
// replaced by one of 19 characters.
//
//   node gen-json-parse-goldens.mjs > json-parse-goldens.jsonl
import util from 'node:util';


const seeds = [
  '{"name":"cakebear","n":42,"list":[1,"two",null,true],"nested":{"ok":false}}',
  '[1, 2.5, -0, 1e400, -1e-400, 5e-324, 9007199254740993, 12345678901234567890]',
  '{"b":1,"2":2,"a":3,"1":4,"-1":5,"01":6,"4294967294":7,"4294967295":8,"4294967296":9,"0":10}',
  '{"a":1,"b":2,"a":3}',
  '{"__proto__":{"polluted":true},"x":1}',
  '"A\\u00e9 \\ud83d\\ude00 \\ud83d x \\ude00 \\" \\\\ \\/ \\b \\f \\n \\r \\t"',
  '{\n  "a": [\r\n    1,\r    "x"\n  ],\n\t"b": {}\n}',
  '"héllo \u{1F600} 日本"',
  'true', 'false', 'null', '0', '-0', '1.0', '1.50', '1E2', '1e+2', '1e-2', '""', '[]', '{}',
  '[[[[[[[[[[1]]]]]]]]]]',
  '{"k":{"k":{"k":{"k":"v"}}}}',
];

// Hand-picked failures, s1-node's R29 among them.
const failures = [
  '', ' ', '[', '{', '{"a"', '{"a":', '{"a":1', '{"a":1,', '{"a":1,}', '{a:1}', "{'a':1}",
  '[1 2]', '[1,]', '[,1]', '"unterminated', '"bad \\x"', '"\\u12 "', '"\\u12', '"\\', '"tab\there"',
  '"nl\nhere"', '-', '-Infinity', '1.', '1e', '1e+', '01', '-01', '00', '{} x', '1 2', '0x10',
  'null x', '.5', '+1', "'str'", 'True', 'nan', 'tru', 'nul', 'fals', '<html>oops</html>',
  '{"a": tru}', '{"a":undefined}', 'undefined', 'NaN', 'Infinity', '[object Object]',
  ' undefined', 'NaN ', ' {}', ' {}', '﻿{}', '"\u{1F600}"', '\u{1F600}', '[1,\n"é\u{1F600}" 3]',
  '[\r1 2]', '[\n\r1 2]', '[\r\n\r\n1 2]', 'tr1e', 'nu"ll', 'n-ull', '[1,2,3,4,5,6,7,8,9,10,11 12]',
  '{"aaaaaaaaaa":1,"bbbbbbbbbb":2,,}', '[1,2,3,4,5,6,7,8,9,]', '["é\u{1F600}日", 1 2]',
  '[\t1,\t2 3]', '{"a":1 "b":2}', '["a" "b"]', '{"a" "b"}', '{"a":1}}', '[1]]', '{"a":[}', '[{]',
  '{"\u{1F600}\u{1F600}\u{1F600}\u{1F600}\u{1F600}\u{1F600}": x}', '"\u0000"', '"\u001f"', '"\u007f"',
];

// Mutations of the seeds: every prefix, and each position with a character
// deleted or replaced by one from this set. Deterministic.
const subs = ['', ',', ':', ']', '}', '[', '{', '"', '\\', '0', '1', '-', '.', 'e', 'x', ' ', '\n', 'é', '\u{1F600}'];

const inputs = new Set([...seeds, ...failures]);
for (const s of seeds) {
  const units = s.length;
  for (let i = 0; i <= units; i++) inputs.add(s.slice(0, i));
  for (let i = 0; i < units; i++) {
    for (const c of subs) inputs.add(s.slice(0, i) + c + s.slice(i + 1));
  }
}

const wellFormed = (s) => s.isWellFormed();
const enc = (s) => (wellFormed(s) ? s : { u: Array.from({ length: s.length }, (_, i) => s.charCodeAt(i)) });

for (const s of inputs) {
  let line;
  try {
    const v = JSON.parse(s);
    line = { in: enc(s), inspect: enc(util.inspect(v)), json: enc(JSON.stringify(v)) };
  } catch (e) {
    if (!(e instanceof SyntaxError)) throw e;
    line = { in: enc(s), err: enc(e.message) };
  }
  process.stdout.write(JSON.stringify(line) + '\n');
}
