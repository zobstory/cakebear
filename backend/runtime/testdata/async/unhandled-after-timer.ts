// rule: header-from-frames
// R11, m6-node's c3-07b: handled only after a timer is too late.
export {};
const p = new Promise<number>((_resolve, reject) => reject(new Error("handled after a timer")));
console.log("waiting for a timer before awaiting p");
await new Promise<void>((resolve) => setTimeout(resolve, 0));
try {
  await p;
} catch (e) {
  console.log("caught:", (e as Error).message);
}
console.log("done");
