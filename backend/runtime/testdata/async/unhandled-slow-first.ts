// rule: header-from-frames
// R11, m6-node's c3-07d: start two calls, await the slow one first, and the
// fast one's rejection comes after its timer with nothing awaiting it.
export {};
function rejectAfter(ms: number, message: string): Promise<number> {
  return new Promise((_resolve, reject) => setTimeout(() => reject(new Error(message)), ms));
}
function resolveAfter(ms: number, value: number): Promise<number> {
  return new Promise((resolve) => setTimeout(() => resolve(value), ms));
}
async function main(): Promise<void> {
  const fast = rejectAfter(1, "fast one failed");
  const slow = resolveAfter(20, 2);
  try {
    console.log("slow:", await slow);
    console.log("fast:", await fast);
  } catch (e) {
    console.log("caught:", (e as Error).message);
  }
}
await main();
console.log("done");
