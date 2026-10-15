// The value encoding shared by gen-inspect-goldens.mjs, verify-goldens.mjs,
// check-console-log.mjs and gen-json-goldens.mjs. See the contract in
// gen-inspect-goldens.mjs. One form is the JSON corpus's alone, a string
// holding a lone surrogate, as its UTF-16 code units: {"u":[55296]}.

import util from 'node:util';

export function numText(x) {
  return Object.is(x, -0) ? '-0' : String(x);
}

export function codeUnits(s) {
  return Array.from({ length: s.length }, (_, i) => s.charCodeAt(i));
}

// decodeArgs rebuilds the JS values of one entry's args. A container is
// created and registered before its children are decoded, so a ref may name
// an ancestor (a cycle) or anything earlier in the pre-order walk.
export function decodeArgs(args) {
  const reg = new Map();
  const register = (d, v) => {
    if (!('id' in d)) return;
    if (!Number.isInteger(d.id)) throw new Error(`bad id ${d.id}`);
    if (reg.has(d.id)) throw new Error(`duplicate id ${d.id}`);
    reg.set(d.id, v);
  };
  const dec = (d) => {
    if (d === null || typeof d !== 'object' || Array.isArray(d)) {
      throw new Error(`bad description ${JSON.stringify(d)}`);
    }
    const keys = Object.keys(d);
    if ('n' in d) {
      if (keys.length !== 1 || typeof d.n !== 'string') throw new Error('bad number');
      const x = Number(d.n);
      if (numText(x) !== d.n) throw new Error(`non-canonical number text ${d.n}`);
      return x;
    }
    if ('s' in d) {
      if (keys.length !== 1 || typeof d.s !== 'string') throw new Error('bad string');
      return d.s;
    }
    if ('u' in d) {
      if (keys.length !== 1 || !Array.isArray(d.u) || d.u.some((c) => !Number.isInteger(c) || c < 0 || c > 0xffff)) {
        throw new Error('bad code units');
      }
      const s = String.fromCharCode(...d.u);
      if (s.isWellFormed()) throw new Error('a well-formed string as code units');
      return s;
    }
    if ('b' in d) {
      if (keys.length !== 1 || typeof d.b !== 'boolean') throw new Error('bad boolean');
      return d.b;
    }
    if ('ref' in d) {
      if (keys.length !== 1) throw new Error('bad ref');
      if (!reg.has(d.ref)) throw new Error(`ref ${d.ref} names no earlier container`);
      return reg.get(d.ref);
    }
    if ('a' in d) {
      if (!Array.isArray(d.a) || keys.some((k) => k !== 'a' && k !== 'id')) throw new Error('bad array');
      const arr = [];
      register(d, arr);
      for (const e of d.a) arr.push(dec(e));
      return arr;
    }
    if ('o' in d) {
      if (!Array.isArray(d.o) || keys.some((k) => k !== 'o' && k !== 'id')) throw new Error('bad object');
      const obj = {};
      register(d, obj);
      for (const pair of d.o) {
        if (!Array.isArray(pair) || pair.length !== 2 || typeof pair[0] !== 'string') {
          throw new Error('bad object entry');
        }
        const [k, v] = pair;
        if (k === '__proto__') throw new Error('__proto__ key');
        if (Object.hasOwn(obj, k)) throw new Error(`duplicate key ${JSON.stringify(k)}`);
        obj[k] = dec(v);
      }
      return obj;
    }
    throw new Error(`unknown description ${JSON.stringify(d)}`);
  };
  return args.map(dec);
}

