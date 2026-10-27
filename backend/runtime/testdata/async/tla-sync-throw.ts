// rule: header-from-frames
// R14: the same for a throw before the module's first await.
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
const n: number = 1;
hops("a");
console.log("throwing before any await");
if (n > 0) throw new RangeError("before the first await");
await tick();
