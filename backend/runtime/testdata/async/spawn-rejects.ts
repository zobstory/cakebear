// rule: ordering
// shim
// A spawned function's throw, and an async one's rejection, reject the
// spawn's promise in its caller, whose await throws it again. Under Node the
// shim runs fn on the same thread: `spawn = (fn) => Promise.resolve().then(fn)`.
export {};
function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
try {
  const n: number = await spawn((): number => {
    throw new RangeError("in a spawn");
  });
  console.log("never", n);
} catch (e) {
  console.log("spawn", String(e));
}
try {
  await spawn(async (): Promise<number> => {
    await delay(1);
    throw new Error("in an async spawn");
  });
} catch (e) {
  console.log("async spawn", String(e));
}
try {
  await spawn(async (): Promise<number> => {
    await delay(1);
    throw { code: 7, list: [1, 2] };
  });
} catch (e) {
  console.log("an object", e);
}
try {
  await spawn((): Promise<number> => new Promise((_resolve, reject) => setTimeout(() => reject("later"), 1)));
} catch (e) {
  console.log("a rejection", e);
}
console.log("end");
