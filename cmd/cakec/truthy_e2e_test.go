package main

import "testing"

// truthyProgram covers M5 step 7, truthiness (D7):
//   - numbers and strings tested in ?: and by !, 0, -0, NaN and "" falsy,
//     and maybes of both, null and undefined falsy;
//   - `if (u && u.name)` narrowing a maybe of an object, else if, else;
//   - || and && as values on calls, each called at most once, on a maybe
//     element, and keeping an empty object's undefined;
//   - filter, some and every given Boolean, and !! in a callback;
//   - while on a number;
//   - conditional spreads, `...(c && { … })` and `...(c ? { … } : {})`,
//     before and after a property, and of a shorthand.
//
// Node 24.18 ran this same file and printed nodeTruthy, the same in 5 of 5
// runs.
const truthyProgram = `interface User { name: string; nick?: string; age: number | null }
interface Opts { a: number; label?: string; tags?: string[] }

const vals: (number | undefined)[] = [0, -0, 0 / 0, 1, -1, undefined];
const strs: (string | null)[] = ["", "0", " ", null, "x"];
for (const v of vals) {
  console.log("number", v, v ? "truthy" : "falsy", !v);
}
for (const s of strs) {
  console.log("string", s, s ? "truthy" : "falsy", !s);
}
const users: (User | undefined)[] = [{ name: "ada", age: 36 }, undefined, { name: "", nick: "bo", age: null }];
for (const u of users) {
  if (u && u.name) {
    console.log("named", u.name, u.age || -1);
  } else if (u) {
    console.log("nameless", u.nick || "?", u.age ?? 0);
  } else {
    console.log("nobody");
  }
}

let calls: number = 0;
function count(n: number): number {
  calls += 1;
  return n;
}
const or1: number = count(0) || count(5);
const or2: number = count(3) || count(9);
const and1: number = count(0) && count(7);
const and2: number = count(2) && count(8);
console.log("logical", or1, or2, and1, and2, calls);
const pick: string = strs[1] || "default";
const fallback: string = strs[0] || "empty";
const maybeName: string | undefined = users[1] && users[1].name;
const firstName: string | undefined = users[0] && users[0].name;
console.log("values", pick, fallback, maybeName, firstName);
console.log("filter", vals.filter(Boolean), strs.filter(Boolean), vals.some(Boolean), strs.every(Boolean), vals.filter((v) => !!v).length);

let i: number = 3;
while (i) {
  i--;
}
console.log("while", i);

const flag: boolean = calls > 2;
const o: Opts = { a: 1, ...(flag && { label: "yes" }), ...(calls > 100 ? { tags: ["t"] } : {}) };
const p: Opts = { ...(calls > 100 && { label: "no" }), a: 2 };
const label: string | undefined = users[2]?.nick;
const q: Opts = { a: 3, ...(label ? { label } : {}) };
console.log("spread", o, p, q, JSON.stringify(o));
`

const nodeTruthy = `number 0 falsy true
number -0 falsy true
number NaN falsy true
number 1 truthy false
number -1 truthy false
number undefined falsy true
string  falsy true
string 0 truthy false
string   truthy false
string null falsy true
string x truthy false
named ada 36
nobody
nameless bo 0
logical 5 3 0 8 6
values 0 empty undefined ada
filter [ 1, -1 ] [ '0', ' ', 'x' ] true false 2
while 0
spread { a: 1, label: 'yes' } { a: 2 } { a: 3, label: 'bo' } {"a":1,"label":"yes"}
`

func TestTruthinessMatchesNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "truthy", truthyProgram), nodeTruthy, 20)
}
