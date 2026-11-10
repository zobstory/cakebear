package main

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// M7 done-check 10: examples/workspace, a server in a workspace. The app
// imports its own modules through an `@/` alias and a package linked through
// node_modules, whose barrel re-exports a frozen const and a default export.
// It serves what Node serves from the same sources (workspaceReplies).

// workspaceServer builds a copy of examples/workspace, its node_modules link
// made again, on a free port, and starts it.
func workspaceServer(t *testing.T, race bool) (*runningServer, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sends SIGINT, which a Windows process cannot receive")
	}
	example := filepath.Join("..", "..", "examples", "workspace")
	project, links := map[string]string{}, map[string]string{}
	err := filepath.WalkDir(example, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(example, path)
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir() && rel == "bin":
			return filepath.SkipDir
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			links[rel] = filepath.ToSlash(target)
			return err
		case d.Type().IsRegular():
			src, err := os.ReadFile(path)
			project[rel] = string(src)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("examples/workspace has %d symlinks, want its one node_modules link: %v", len(links), links)
	}
	port := strconv.Itoa(freePort(t))
	if !strings.Contains(project["app/main.ts"], "const port: number = 3000;") {
		t.Fatal("app/main.ts no longer declares its port as the test expects")
	}
	project["app/main.ts"] = strings.ReplaceAll(project["app/main.ts"], "const port: number = 3000;", "const port: number = "+port+";")
	dir := filepath.Join(t.TempDir(), "workspace")
	writeProject(t, dir, project)
	writeLinks(t, dir, links)
	if code, stderr := projectBuild(t, dir, buildOptions{race: race}); code != exitOK {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	bin := filepath.Join(dir, "bin", "server")
	if race {
		if info, err := exec.Command("go", "version", "-m", bin).Output(); err != nil || !strings.Contains(string(info), "-race=true") {
			t.Fatalf("the server wasn't built with the race detector (%v):\n%s", err, info)
		}
	}
	server := startServer(t, bin)
	server.waitForLine(t, "listening on http://localhost:"+port, 30*time.Second)
	return server, "http://127.0.0.1:" + port
}

// TestWorkspaceServerServes: each request gets Node's status, Content-Type
// and body.
func TestWorkspaceServerServes(t *testing.T) {
	t.Parallel()

	server, base := workspaceServer(t, false)
	client := &http.Client{Timeout: 10 * time.Second}
	for _, want := range workspaceReplies {
		status, typ, body, err := request(client, http.MethodGet, base+want.path)
		if err != nil {
			t.Errorf("GET %s: %v", want.path, err)
			continue
		}
		if status != want.status || typ != want.contentType || body != want.body {
			t.Errorf("GET %s = %d %q %q, want %d %q %q", want.path, status, typ, body, want.status, want.contentType, want.body)
		}
	}
	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
}

// TestWorkspaceServerUnderRace: 200 requests, 8 at a time, from a
// race-detector build, every reply Node's and no race found.
func TestWorkspaceServerUnderRace(t *testing.T) {
	t.Parallel()

	server, base := workspaceServer(t, true)
	client := &http.Client{Timeout: 30 * time.Second}
	var wg sync.WaitGroup
	errs := make(chan error, 8*25)
	for w := range 8 {
		wg.Go(func() {
			for i := range 25 {
				want := workspaceReplies[(w*25+i)%len(workspaceReplies)]
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
