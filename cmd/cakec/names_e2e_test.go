package main

import "testing"

// namesProgram uses names TypeScript allows and Go doesn't: `$` in every
// position, written as variables, functions, parameters, an arrow and a loop
// variable; an identifier with a combining mark, one with a zero-width
// non-joiner, and a letter-number (\u216b is Ⅻ), each written as an escape,
// which both languages accept in a name. a$$ and a_D$ are distinct
// variables that a careless escape of `$` would merge. Node 24.18 ran this
// same file and printed nodeNames, the same in 5 of 5 runs.
const namesProgram = `const $count: number = 2;
let a$$: number = 1;
let a_D$: number = 10;
function add$(a$: number, $b: number): number {
  return a$ + $b;
}
const twice$ = ($x: number): number => $x * 2;
const café: string = "decomposed";
const Ⅻ: number = 12;
const zw‌nj: string = "joined";
for (let $i: number = 0; $i < 2; $i++) {
  a$$ += $i;
}
a_D$ += a$$;
console.log(add$($count, 3));
console.log(twice$(Ⅻ));
console.log(café);
console.log(zw‌nj);
console.log(a$$);
console.log(a_D$);
`

const nodeNames = "5\n24\ndecomposed\njoined\n2\n12\n"

func TestOddNamesMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "names", namesProgram), nodeNames, 20)
}
