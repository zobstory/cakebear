package main

import "testing"

// orderProgram reads a binding in the same expression as a call that changes
// it, in each position an expression can: either side of an operator, call
// arguments, a parameter a closure reassigns, a string, a loop condition, and
// across an await while a timer changes the binding. Node 24.18 ran this same
// file and printed nodeOrder, the same in 5 of 5 runs. Before reads were
// ordered (backend/order.go) cakebear printed 22, 42, 3131, 6, abab, 3, 1001
// and 1000: six of eight lines wrong, with no error.
const orderProgram = "let a: number = 1;\nfunction bump(): number {\n  a = a + 10;\n  return a;\n}\nconsole.log(a + bump());\nconsole.log(bump() + a);\nfunction pair(x: number, y: number): number {\n  return x * 100 + y;\n}\nconsole.log(pair(a, bump()));\nfunction viaParam(x: number): number {\n  const set = (): number => {\n    x = 5;\n    return 1;\n  };\n  return x + set();\n}\nconsole.log(viaParam(7));\nlet s: string = \"a\";\nfunction grow(): string {\n  s = s + \"b\";\n  return s;\n}\nconsole.log(s + grow());\nlet i: number = 0;\nfunction step(): number {\n  i = i + 1;\n  return 3;\n}\nwhile (i < step()) {}\nconsole.log(i);\nconst late: Promise<number> = new Promise((resolve) => {\n  setTimeout(() => {\n    a = 1000;\n    resolve(1);\n  }, 1);\n});\nconsole.log(a + (await late));\nconsole.log(a);\nexport {};\n"

const nodeOrder = "12\n42\n2131\n8\naab\n4\n32\n1000\n"

func TestEvaluationOrderMatchesNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "order", orderProgram), nodeOrder, 20)
}
