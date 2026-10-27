// rule: header-from-frames
// R14, m6-node's c2-08: an Error a top-level await rejects with is reported
// where it was made; the timer never runs.
export {};
console.log("before await");
setTimeout(() => console.log("timer"), 0);
await new Promise<void>((_resolve, reject) => reject(new Error("awaited rejection")));
console.log("not reached");
