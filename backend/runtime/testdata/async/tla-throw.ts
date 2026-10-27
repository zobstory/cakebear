// rule: header-from-frames
// R14, m6-node's c2-07: a throw after a top-level await is an uncaught
// exception, reported five microtasks after it, as Node's loader reports
// it; the timer set before it never runs.
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
console.log("before");
await tick();
hops("a");
setTimeout(() => console.log("timer, never"), 0);
console.log("throwing");
throw new Error("after a top-level await");
