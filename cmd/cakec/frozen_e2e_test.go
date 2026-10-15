package main

import (
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// routeTableProgram is a server whose handlers read a route table declared
// at the top level: a frozen const (M4 plan D10b), readonly all the way down
// and built from literals, which the race rule lets every request read. Its
// 404 sets no Content-Type, and none is sent, as under Node.
const routeTableProgram = `import { createServer } from "node:http";
interface Page { readonly title: string; readonly items: readonly string[] }
interface Route { readonly path: string; readonly page: Page }
const home: Page = { title: "home", items: ["tea", "cake"] };
const routes: readonly Route[] = [
  { path: "/", page: home },
  { path: "/about", page: { title: "about", items: [] } },
];
function find(url: string): number {
  let i: number = 0;
  for (const { path } of routes) {
    if (path === url) {
      return i;
    }
    i++;
  }
  return -1;
}
createServer((req, res) => {
  const i: number = find(req.url);
  if (i < 0) {
    res.writeHead(404);
    res.end("not found\n");
    return;
  }
  res.setHeader("content-type", "application/json");
  res.end(JSON.stringify(routes[i].page) + "\n");
}).listen(3000, () => {
  console.log("listening");
});
`

// TestFrozenRouteTableServes: each route answers as Node 24.18 answered the
// same file: status, Content-Type and body.
func TestFrozenRouteTableServes(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sends SIGINT, which a Windows process cannot receive")
	}

	port := strconv.Itoa(freePort(t))
	server := startServer(t, buildProgram(t, "routes", strings.ReplaceAll(routeTableProgram, "3000", port)))
	select {
	case line := <-server.lines:
		if line != "listening" {
			t.Fatalf("first line = %q, want %q", line, "listening")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("not listening within 10s")
	}
	for _, tt := range []struct {
		path        string
		status      int
		contentType string
		body        string
	}{
		{"/", 200, "application/json", "{\"title\":\"home\",\"items\":[\"tea\",\"cake\"]}\n"},
		{"/about", 200, "application/json", "{\"title\":\"about\",\"items\":[]}\n"},
		{"/missing", 404, "", "not found\n"},
	} {
		status, typ, body := get(t, "http://127.0.0.1:"+port+tt.path)
		if status != tt.status || typ != tt.contentType || body != tt.body {
			t.Errorf("GET %s = %d %q %q, want %d %q %q", tt.path, status, typ, body, tt.status, tt.contentType, tt.body)
		}
	}
	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
}
