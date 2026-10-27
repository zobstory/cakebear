// rule: header-from-frames
// m6-node's c2-05: the same for a throw after an await.
export {};
function tick(): Promise<void> {
  return new Promise((resolve) => resolve());
}
async function g(): Promise<void> {
  console.log("g start");
  await tick();
  console.log("g resumed");
  throw new Error("after an await");
}
async function chain(name: string): Promise<void> {
  for (let i = 1; i <= 4; i++) {
    await tick();
    console.log(name, i);
  }
}
g();
console.log("after call");
chain("then");
setTimeout(() => console.log("timeout 0"), 0);
