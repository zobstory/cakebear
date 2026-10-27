// rule: header-from-frames
// No await and no timer, so the generated main never runs the loop: the
// check comes from Exit, after the top-level code.
export {};
async function f(): Promise<void> {
  throw new RangeError("nothing awaits this");
}
f();
console.log("after the call");
