// rule: limit0
// shim
// A throw in a spawned function's own timer, before its result, is uncaught
// there, as it is under Node, where the shim runs it on the main thread: it
// ends the program, past the try around the caller's await.
export {};
function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
try {
  await spawn(async (): Promise<number> => {
    setTimeout(() => {
      throw new Error("in a spawned timer");
    }, 1);
    await delay(50);
    return 1;
  });
  console.log("never");
} catch (e) {
  console.log("never caught", String(e));
}
