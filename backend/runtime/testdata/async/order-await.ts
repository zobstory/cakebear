// rule: ordering
// m6-node's c3-02, R17: a rejection takes as many microtask hops through an
// await as a resolution, and a throw settles in the round it happens.
export {};
// Only the subset's async tools: async/await, new Promise, resolve, reject,
// setTimeout, try/catch/finally.
function tick(): Promise<void> {
  return new Promise((resolve) => resolve());
}
function rejectNow(message: string): Promise<number> {
  return new Promise((_resolve, reject) => reject(new Error(message)));
}
function resolveAfter(ms: number, value: number): Promise<number> {
  return new Promise((resolve) => setTimeout(() => resolve(value), ms));
}
function rejectAfter(ms: number, message: string): Promise<number> {
  return new Promise((_resolve, reject) => setTimeout(() => reject(new Error(message)), ms));
}
async function ok(name: string): Promise<number> {
  console.log(name, "start");
  await tick();
  console.log(name, "after await 1");
  await tick();
  console.log(name, "after await 2");
  return 1;
}
async function bad(name: string): Promise<number> {
  console.log(name, "start");
  await tick();
  console.log(name, "after await 1");
  throw new Error(name);
}
async function badSync(name: string): Promise<number> {
  console.log(name, "start");
  throw new Error(name);
}
async function run(name: string, p: Promise<number>): Promise<void> {
  try {
    const v: number = await p;
    console.log(name, "resolved", v);
  } catch (e) {
    console.log(name, "rejected", (e as Error).message);
    await tick();
    console.log(name, "after an await in catch");
  } finally {
    console.log(name, "finally");
  }
  console.log(name, "after try");
}
console.log("sync start");
setTimeout(() => console.log("timeout 0"), 0);
const runs: Promise<void>[] = [
  run("A", ok("a")),
  run("B", bad("b")),
  run("C", badSync("c")),
  run("D", rejectNow("d")),
  run("E", resolveAfter(5, 5)),
  run("F", rejectAfter(5, "f")),
  run("G", rejectAfter(0, "g")),
];
console.log("sync end");
for (const r of runs) await r;
console.log("all done");
