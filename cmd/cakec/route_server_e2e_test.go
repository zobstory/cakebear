package main

import (
	"fmt"
	"io"
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

// M4's check 8, zob's live test: examples/route-server, a JSON API in four
// modules, builds from its tsconfig.json and answers as Node does. It reads
// a frozen menu (step 10) in every request, parses req.url with the string
// methods and parseInt's kin (the strings step), and answers with
// JSON.stringify (step 9), objects and arrays (steps 5 to 7) throughout.

// routeReplies is what Node 24.18 answered, running the same sources, to
// each request: status, Content-Type and body.
var routeReplies = []struct {
	method, path string
	status       int
	contentType  string
	body         string
}{
	{"GET", "/", 200, "application/json", "{\"service\":\"cakebear menu\",\"version\":\"1.0\",\"items\":4}\n"},
	{"GET", "/health", 200, "text/plain", "ok\n"},
	{"GET", "/items", 200, "application/json", "[{\"id\":1,\"name\":\"Green tea\",\"price\":4.5,\"tags\":[\"hot\",\"tea\"]},{\"id\":2,\"name\":\"Stra\u00dfe cake\",\"price\":2.25,\"tags\":[\"sweet\"]},{\"id\":3,\"name\":\"Iced coffee\",\"price\":3.75,\"tags\":[\"cold\",\"coffee\"]},{\"id\":4,\"name\":\"Chai \U0001f600\",\"price\":4,\"tags\":[\"hot\",\"tea\",\"spiced\"]}]\n"},
	{"GET", "/items?tag=hot", 200, "application/json", "[{\"id\":1,\"name\":\"Green tea\",\"price\":4.5,\"tags\":[\"hot\",\"tea\"]},{\"id\":4,\"name\":\"Chai \U0001f600\",\"price\":4,\"tags\":[\"hot\",\"tea\",\"spiced\"]}]\n"},
	{"GET", "/items?q=CAKE", 200, "application/json", "[{\"id\":2,\"name\":\"Stra\u00dfe cake\",\"price\":2.25,\"tags\":[\"sweet\"]}]\n"},
	{"GET", "/items?tag=tea&q=chai", 200, "application/json", "[{\"id\":4,\"name\":\"Chai \U0001f600\",\"price\":4,\"tags\":[\"hot\",\"tea\",\"spiced\"]}]\n"},
	{"GET", "/items?q=stra%C3%9Fe", 200, "application/json", "[{\"id\":2,\"name\":\"Stra\u00dfe cake\",\"price\":2.25,\"tags\":[\"sweet\"]}]\n"},
	{"GET", "/items/2", 200, "application/json", "{\n  \"id\": 2,\n  \"name\": \"Stra\u00dfe cake\",\n  \"price\": 2.25,\n  \"tags\": [\n    \"sweet\"\n  ]\n}\n"},
	{"GET", "/items/9", 404, "application/json", "{\"error\":\"no item 9\"}\n"},
	{"GET", "/items/abc", 404, "application/json", "{\"error\":\"no item abc\"}\n"},
	{"GET", "/menu", 200, "text/plain; charset=utf-8", "Green tea      $4.50\nStra\u00dfe cake    $2.25\nIced coffee    $3.75\nChai \U0001f600        $4.00\n"},
	{"GET", "/nope", 404, "application/json", "{\"error\":\"no route /nope\"}\n"},
	{"POST", "/items", 405, "application/json", "{\"error\":\"POST is not allowed\"}\n"},
	{"DELETE", "/items/1", 405, "application/json", "{\"error\":\"DELETE is not allowed\"}\n"},
}

// routeServer builds examples/route-server on a free port, with Go's race
// detector when race is set, and starts it.
func routeServer(t *testing.T, race bool) (*runningServer, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sends SIGINT, which a Windows process cannot receive")
	}
	dir := filepath.Join(t.TempDir(), "route-server")
	example := filepath.Join("..", "..", "examples", "route-server")
	project := map[string]string{}
	for _, name := range []string{"tsconfig.json", "src/main.ts", "src/routes.ts", "src/query.ts", "src/data.ts"} {
		src, err := os.ReadFile(filepath.Join(example, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		project[name] = string(src)
	}
	port := strconv.Itoa(freePort(t))
	project["src/main.ts"] = strings.ReplaceAll(project["src/main.ts"], "const port: number = 3000;", "const port: number = "+port+";")
	writeProject(t, dir, project)
	if code, stderr := projectBuild(t, dir, buildOptions{race: race}); code != exitOK {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	bin := filepath.Join(dir, "bin", "server")
	if race {
		// A clean run proves nothing unless the detector was on: Go records
		// it in the binary's build settings.
		info, err := exec.Command("go", "version", "-m", bin).Output()
		if err != nil || !strings.Contains(string(info), "-race=true") {
			t.Fatalf("the server wasn't built with the race detector (%v):\n%s", err, info)
		}
	}
	server := startServer(t, bin)
	server.waitForLine(t, "listening on http://localhost:"+port, 30*time.Second)
	return server, "http://127.0.0.1:" + port
}

// request makes one request, returning its status, Content-Type and body.
func request(client *http.Client, method, url string) (int, string, string, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return 0, "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(b), err
}

func TestRouteServerServes(t *testing.T) {
	t.Parallel()

	server, base := routeServer(t, false)
	client := &http.Client{Timeout: 5 * time.Second}
	for _, want := range routeReplies {
		status, typ, body, err := request(client, want.method, base+want.path)
		if err != nil {
			t.Fatalf("%s %s: %v", want.method, want.path, err)
		}
		if status != want.status || typ != want.contentType || body != want.body {
			t.Errorf("%s %s = %d %q %q\nwant, as Node answered, %d %q %q", want.method, want.path, status, typ, body, want.status, want.contentType, want.body)
		}
	}
	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
}

// TestRouteServerUnderRace: built with Go's race detector, the server answers
// many requests at once, every handler reading the same frozen menu, each
// as Node answered it alone, and the detector reports nothing.
func TestRouteServerUnderRace(t *testing.T) {
	t.Parallel()

	server, base := routeServer(t, true)
	client := &http.Client{Timeout: 30 * time.Second}
	var wg sync.WaitGroup
	errs := make(chan error, 8*25)
	for w := range 8 {
		wg.Go(func() {
			for i := range 25 {
				want := routeReplies[(w*25+i)%len(routeReplies)]
				status, typ, body, err := request(client, want.method, base+want.path)
				switch {
				case err != nil:
					errs <- err
				case status != want.status || typ != want.contentType || body != want.body:
					errs <- fmt.Errorf("%s %s = %d %q %q, want %d %q %q", want.method, want.path, status, typ, body, want.status, want.contentType, want.body)
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
