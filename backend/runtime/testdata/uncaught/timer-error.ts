// rule: limit0
export {};
const e: Error = new Error("made at the top");
console.log("sync");
setTimeout(() => {
  console.log("timer 1");
  throw e;
}, 0);
setTimeout(() => console.log("timer 2, never"), 0);
