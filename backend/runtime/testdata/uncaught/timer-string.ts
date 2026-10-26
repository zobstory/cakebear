// rule: limit0
export {};
setTimeout(() => {
  throw "from a timer";
}, 0);
