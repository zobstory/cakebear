package main

// S1 step 2 (plan D3, D4 (b)): JSON.parse and res.json() into an unknown,
// and the narrowing that reads what they make, held to what Node 24.18
// printed (fetch-goldens.json, through TestFetchMatchesNode, which runs
// these too). A program named uncaught-… has its whole report held to
// Node's: a parse's SyntaxError points into the data, whose report is the
// same text, but for its last line.

func init() {
	for name, src := range jsonPrograms {
		fetchPrograms[name] = src
	}
}

var jsonPrograms = map[string]string{
	// R26, R27, R28: each kind, numbers at the edges, keys' order, and
	// strings, printed as Node prints them.
	"json-values": `const inputs: string[] = ['42', '"str"', 'true', 'null', '[1, "a", null, {"k": [true]}]', '{"a":{"b":{"c":1}}}',
  ' \t\n\r 7 \n', '1e400', '-0', '9007199254740993', '{"b":1,"2":2,"a":3,"1":4,"-1":5,"01":6}', '{"a":1,"b":2,"a":3}',
  '{"__proto__":{"polluted":true},"x":1}', '"\\ud83d"', '"\\u0041\\u00e9"', '{"long":"' + "x".repeat(80) + '","n":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27]}'];
for (const s of inputs) {
  const v: unknown = JSON.parse(s);
  console.log("parsed", v, String(v));
}
export {};
`,
	// D4 (b): what JSON.parse made, read by narrowing.
	"json-narrow": `function describe(v: unknown): string {
  if (typeof v === "string") {
    return "string " + v;
  }
  if (typeof v === "number") {
    return ` + "`number ${v}`" + `;
  }
  if (typeof v === "boolean") {
    return ` + "`boolean ${v}`" + `;
  }
  if (v === null) {
    return "null";
  }
  if (Array.isArray(v)) {
    let out: string = ` + "`array of ${v.length}:`" + `;
    for (let i = 0; i < v.length; i++) {
      const item: unknown = v[i];
      out += " " + describe(item);
    }
    return out;
  }
  if (typeof v === "object") {
    let out: string = "object";
    if ("name" in v) {
      out += " name=" + describe(v.name);
    }
    if ("n" in v) {
      out += " n=" + describe(v.n);
    }
    if ("list" in v) {
      out += " list=" + describe(v.list);
    }
    if ("nested" in v && typeof v.nested === "object" && v.nested !== null && "ok" in v.nested) {
      out += " ok=" + describe(v.nested.ok);
    }
    if ("toString" in v) {
      out += " (inherits toString)";
    }
    return out;
  }
  return "undefined";
}
const data: unknown = JSON.parse('{"name":"cakebear","n":42,"list":[1,"two",null,true],"nested":{"ok":false}}');
console.log(describe(data));
console.log(describe(JSON.parse('[[], {}, "x", [[1]]]')));
const missing: unknown = JSON.parse('{"other":1}');
if (typeof missing === "object" && missing !== null && "name" in missing) {
  console.log("has a name");
} else {
  console.log("no name");
}
export {};
`,
	// R29: V8's messages.
	"json-errors": `const bad: string[] = ["{bad", '{"a":1,}', "", "[1 2]", "<html>oops</html>", "undefined", '"\\x"', "01", "1 2", "tru", '"😀', "😀"];
for (const s of bad) {
  try {
    const v: unknown = JSON.parse(s);
    console.log("parsed", v);
  } catch (e) {
    console.log("caught", String(e), e instanceof SyntaxError);
  }
}
export {};
`,
	// R5, R13: res.json(), a bad body, an empty one and a BOM.
	"json-fetch": `const res = await fetch("BASE/data?body=" + encodeURIComponent('{"n":1,"list":["a"]}'));
const v: unknown = await res.json();
console.log("json", v, res.bodyUsed);
for (const body of ["<html>oops</html>", '{"a":1,}', ""]) {
  const r = await fetch("BASE/data?body=" + encodeURIComponent(body));
  try {
    const w: unknown = await r.json();
    console.log("parsed", w);
  } catch (e) {
    console.log(String(e));
  }
}
const bom = await fetch("BASE/data?bom=1&body=" + encodeURIComponent('{"bom":true}'));
const b: unknown = await bom.json();
console.log("bom", b);
export {};
`,
	// R30: an uncaught parse error's report points into the data.
	"uncaught-json": `console.log("before");
const v: unknown = JSON.parse("{bad");
console.log("never", v);
export {};
`,
	"uncaught-json-multiline": `console.log("before");
const v: unknown = JSON.parse('{\n  "a": 1,\n  "b": ,\n  "c": 3\n}');
console.log("never", v);
export {};
`,
	"uncaught-json-eof": `console.log("before");
const v: unknown = JSON.parse('{"a":');
console.log("never", v);
export {};
`,
	"uncaught-json-rethrow": `console.log("before");
try {
  const v: unknown = JSON.parse("{bad");
  console.log("never", v);
} catch (e) {
  console.log("caught, rethrowing");
  throw e;
}
export {};
`,
	"uncaught-json-await": `async function load(): Promise<unknown> {
  await new Promise<void>((resolve) => setTimeout(resolve, 1));
  return JSON.parse("[1,\n2 3]");
}
console.log("before");
const v: unknown = await load();
console.log("never", v);
export {};
`,
	"uncaught-json-fetch": `const r = await fetch("BASE/data?body=" + encodeURIComponent("<html>oops</html>"));
console.log("before");
const v: unknown = await r.json();
console.log("never", v);
export {};
`,
}
