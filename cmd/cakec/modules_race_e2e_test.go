package main

import (
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestModulesServeUnderRace (M7 done-check 6): a server whose handler reads
// frozen consts and calls functions through a barrel, a `paths` alias, a
// workspace link and default imports serves 200 requests, 8 at a time, from
// a race-detector build, every reply Node's, with no race found.
func TestModulesServeUnderRace(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sends SIGINT, which a Windows process cannot receive")
	}

	dir := t.TempDir()
	port := strconv.Itoa(freePort(t))
	project := map[string]string{}
	for name, src := range raceModulesProject {
		project[name] = strings.ReplaceAll(src, "const port: number = 3000;", "const port: number = "+port+";")
	}
	writeProject(t, dir, project)
	writeLinks(t, dir, raceModulesLinks)
	if code, stderr := projectBuild(t, dir, buildOptions{race: true}); code != exitOK {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	bin := filepath.Join(dir, "bin", "server")
	if info, err := exec.Command("go", "version", "-m", bin).Output(); err != nil || !strings.Contains(string(info), "-race=true") {
		t.Fatalf("the server wasn't built with the race detector (%v):\n%s", err, info)
	}
	server := startServer(t, bin)
	server.waitForLine(t, "listening on http://localhost:"+port, 30*time.Second)
	base := "http://127.0.0.1:" + port

	client := &http.Client{Timeout: 30 * time.Second}
	var wg sync.WaitGroup
	errs := make(chan error, 8*25)
	for w := range 8 {
		wg.Go(func() {
			for i := range 25 {
				want := raceModulesReplies[(w*25+i)%len(raceModulesReplies)]
				status, typ, body, err := request(client, http.MethodGet, base+want.path)
				switch {
				case err != nil:
					errs <- err
				case status != want.status || typ != want.contentType || body != want.body:
					errs <- fmt.Errorf("GET %s = %d %q %q, want %d %q %q", want.path, status, typ, body, want.status, want.contentType, want.body)
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
	if strings.Contains(server.stderr.String(), "DATA RACE") {
		t.Errorf("the race detector found a race:\n%s", server.stderr.String())
	}
}

// TestModulesRaceRefusals (M7 done-check 6): a handler reaching shared state
// through each new way to name a binding is refused, naming the module that
// declares it.
func TestModulesRaceRefusals(t *testing.T) {
	t.Parallel()

	handler := func(imports, body string) string {
		return "import { createServer } from \"node:http\";\n" + imports +
			"createServer((req, res) => {\n  " + body + "\n  res.end(\"ok\");\n}).listen(3000);\n"
	}
	const counter = "let hits: number = 0;\nexport function hit(): void {\n  hits = hits + 1;\n}\n"
	workspace := func(index string) map[string]string {
		return map[string]string{
			"packages/pkg/package.json": `{ "name": "pkg", "type": "module", "exports": "./src/index.ts" }`,
			"packages/pkg/src/index.ts": index,
		}
	}
	link := map[string]string{"node_modules/pkg": "../packages/pkg"}
	for _, tt := range []struct {
		name  string
		files map[string]string
		links map[string]string
		want  string
	}{
		{
			"a let-writing call through an alias to a barrel",
			map[string]string{
				"src/lib/counter.ts": counter,
				"src/lib/index.ts":   "export * from \"./counter.ts\";\n",
				"src/main.ts":        handler("import { hit } from \"@/lib/index.ts\";\n", "hit();"),
			},
			nil, "main.ts:4:3: cannot compile yet: this handler runs on many requests at once, so it can't call `hit()`, which changes `hits` in counter.ts, shared by them all",
		},
		{
			"a mutable array through a workspace link",
			merge(workspace("export const cart: string[] = [];\n"), map[string]string{
				"src/main.ts": handler("import { cart } from \"pkg\";\n", "const n: number = cart.length;"),
			}),
			link, "so it can't use `cart` in index.ts, an array created outside it",
		},
		{
			"a let-writing default function through a workspace link",
			merge(workspace("let hits: number = 0;\nexport default function hit(): void {\n  hits = hits + 1;\n}\n"), map[string]string{
				"src/main.ts": handler("import tick from \"pkg\";\n", "tick();"),
			}),
			link, "so it can't call `tick()`, which changes `hits` in index.ts",
		},
		{
			"a default object through an alias",
			map[string]string{
				"src/lib/home.ts": "export default { path: \"/\" };\n",
				"src/main.ts":     handler("import home from \"@/lib/home.ts\";\n", "const p: string = home.path;"),
			},
			nil, "so it can't use `home` in home.ts, an object created outside it; an object can be shared between requests only if it is frozen (a const, readonly all the way down, built from literals), and this one isn't: it is the default export of an expression",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeProject(t, dir, merge(tt.files, map[string]string{"tsconfig.json": boundaryConfig(map[string][]string{"@/*": {"./src/*"}})}))
			writeLinks(t, dir, tt.links)
			code, stderr := projectBuild(t, dir, buildOptions{})
			if code != exitErrors || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit %d, want %d with\n%s\ngot:\n%s", code, exitErrors, tt.want, stderr)
			}
		})
	}
}

// merge is a and b in one map, b's entries winning.
func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
