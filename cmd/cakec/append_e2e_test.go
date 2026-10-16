package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// appendProgram appends to strings every way the subset can (M5 plan D13):
// a template in a loop that reads the string's length as it goes,
// `s = s + a + b`, a parameter, a closure appending to a top-level let, an
// alias kept across an append, a surrogate pair split across one, a binding
// reset in a loop, and `order += bump()`, where bump appends to order too and
// JavaScript discards that append, having read order first. Node 24.18 ran
// it and printed nodeAppend.
const appendProgram = "export {};\nlet xml: string = \"<urlset>\";\nfor (let i: number = 0; i < 300; i++) {\n  xml += `<url><loc>https://example.com/p/${i}</loc></url>`;\n  if (xml.length % 1000 === 0) {\n    console.log(\"at\", xml.length);\n  }\n}\nxml += \"</urlset>\";\nconsole.log(xml.length, xml.slice(0, 40), xml.slice(-30));\nfunction build(n: number, sep: string): string {\n  let out: string = \"\";\n  for (let i: number = 0; i < n; i++) {\n    out = out + sep + String(i);\n  }\n  sep += \"!\";\n  return out + sep;\n}\nconsole.log(build(5, \",\"));\nconsole.log(build(100, \"é\").length);\nlet t: string = \"x\";\nconst add = (piece: string): void => {\n  t = `${t}${piece}-`;\n};\nfor (let i: number = 0; i < 100; i++) {\n  add(\"ab\");\n}\nconsole.log(t.length, t.slice(0, 12));\nconst kept: string = t;\nt += \"more\";\nconsole.log(kept.length, t.length, kept === t.slice(0, kept.length));\nlet seam: string = \"a\".repeat(300) + \"\\uD83D\";\nseam += \"\\uDE00\";\nconsole.log(seam.length, seam.slice(-2) === \"😀\");\nlet reset: string = \"\";\nfor (let i: number = 0; i < 3; i++) {\n  reset = \"\";\n  for (let j: number = 0; j < 200; j++) {\n    reset += \"ab\";\n  }\n}\nconsole.log(reset.length);\nlet order: string = \"a\".repeat(300);\nfunction bump(): string {\n  order += \"Z\";\n  return \"f\";\n}\norder += bump();\nconsole.log(order.length, order.slice(-3));\n"

const nodeAppend = "at 4000\n14007 <urlset><url><loc>https://example.com/p/ com/p/299</loc></url></urlset>\n,0,1,2,3,4,!\n292\n301 xab-ab-ab-ab\n301 305 true\n302 true\n400\n301 aaf\n"

func TestAppendsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "appends", appendProgram), nodeAppend, 3)
}

// prefixProgram shares a string with a spawn while its binding keeps
// appending: the const holds a prefix of the accumulator's buffer, which the
// spawn reads in parallel as main writes past its end. Node 24.18 ran it with
// the shim `spawn = fn => Promise.resolve().then(fn)` and printed the line
// below.
const prefixProgram = "let acc: string = \"a\".repeat(300);\nfor (let i: number = 0; i < 100; i++) {\n  acc += \"b\";\n}\nconst snapshot: string = acc;\nconst job: Promise<number> = spawn((): number => {\n  let n: number = 0;\n  for (let i: number = 0; i < 20000; i++) {\n    n += snapshot.length + snapshot.charCodeAt(i % 400);\n  }\n  return n;\n});\nfor (let i: number = 0; i < 20000; i++) {\n  acc += \"c\";\n}\nconsole.log(await job, acc.length, snapshot.length, snapshot === acc.slice(0, 400));\nexport {};\n"

// TestAppendedPrefixSharedWithASpawn runs prefixProgram built with the race
// detector, 10 times: an accumulator writes only past its newest string's
// end, so a prefix another domain holds is never written while it is read.
func TestAppendedPrefixSharedWithASpawn(t *testing.T) {
	t.Parallel()
	if out, err := exec.Command("go", "env", "CGO_ENABLED").Output(); err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Skip("the race detector needs cgo, which this toolchain has disabled")
	}
	path := writeTS(t, "prefix.ts", prefixProgram)
	bin := filepath.Join(filepath.Dir(path), "prefix")
	var stderr strings.Builder
	if code := runBuild(buildOptions{files: []string{path}, output: bin, race: true}, &stderr); code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, stderr.String())
	}
	if info, err := exec.Command("go", "version", "-m", bin).Output(); err != nil || !strings.Contains(string(info), "-race=true") {
		t.Fatalf("not built with the race detector (%v):\n%s", err, info)
	}
	for run := range 10 {
		var out, errs strings.Builder
		cmd := exec.Command(bin)
		cmd.Stdout, cmd.Stderr = &out, &errs
		if err := cmd.Run(); err != nil || out.String() != "9945000 20400 400 true\n" {
			t.Fatalf("run %d: %v, printed %q, stderr\n%s", run, err, out.String(), errs.String())
		}
	}
}
