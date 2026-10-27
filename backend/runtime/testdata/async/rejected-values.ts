// rule: ordering
// R18: what a catch gets through await is the rejected value itself, from
// reject and from a throw, and a rejection handled later in the same drain
// is handled (R11, m6-node's c3-07a).
export {};
const box = { a: 1 };
const list: unknown[] = [1, "two", null];
const err = new TypeError("m");
function tick(): Promise<void> {
  return new Promise((resolve) => resolve());
}
function rejectNow(reason: unknown): Promise<number> {
  return new Promise((_resolve, reject) => reject(reason));
}
async function throwIt(reason: unknown): Promise<number> {
  throw reason;
}
const reasons: unknown[] = [err, "m", 5, -0, true, box, list, undefined, null];
for (const reason of reasons) {
  try {
    await rejectNow(reason);
  } catch (e) {
    console.log("rejected", typeof e, e === reason, e);
  }
  try {
    await throwIt(reason);
  } catch (e) {
    console.log("thrown", typeof e, e === reason, e);
  }
}
const late = new Promise<number>((_resolve, reject) => reject(new Error("handled a few microtasks late")));
await tick();
await tick();
await tick();
try {
  await late;
} catch (e) {
  console.log("caught:", (e as Error).message);
}
console.log("done");
