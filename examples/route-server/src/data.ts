// The menu the server answers from. Both consts are frozen (M4 step 10):
// readonly all the way down, and built from literals, so every request may
// read them at once, in parallel, without a race.

export interface Item {
  readonly id: number;
  readonly name: string;
  readonly price: number;
  readonly tags: readonly string[];
}

export const items: readonly Item[] = [
  { id: 1, name: "Green tea", price: 4.5, tags: ["hot", "tea"] },
  { id: 2, name: "Straße cake", price: 2.25, tags: ["sweet"] },
  { id: 3, name: "Iced coffee", price: 3.75, tags: ["cold", "coffee"] },
  { id: 4, name: "Chai 😀", price: 4, tags: ["hot", "tea", "spiced"] },
];

export const service: { readonly name: string; readonly version: string } = {
  name: "cakebear menu",
  version: "1.0",
};
