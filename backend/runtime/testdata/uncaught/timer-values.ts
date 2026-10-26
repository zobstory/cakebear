// rule: limit0
export {};
interface Box { a: number; list: unknown[] }
const box: Box = { a: 1, list: [undefined, null, "s"] };
setTimeout(() => {
  throw box;
}, 0);
