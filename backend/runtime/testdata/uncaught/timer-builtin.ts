// rule: limit0
export {};
interface Link { next: Link[] }
const a: Link = { next: [] };
a.next.push(a);
setTimeout(() => {
  console.log(JSON.stringify(a));
}, 0);