// encodeArgs is decodeArgs' inverse: ids only on containers reached more
// than once, numbered 1.. in pre-order. A generated description must
// survive decode-then-encode unchanged, byte for byte.
export function encodeArgs(values) {
  const seen = new Set();
  const multi = new Set();
  const children = (v) => (Array.isArray(v) ? v : Object.keys(v).map((k) => v[k]));
  const walk = (v) => {
    if (typeof v !== 'object' || v === null) return;
    if (seen.has(v)) { multi.add(v); return; }
    seen.add(v);
    for (const c of children(v)) walk(c);
  };
  values.forEach(walk);
  const ids = new Map();
  let next = 1;
  const enc = (v) => {
    switch (typeof v) {
      case 'number': return { n: numText(v) };
      case 'string': return v.isWellFormed() ? { s: v } : { u: codeUnits(v) };
      case 'boolean': return { b: v };
      case 'object': {
        if (v === null) throw new Error('null is outside the subset');
        if (ids.has(v)) {
          if (ids.get(v) === undefined) throw new Error('second visit of an unshared container');
          return { ref: ids.get(v) };
        }
        let id;
        if (multi.has(v)) { id = next++; ids.set(v, id); } else { ids.set(v, undefined); }
        let r;
        if (Array.isArray(v)) {
          r = { a: v.map(enc) };
        } else {
          r = { o: Object.keys(v).map((k) => [k, enc(v[k])]) };
        }
        if (id !== undefined) r.id = id;
        return r;
      }
      default:
        throw new Error(`${typeof v} is outside the subset`);
    }
  };
  // An undefined id marks "visited, not shared": walk put every container
  // reached twice in multi, so a second visit of one is a bug.
  return values.map(enc);
}

// The subset's constraints, checked on every entry.

const isIntegerLike = (k) => /^(0|[1-9][0-9]*)$/.test(k) && Number(k) <= 4294967294;

// forJSON is the JSON corpus's contract: args is one value, which may hold
// a lone surrogate, in the {"u":…} form and never in a key, and whose
// strings may hold '%', since no util.format reads them.
export function validate(args, { forJSON = false } = {}) {
  const kindOf = new Map();
  const keySetOf = new Map();
  const keySet = (d) => d.o.map((p) => p[0]).sort().join('\u0000');
  const walk = (d) => {
    if ('n' in d) return 'n';
    if ('b' in d) return 'b';
    if ('s' in d) {
      if (!d.s.isWellFormed()) throw new Error('lone surrogate in a string');
      return 's';
    }
    if ('u' in d) {
      if (!forJSON) throw new Error('lone surrogate in a string');
      return 's';
    }
    if ('ref' in d) {
      if (!kindOf.has(d.ref)) throw new Error(`ref ${d.ref} to nothing earlier`);
      return kindOf.get(d.ref);
    }
    if ('a' in d) {
      if ('id' in d) kindOf.set(d.id, 'a');
      const kinds = new Set(d.a.map(walk));
      if (kinds.size > 1) throw new Error(`mixed array of ${[...kinds]}`);
      if (kinds.has('o')) {
        const sets = new Set(d.a.map((e) => ('ref' in e ? keySetOf.get(e.ref) : keySet(e))));
        if (sets.size > 1) throw new Error('array of objects with different key sets');
      }
      return 'a';
    }
    if ('o' in d) {
      if ('id' in d) { kindOf.set(d.id, 'o'); keySetOf.set(d.id, keySet(d)); }
      const seen = new Set();
      for (const [k, val] of d.o) {
        if (isIntegerLike(k)) throw new Error(`integer-like key ${k}`);
        if (k === '__proto__') throw new Error('__proto__ key');
        if (!k.isWellFormed()) throw new Error('lone surrogate in a key');
        if (seen.has(k)) throw new Error(`duplicate key ${k}`);
        seen.add(k);
        walk(val);
      }
      return 'o';
    }
    throw new Error('unknown description');
  };
  args.forEach(walk);
  if (forJSON) {
    if (args.length !== 1) throw new Error('JSON.stringify of several values');
  } else if ('s' in args[0] && args[0].s.includes('%')) {
    throw new Error("a first string argument holds '%'");
  }
}

// checkEntry holds one entry to the contract and to Node, and returns its
// output: util.format of the rebuilt values.
export function checkEntry(args) {
  validate(args);
  const values = decodeArgs(args);
  const again = JSON.stringify(encodeArgs(values));
  if (again !== JSON.stringify(args)) throw new Error(`not canonical:\n${JSON.stringify(args)}\n${again}`);
  const out = util.format(...values);
  if (!out.isWellFormed()) throw new Error('lone surrogate in the output');
  return out;
}
