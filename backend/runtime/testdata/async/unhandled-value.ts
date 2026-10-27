// rule: rejection-value
// variants: string empty number negzero nan boolean undefined null object array multiline surrogate thrown
// R13: a rejection nothing handles whose reason isn't an Error, which Node
// reports as an UnhandledPromiseRejection naming the reason. Each variant
// rejects with one reason, with no await and no timer.
export {};
function rejectWith(reason: unknown): Promise<number> {
  return new Promise((_resolve, reject) => reject(reason));
}
async function throwIt(reason: unknown): Promise<number> {
  throw reason;
}
const v = process.env.VARIANT;
console.log("sync");
if (v === "string") rejectWith("m");
if (v === "empty") rejectWith("");
if (v === "number") rejectWith(5);
if (v === "negzero") rejectWith(-0);
if (v === "nan") rejectWith(NaN);
if (v === "boolean") rejectWith(false);
if (v === "undefined") rejectWith(undefined);
if (v === "null") rejectWith(null);
if (v === "object") rejectWith({ a: 1, b: [2] });
if (v === "array") rejectWith([1, "two"]);
if (v === "multiline") rejectWith('a "quoted"\nsecond line');
if (v === "surrogate") rejectWith("\uD800 lone");
if (v === "thrown") throwIt("thrown from an async function");
