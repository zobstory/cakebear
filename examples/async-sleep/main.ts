// async/await over a timer. It runs unmodified under Node, and cakebear
// compiles it to a native binary that prints the same lines in the same order:
//
//   cakec build examples/async-sleep/main.ts -o sleep && ./sleep
//
// The part of an async function before its first `await` runs synchronously,
// so "a" prints before "c"; the rest resumes once the timer fires: a, c, b.

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

async function main(): Promise<void> {
  console.log("a");
  await sleep(20);
  console.log("b");
}

main();
console.log("c");
