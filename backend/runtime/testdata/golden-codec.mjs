// The value encoding shared by gen-inspect-goldens.mjs, verify-goldens.mjs,
// check-console-log.mjs and gen-json-goldens.mjs. See the contract in
// gen-inspect-goldens.mjs. One form is the JSON corpus's alone, a string
// holding a lone surrogate, as its UTF-16 code units: {"u":[55296]}. M5
// added one: undefined and null, as {"e":"undefined"} and {"e":"null"}, which
// the Go side reads as an empty Maybe.
//
// M6 added two more. An Error, {"err":"TypeError","m":"<message>"}, m absent
// for one with no message of its own, plus "code":"<CODE>" for one of Node's
// own, which is made by doing what makes Node throw it (nodeError), so its
// stack, toString and properties are Node's. And "any":true on an array or
// an object, whose elements or properties are unknowns (M6 plan D5), so an
// array may mix kinds. Node can't see the flag, so the canonical form leaves
// it out (stripAny). An Error is a leaf: it never has an id.
//
// Errors are printed as under --stack-trace-limit=0, which this module sets
// on import for every error made after: the flag only sets the same limit
// before any code runs.

import util from 'node:util';
import http from 'node:http';
import net from 'node:net';

Error.stackTraceLimit = 0;

export const ERROR_KINDS = ['Error', 'TypeError', 'RangeError', 'URIError', 'SyntaxError', 'ReferenceError', 'EvalError'];

// grab is what f throws.
function grab(f) {
  try {
    f();
  } catch (err) {
    return err;
  }
  throw new Error('it threw nothing');
}

function response() {
  return new http.ServerResponse({ method: 'GET', httpVersionMajor: 1, httpVersionMinor: 1, headers: {} });
}

// nodeError makes the error Node's own code throws with code and message,
// by doing what throws it: the cases of the runtime's own Node-throws sites
// (M6 plan D9, R34, R37). Its message is checked against the description.
function nodeError(code, message) {
  const quoted = (prefix) => {
    if (!message.startsWith(prefix + ' ["') || !message.endsWith('"]')) throw new Error(`bad ${code} message`);
    return message.slice(prefix.length + 3, -2);
  };
  const number = (prefix, suffix = '') => {
    if (!message.startsWith(prefix) || !message.endsWith(suffix)) throw new Error(`bad ${code} message`);
    return Number(message.slice(prefix.length, message.length - suffix.length));
  };
  let err;
  switch (code) {
    case 'ERR_HTTP_HEADERS_SENT': {
      const sent = response();
      sent.writeHead(200);
      err = grab(() => (message.startsWith('Cannot set') ? sent.setHeader('x', '1') : sent.writeHead(201)));
      break;
    }
    case 'ERR_INVALID_HTTP_TOKEN':
      err = grab(() => response().setHeader(quoted('Header name must be a valid HTTP token'), 'v'));
      break;
    case 'ERR_INVALID_CHAR':
      err = grab(() => response().setHeader(quoted('Invalid character in header content'), 'a\nb'));
      break;
    case 'ERR_HTTP_INVALID_STATUS_CODE':
      err = grab(() => response().writeHead(number('Invalid status code: ')));
      break;
    case 'ERR_SOCKET_BAD_PORT':
      err = grab(() => net.createServer().listen(number('options.port should be >= 0 and < 65536. Received type number (', ').')));
      break;
    default:
      throw new Error(`no way to make ${code}`);
  }
  if (err.code !== code || err.message !== message) throw new Error(`made ${err.code} ${JSON.stringify(err.message)}, not ${code} ${JSON.stringify(message)}`);
  return err;
}

// stripAny is a description without its any flags, the form Node can tell.
export function stripAny(desc) {
  return JSON.parse(JSON.stringify(desc, (k, v) => (k === 'any' ? undefined : v)));
}

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
    if ('e' in d) {
      if (keys.length !== 1 || (d.e !== 'undefined' && d.e !== 'null')) throw new Error('bad empty value');
      return d.e === 'null' ? null : undefined;
    }
    if ('err' in d) {
      if (!ERROR_KINDS.includes(d.err) || keys.some((k) => !['err', 'm', 'code'].includes(k))) throw new Error('bad error');
      if ('m' in d && typeof d.m !== 'string') throw new Error('bad error message');
      if ('code' in d) {
        const err = nodeError(d.code, d.m);
        if (err.name !== d.err) throw new Error(`${d.code} is a ${err.name}`);
        return err;
      }
      return 'm' in d ? new globalThis[d.err](d.m) : new globalThis[d.err]();
    }
    if ('ref' in d) {
      if (keys.length !== 1) throw new Error('bad ref');
      if (!reg.has(d.ref)) throw new Error(`ref ${d.ref} names no earlier container`);
      return reg.get(d.ref);
    }
    if ('a' in d) {
      if (!Array.isArray(d.a) || keys.some((k) => k !== 'a' && k !== 'id' && k !== 'any')) throw new Error('bad array');
      const arr = [];
      register(d, arr);
      for (const e of d.a) arr.push(dec(e));
      return arr;
    }
    if ('o' in d) {
      if (!Array.isArray(d.o) || keys.some((k) => k !== 'o' && k !== 'id' && k !== 'any')) throw new Error('bad object');
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
    if (typeof v !== 'object' || v === null || v instanceof Error) return;
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
      case 'undefined': return { e: 'undefined' };
      case 'object': {
        if (v === null) return { e: 'null' };
        if (v instanceof Error) {
          const r = { err: v.name };
          if (Object.hasOwn(v, 'message')) r.m = v.message;
          if (Object.hasOwn(v, 'code')) r.code = v.code;
          return r;
        }
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
  const objectOf = new Map();
  const walk = (d) => {
    if ('n' in d) return 'n';
    if ('b' in d) return 'b';
    if ('e' in d) return 'e';
    if ('err' in d) {
      if ('m' in d && !d.m.isWellFormed()) throw new Error('lone surrogate in a message');
      return 'err';
    }
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
      // One kind, besides undefined and null: an array of T or of maybe-T.
      // An unknown[] may hold any.
      const kinds = new Set(d.a.map(walk));
      kinds.delete('e');
      if (kinds.size > 1 && !d.any) throw new Error(`mixed array of ${[...kinds]}`);
      if (kinds.has('o') && !d.any) {
        // Objects of one shape, which may leave its optional properties
        // out: each key's values are of one kind, wherever it appears.
        const kindOfKey = new Map();
        for (const e of d.a) {
          if ('e' in e) continue;
          const o = 'ref' in e ? objectOf.get(e.ref) : e;
          for (const [k, val] of o.o) {
            const kind = 'e' in val ? 'e' : ('ref' in val ? kindOf.get(val.ref) : Object.keys(val).find((x) => x !== 'id'));
            if (kind === 'e') continue;
            const norm = kind === 'u' ? 's' : kind;
            if (kindOfKey.has(k) && kindOfKey.get(k) !== norm) throw new Error(`array of objects whose ${k} differs in kind`);
            kindOfKey.set(k, norm);
          }
        }
      }
      return 'a';
    }
    if ('o' in d) {
      if ('id' in d) { kindOf.set(d.id, 'o'); objectOf.set(d.id, d); }
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
  if (again !== JSON.stringify(stripAny(args))) throw new Error(`not canonical:\n${JSON.stringify(args)}\n${again}`);
  const out = util.format(...values);
  if (!out.isWellFormed()) throw new Error('lone surrogate in the output');
  return out;
}
