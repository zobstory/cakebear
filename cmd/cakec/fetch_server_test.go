package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The server fetch's programs talk to, in-process, and how their goldens are
// recorded: the same programs run under Node 24.18 against this same server,
// so the two sides of a comparison only ever differ in the client.
//
//	CAKEBEAR_RECORD_FETCH=<node 24.18> go test ./cmd/cakec -run TestRecordFetchGoldens
//
// writes testdata/fetch-goldens.json, each output with the server's address
// as BASE.

func upstream(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, _ *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("X-Custom", "Mixed Case Value")
		h["X-Multi"] = []string{"one", "two"}
		h["Set-Cookie"] = []string{"a=1; Path=/", "b=2"}
		_, _ = io.WriteString(w, "hello, world")
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		names := make([]string, 0, len(r.Header))
		for name := range r.Header {
			names = append(names, strings.ToLower(name))
		}
		sort.Strings(names)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"method": r.Method, "url": r.URL.String(), "names": strings.Join(names, ","),
			"type": r.Header.Get("Content-Type"), "agent": r.Header.Get("User-Agent"),
			"custom": r.Header.Get("X-Custom"), "length": r.ContentLength, "body": string(body),
			"cache": r.Header.Get("Cache-Control") + "|" + r.Header.Get("Pragma"),
		})
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		code, _ := strconv.Atoi(r.URL.Query().Get("code"))
		w.WriteHeader(code)
		if code != http.StatusNoContent {
			fmt.Fprintf(w, "status %d", code)
		}
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		code, _ := strconv.Atoi(r.URL.Query().Get("code"))
		w.Header().Set("Location", "/echo?after="+r.URL.Query().Get("code"))
		w.WriteHeader(code)
	})
	mux.HandleFunc("/chain", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		to := "/echo"
		if n > 1 {
			to = "/chain?n=" + strconv.Itoa(n-1)
		}
		w.Header().Set("Location", to)
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		if !wait(r, ms) {
			return // the client went away: an abort
		}
		_, _ = io.WriteString(w, "slow "+strconv.Itoa(ms))
	})
	// /slowbody sends its headers at once and its body ms later.
	mux.HandleFunc("/slowbody", func(w http.ResponseWriter, r *http.Request) {
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		if wait(r, ms) {
			_, _ = io.WriteString(w, "slow body "+strconv.Itoa(ms))
		}
	})
	// /data answers its ?body as JSON: a bad one too, and a BOM before it.
	mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Has("bom") {
			_, _ = io.WriteString(w, "\uFEFF")
		}
		_, _ = io.WriteString(w, r.URL.Query().Get("body"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// wait sleeps ms, or until r's client goes away, reporting whether it slept
// the whole time: a server the test closes waits for its handlers.
func wait(r *http.Request, ms int) bool {
	select {
	case <-time.After(time.Duration(ms) * time.Millisecond):
		return true
	case <-r.Context().Done():
		return false
	}
}

const fetchGoldensPath = "testdata/fetch-goldens.json"

// fetchGolden is what Node printed for one program: stdout and stderr with
// the server's address as BASE, and the exit code.
type fetchGolden struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Exit   int    `json:"exit"`
}

func readFetchGoldens(t *testing.T) map[string]fetchGolden {
	t.Helper()
	raw, err := os.ReadFile(fetchGoldensPath)
	if err != nil {
		t.Fatalf("%v: record them with CAKEBEAR_RECORD_FETCH=<node 24.18>", err)
	}
	var g map[string]fetchGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// withBase is src with BASE spelled as base, a string literal's contents.
func withBase(src, base string) string { return strings.ReplaceAll(src, "BASE", base) }

// unaddress is out with the addresses that vary from run to run named:
// BASE, DEAD, and the dead address's parts in a refused connection's cause,
// whose errno is the platform's (-61 on macOS, -111 on Linux).
func unaddress(out, base, dead string) string {
	host := strings.TrimPrefix(dead, "http://")
	_, port, _ := strings.Cut(host, ":")
	return strings.NewReplacer(
		base, "BASE", dead, "DEAD", host, "DEADADDR", "port: "+port, "port: DEADPORT",
		"errno: "+strconv.Itoa(-int(syscall.ECONNREFUSED)), "errno: ERRNO",
	).Replace(out)
}

// runOutput runs a command and returns its stdout, stderr and exit code.
func runOutput(cmd *exec.Cmd) (string, string, int) {
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		code = -1
	}
	return stdout.String(), stderr.String(), code
}

// TestRecordFetchGoldens runs every fetch program under Node, when asked to.
func TestRecordFetchGoldens(t *testing.T) {
	t.Parallel()
	node := os.Getenv("CAKEBEAR_RECORD_FETCH")
	if node == "" {
		t.Skip("set CAKEBEAR_RECORD_FETCH to Node 24.18's binary to record fetch's goldens")
	}
	base, dead := upstream(t), deadAddress(t)
	out := map[string]fetchGolden{}
	for name, src := range fetchPrograms {
		path := filepath.Join(t.TempDir(), "main.ts")
		if err := os.WriteFile(path, []byte(strings.ReplaceAll(withBase(src, base), "DEAD", dead)), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := runOutput(exec.Command(node, "--stack-trace-limit=0", path))
		out[name] = fetchGolden{Stdout: unaddress(stdout, base, dead), Stderr: unaddress(strings.ReplaceAll(stderr, path, "MAIN"), base, dead), Exit: code}
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fetchGoldensPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
