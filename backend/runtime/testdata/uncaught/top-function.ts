// rule: header-from-frames
export {};
function inner(): void {
	if (true) throw new RangeError("from inner");
}
function outer(): void {
  inner();
}
outer();
console.log("not reached");
