// rule: header-from-frames
// R10, m6-node's c2-06: a rejection in a timer callback ends the program
// after that callback's microtasks, before the next timer due in the same
// tick.
export {};
function tick(): Promise<void> {
  return new Promise((resolve) => resolve());
}
async function fails(): Promise<void> {
  throw new Error("rejected in timer 1");
}
async function after(): Promise<void> {
  await tick();
  console.log("microtask after the rejection");
}
setTimeout(() => {
  console.log("timer 1");
  fails();
  after();
}, 0);
setTimeout(() => console.log("timer 2, same delay"), 0);
setTimeout(() => console.log("timer 3, later"), 20);
