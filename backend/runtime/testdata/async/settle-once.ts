// rule: ordering
// m6-node's c3-04 and c3-05, R16: a promise settles once, later settlings
// and an executor's throw after one are ignored, and an executor that
// throws first rejects with what it threw.
export {};
const p = new Promise<number>((resolve, reject) => {
  resolve(1);
  reject(new Error("ignored"));
  resolve(2);
});
console.log("resolve, reject, resolve:", await p);
const q = new Promise<number>((resolve, reject) => {
  reject(new Error("first"));
  resolve(3);
  reject(new Error("second"));
});
try {
  await q;
} catch (e) {
  console.log("reject, resolve, reject:", (e as Error).message);
}
// Settling later, from timers: the first one wins.
const r = new Promise<number>((resolve, reject) => {
  setTimeout(() => resolve(4), 1);
  setTimeout(() => reject(new Error("too late")), 5);
});
console.log("timer resolve, then timer reject:", await r);
await new Promise<void>((resolve) => setTimeout(resolve, 10));
console.log("before new Promise");
const t = new Promise<number>(() => {
  console.log("executor runs synchronously");
  throw new Error("executor threw");
});
console.log("after new Promise");
try {
  await t;
} catch (e) {
  console.log("caught:", (e as Error).message);
}
const u = new Promise<number>((resolve) => {
  resolve(1);
  throw new Error("thrown after resolve");
});
console.log("resolve then throw:", await u);
const v = new Promise<number>((_resolve, reject) => {
  reject(new Error("rejected first"));
  throw new Error("thrown after reject");
});
try {
  await v;
} catch (e) {
  console.log("reject then throw:", (e as Error).message);
}
const s = new Promise<number>(() => {
  throw "a string";
});
try {
  await s;
} catch (e) {
  console.log("executor threw a string:", typeof e, e);
}
console.log("done");
