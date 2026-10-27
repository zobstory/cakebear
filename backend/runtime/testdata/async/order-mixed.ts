// rule: ordering
// Resolutions and rejections interleaved through chains of awaits, a throw
// before the first await, and executors that reject, throw, and resolve
// then throw (R15 to R17).
export {};
async function ok(v: string): Promise<string> {
  return v;
}
async function bad(v: string): Promise<string> {
  throw v;
}
async function relay(p: Promise<string>): Promise<string> {
  return await p;
}
async function relayCatch(p: Promise<string>): Promise<string> {
  try {
    return await p;
  } catch (e) {
    return "recovered " + String(e);
  }
}
async function w(name: string, p: Promise<string>): Promise<void> {
  try {
    console.log(name + " " + (await p));
  } catch (e) {
    console.log(name + " caught " + String(e));
  }
}
async function early(): Promise<string> {
  console.log("early body");
  throw "early";
}
w("a", ok("1"));
w("b", bad("2"));
w("c", relay(bad("3")));
w("d", relay(ok("4")));
w("e", relayCatch(bad("5")));
w("f", relay(relay(bad("6"))));
w("g", early());
console.log("caller continues");
setTimeout(() => console.log("timer"), 0);
const p1: Promise<string> = new Promise((resolve, reject) => {
  reject("r1");
  resolve("1");
});
const p2: Promise<string> = new Promise(() => {
  throw "t2";
});
const p3: Promise<string> = new Promise((resolve) => {
  resolve("3");
  throw "ignored";
});
w("p1", p1);
w("p2", p2);
w("p3", p3);
console.log("sync done");
