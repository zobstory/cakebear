package main

import "testing"

// errorsProgram covers M6 step 3, throw and try lowered and emitted:
//   - each built-in kind thrown and caught, narrowed by instanceof, and
//     return from a try and from its catch, with a finally after both;
//   - a finally on a return, and on a loop's break and continue, a break
//     of a loop inside the try, a return through two finallys, and a
//     finally's return swallowing a throw;
//   - a string, a number, an Error and null thrown, caught as an unknown,
//     and tested with String, typeof, ===, instanceof;
//   - an Error printed, converted and serialised, a rethrow through a nested
//     try's finally, catch {} of a builtin's URIError, and RangeError's
//     message from repeat;
//   - an unknown parameter narrowed four ways, a result-style
//     `{ error: Error | null }`, and await inside a try in an async function;
//   - a closure's own return inside a try's block, and the truthiness of
//     unknowns.
//
// Node 24.18 ran this same file under --stack-trace-limit=0, which is how
// cakebear prints an Error (M6 plan D4), and printed nodeErrors, the same in
// 3 of 3 runs.
const errorsProgram = `export {};

function divide(a: number, b: number): number {
  if (b === 0) {
    throw new RangeError("division by zero");
  }
  return a / b;
}

function attempt(b: number): string {
  try {
    return ` + "`" + `ok ${divide(1, b)}` + "`" + `;
  } catch (e) {
    if (e instanceof RangeError) {
      return ` + "`" + `range: ${e.message}` + "`" + `;
    }
    return "other";
  } finally {
    console.log("finally", b);
  }
}
console.log(attempt(2));
console.log(attempt(0));

function override(): number {
  try {
    return 1;
  } finally {
    console.log("in finally");
  }
}
console.log(override());

for (let i = 0; i < 6; i++) {
  try {
    if (i === 1) {
      continue;
    }
    if (i === 3) {
      break;
    }
    console.log("body", i);
  } finally {
    console.log("loop finally", i);
  }
}

function thrower(k: number): void {
  if (k === 0) {
    throw "a string";
  }
  if (k === 1) {
    throw 42;
  }
  if (k === 2) {
    throw new TypeError("typed");
  }
  if (k === 3) {
    throw null;
  }
}
for (let k = 0; k < 5; k++) {
  try {
    thrower(k);
    console.log("no throw", k);
  } catch (e) {
    console.log("caught", String(e), typeof e === "string", e === 42, e === null, e instanceof Error);
  }
}

const err: Error = new Error("printed");
console.log(err);
console.log("strings", String(new URIError("u")), err.name, err.message);
console.log(JSON.stringify({ e: err }));

try {
  try {
    throw new Error("inner");
  } catch (e) {
    console.log("caught inner");
    throw e;
  } finally {
    console.log("inner finally");
  }
} catch (e) {
  console.log("outer", e instanceof Error ? e.message : "?");
}

try {
  decodeURIComponent("%");
} catch {
  console.log("bad uri");
}
try {
  "ab".repeat(-1);
} catch (e) {
  console.log(String(e));
}

for (let i = 0; i < 2; i++) {
  try {
    for (let j = 0; j < 3; j++) {
      if (j === 1) {
        break;
      }
      console.log("inner", i, j);
    }
  } finally {
    console.log("outer finally", i);
  }
}

function nested(): string {
  try {
    try {
      return "deep";
    } finally {
      console.log("f1");
    }
  } finally {
    console.log("f2");
  }
}
console.log(nested());

function swallow(): string {
  try {
    throw new Error("lost");
  } finally {
    return "swallowed";
  }
}
console.log(swallow());

function describe(x: unknown): string {
  if (x instanceof Error) {
    return ` + "`" + `error ${x.message}` + "`" + `;
  }
  if (typeof x === "number") {
    return ` + "`" + `number ${x + 1}` + "`" + `;
  }
  if (x === undefined) {
    return "undefined";
  }
  return String(x);
}
interface Result {
  value: number;
  error: Error | null;
}
function safe(n: number): Result {
  try {
    return { value: divide(10, n), error: null };
  } catch (e) {
    return { value: 0, error: e instanceof Error ? e : new Error(String(e)) };
  }
}
for (const n of [5, 0]) {
  const r: Result = safe(n);
  console.log("safe", r.value, r.error === null ? "no error" : r.error.message, describe(r.error), describe(n), describe(undefined));
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}
async function later(): Promise<string> {
  try {
    await sleep(1);
    throw new Error("after await");
  } catch (e) {
    return e instanceof Error ? e.message : "?";
  } finally {
    console.log("async finally");
  }
}

function withClosure(): number {
  try {
    const twice = (n: number): number => {
      return n * 2;
    };
    return twice(21);
  } finally {
    console.log("closure finally");
  }
}
console.log(withClosure());

const flags: unknown[] = [0, "", "x", null, undefined, new Error("e"), 2];
console.log("truthy", flags.map((f: unknown): boolean => !!f));
console.log(await later());
`

const nodeErrors = `finally 2
ok 0.5
finally 0
range: division by zero
in finally
1
body 0
loop finally 0
loop finally 1
body 2
loop finally 2
loop finally 3
caught a string true false false false
caught 42 false true false false
caught TypeError: typed false false false true
caught null false false true false
no throw 4
[Error: printed]
strings URIError: u Error printed
{"e":{}}
caught inner
inner finally
outer inner
bad uri
RangeError: Invalid count value: -1
inner 0 0
outer finally 0
inner 1 0
outer finally 1
f1
f2
deep
swallowed
safe 2 no error null number 6 undefined
safe 0 division by zero error division by zero number 1 undefined
closure finally
42
truthy [
  false, false,
  true,  false,
  false, true,
  true
]
async finally
after await
`

func TestErrorsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "errors", errorsProgram), nodeErrors, 20)
}

// TestUncaughtThrowsEndAsNode: a throw nothing catches, at the top level, in
// a function, rethrown from a catch, and in a timer, ends the program as it
// ends Node's: the output so far, the report, exit 1. Each body is what Node
// 24.18 printed for the same file under --stack-trace-limit=0.
func TestUncaughtThrowsEndAsNode(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, src, stdout, body string }{
		{"top", "export {};\nconsole.log(\"before\");\nthrow new TypeError(\"boom\");\n", "before\n", "[TypeError: boom]"},
		{"function", "export {};\nfunction fail(m: string): void {\n  throw new RangeError(m);\n}\nconsole.log(\"before\");\nfail(\"deep\");\nconsole.log(\"never\");\n",
			"before\n", "[RangeError: deep]"},
		{"rethrow", "export {};\nconsole.log(\"before\");\ntry {\n  throw new Error(\"first\");\n} catch (e) {\n  throw e;\n}\n", "before\n", "[Error: first]"},
		{"timer", "export {};\nsetTimeout(() => {\n  console.log(\"in timer\");\n  throw new URIError(\"late\");\n}, 1);\nconsole.log(\"before\");\n",
			"before\nin timer\n", "[URIError: late]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkUncaught(t, buildProgram(t, "uncaught_"+tt.name, tt.src), tt.stdout, tt.body)
		})
	}
}
