package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// lspServer is a language server started for a test (`<bin> --lsp --stdio`),
// spoken to over its stdin and stdout.
type lspServer struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr syncBuffer
	inbox  chan lspMessage
	nextID int
}

// lspMessage is any JSON-RPC message: a request has an ID and a method, a
// notification only a method, and a response only an ID.
type lspMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

func startLSP(t *testing.T, bin, dir string) *lspServer {
	t.Helper()
	s := &lspServer{t: t, inbox: make(chan lspMessage, 64)}
	s.cmd = exec.Command(bin, "--lsp", "--stdio")
	s.cmd.Dir = dir
	s.cmd.Stderr = &s.stderr
	stdin, err := s.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s.stdin = stdin
	go s.read(bufio.NewReader(stdout))
	return s
}

// read turns the server's output into messages until it ends.
func (s *lspServer) read(r *bufio.Reader) {
	defer close(s.inbox)
	for {
		length := -1
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if v, ok := strings.CutPrefix(strings.ToLower(line), "content-length:"); ok {
				length, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
		if length < 0 {
			return
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(r, body); err != nil {
			return
		}
		var m lspMessage
		if err := json.Unmarshal(body, &m); err != nil {
			return
		}
		s.inbox <- m
	}
}

// send writes one message. A nil "params" is left out: the server refuses a
// null one where a method takes none (shutdown, exit).
func (s *lspServer) send(m map[string]any) {
	m["jsonrpc"] = "2.0"
	if params, ok := m["params"]; ok && params == nil {
		delete(m, "params")
	}
	body, err := json.Marshal(m)
	if err != nil {
		s.t.Fatal(err)
	}
	// A write error means the server has gone, which the next read reports.
	_, _ = fmt.Fprintf(s.stdin, "Content-Length: %d\r\n\r\n%s", len(body), body)
}

func (s *lspServer) notify(method string, params any) {
	s.send(map[string]any{"method": method, "params": params})
}

// request sends a request and returns its result. Whatever the server asks in
// the meantime (configuration, capability registration) gets a null answer.
func (s *lspServer) request(method string, params any) json.RawMessage {
	s.t.Helper()
	s.nextID++
	id := strconv.Itoa(s.nextID)
	s.send(map[string]any{"id": s.nextID, "method": method, "params": params})
	timeout := time.After(30 * time.Second)
	for {
		select {
		case m, ok := <-s.inbox:
			if !ok {
				s.t.Fatalf("%s: the server's output ended\n%s", method, s.stderr.String())
			}
			switch {
			case m.Method != "" && m.ID != nil:
				s.send(map[string]any{"id": m.ID, "result": nil})
			case m.Method == "" && string(m.ID) == id:
				if m.Error != nil {
					s.t.Fatalf("%s: %s", method, m.Error)
				}
				return m.Result
			}
		case <-timeout:
			s.t.Fatalf("%s: no answer within 30s\n%s", method, s.stderr.String())
		}
	}
}

// initialize opens the session for the project at dir, as an editor does
// first, and returns the server's answer: everything it can do.
func (s *lspServer) initialize(dir string) json.RawMessage {
	s.t.Helper()
	result := s.request("initialize", map[string]any{
		"processId": nil,
		"rootUri":   fileURI(dir),
		"capabilities": map[string]any{"textDocument": map[string]any{
			"diagnostic": map[string]any{}, "definition": map[string]any{}, "publishDiagnostics": map[string]any{},
		}},
	})
	s.notify("initialized", map[string]any{})
	return result
}

func fileURI(path string) string { return "file://" + filepath.ToSlash(path) }

// open tells the server an editor opened the file at path.
func (s *lspServer) open(path, text string) {
	s.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": fileURI(path), "languageId": "typescript", "version": 1, "text": text,
	}})
}

// lspDiagnostic is the part of a diagnostic the tests look at.
type lspDiagnostic struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
}

// lspLocation is a Location (uri) or a LocationLink (targetUri).
type lspLocation struct {
	URI       string `json:"uri"`
	TargetURI string `json:"targetUri"`
}

// diagnostics pulls the open file's diagnostics.
func (s *lspServer) diagnostics(path string) []lspDiagnostic {
	s.t.Helper()
	var report struct {
		Items []lspDiagnostic `json:"items"`
	}
	result := s.request("textDocument/diagnostic", map[string]any{"textDocument": map[string]any{"uri": fileURI(path)}})
	if err := json.Unmarshal(result, &report); err != nil {
		s.t.Fatalf("diagnostics: %v in %s", err, result)
	}
	return report.Items
}

// definition returns the URIs go-to-definition offers at a position.
func (s *lspServer) definition(path string, line, character int) []string {
	s.t.Helper()
	result := s.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": fileURI(path)},
		"position":     map[string]any{"line": line, "character": character},
	})
	// A Location, a list of them, or a list of LocationLinks.
	var locations []lspLocation
	if err := json.Unmarshal(result, &locations); err != nil {
		var one lspLocation
		if err := json.Unmarshal(result, &one); err != nil {
			s.t.Fatalf("definition: %v in %s", err, result)
		}
		locations = append(locations, one)
	}
	var uris []string
	for _, l := range locations {
		uris = append(uris, l.URI+l.TargetURI)
	}
	return uris
}

// close shuts the server down as an editor does and returns its exit code.
// Upstream's server exits 1 even after a clean shutdown: its exit handler
// cancels the context that Run then returns as an error, "context canceled".
func (s *lspServer) close() int {
	s.t.Helper()
	s.request("shutdown", nil)
	s.notify("exit", nil)

	// Wait closes stdout, so the reader has to finish first. Stdin stays open
	// until the server has gone: closing it as well would race exit's
	// handling against end of input's, and the two exit with different codes.
	timeout := time.After(10 * time.Second)
	for drained := false; !drained; {
		select {
		case _, ok := <-s.inbox:
			drained = !ok
		case <-timeout:
			_ = s.cmd.Process.Kill()
			s.t.Errorf("the server was still running 10s after exit\n%s", s.stderr.String())
			drained = true
		}
	}
	_ = s.stdin.Close()
	err := s.cmd.Wait()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode()
	}
	if err != nil {
		s.t.Errorf("waiting for the server: %v", err)
	}
	return 0
}
