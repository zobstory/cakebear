// rule: header-from-frames
// R9, m6-node's c2-04 in the subset: a rejection nothing handles ends the
// program once the microtasks have drained, every chain run first, and
// before the timer.
export {};
function tick(): Promise<void> {
  return new Promise((resolve) => resolve());
}
async function chain(name: string): Promise<void> {
  for (let i = 1; i <= 4; i++) {
    await tick();
    console.log(name, i);
  }
}
async function fail(): Promise<void> {
  console.log("fail start");
  throw new Error("from async");
}
setTimeout(() => console.log("timer, never"), 0);
console.log("before call");
fail();
console.log("after call");
chain("then");
console.log("sync end");
