// rule: throw-site
// R14, m6-node's c2-09: a top-level await of a promise rejected with a
// string is an uncaught throw of the string, not R13's report. reject has no
// site, so cakebear's report has no excerpt.
export {};
function tick(): Promise<void> {
  return new Promise((resolve) => resolve());
}
async function hops(name: string): Promise<void> {
  for (let i = 1; i <= 8; i++) {
    await tick();
    console.log(name, "hop", i);
  }
}
hops("a");
const p: Promise<void> = new Promise((_resolve, reject) => reject("a string reason"));
console.log("awaiting a rejected promise");
await p;
console.log("never");
