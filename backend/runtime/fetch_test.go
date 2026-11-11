package runtime

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fetchServer serves the routes s1-node's cases fetch (spikes/s1-node,
// server.mjs), as far as Go's server can: /echo answers with what it
// received.
func fetchServer(t *testing.T) string {
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
		head := map[string]string{}
		for name, values := range r.Header {
			head[strings.ToLower(name)] = strings.Join(values, ", ")
		}
		head["content-length"] = strconv.FormatInt(r.ContentLength, 10)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"method": r.Method, "url": r.URL.String(), "headers": head, "body": string(body)})
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		code, _ := strconv.Atoi(r.URL.Query().Get("code"))
		w.WriteHeader(code)
		fmt.Fprintf(w, "status %d", code)
	})
	mux.HandleFunc("/gzip", func(w http.ResponseWriter, _ *http.Request) {
		var z bytes.Buffer
		zw := gzip.NewWriter(&z)
		_, _ = io.WriteString(zw, "this body was gzipped by the server")
		_ = zw.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", strconv.Itoa(z.Len()))
		_, _ = w.Write(z.Bytes())
	})
	mux.HandleFunc("/bytes", func(w http.ResponseWriter, r *http.Request) {
		b, _ := hexBytes(r.URL.Query().Get("hex"))
		_, _ = w.Write(b)
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
		time.Sleep(time.Duration(ms) * time.Millisecond)
		_, _ = io.WriteString(w, "slow")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func hexBytes(s string) ([]byte, error) {
	out := make([]byte, 0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		n, err := strconv.ParseUint(s[i:i+2], 16, 8)
		if err != nil {
			return nil, err
		}
		out = append(out, byte(n))
	}
	return out, nil
}

// rawServer answers every connection with resp, as written: for the status
// lines Go's server won't send, such as a reason phrase of the program's own.
func rawServer(t *testing.T, resp string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil || line == "\r\n" {
						break
					}
				}
				_, _ = io.WriteString(c, resp)
			}()
		}
	}()
	return "http://" + ln.Addr().String()
}

// fetchProgram fetches each input in turn in one async main, logging what
// show makes of each Response, or the failure.
func fetchProgram(urls []string, inits []*FetchInit, show func(d *Domain, co *Co, r *Response, log func(string))) func(*Domain, func(string)) {
	return func(d *Domain, log func(string)) {
		Async(d, func(co *Co) struct{} {
			for i, u := range urls {
				var r *Response
				if t := Try(func() { r = Await(co, Fetch(d, u, inits[i], nil)) }); t != nil {
					log("rejected " + UnknownToString(t.V))
					continue
				}
				show(d, co, r, log)
			}
			return struct{}{}
		})
	}
}

func showText(d *Domain, co *Co, r *Response, log func(string)) {
	log(fmt.Sprintf("%v %v %q %v", r.Status(), r.OK(), r.StatusText(), r.Redirected()))
	log(Await(co, r.Text(d, nil)))
}

func TestFetchReadsAResponse(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	base := fetchServer(t)
	got := runProgram(t, func(d *Domain, log func(string)) {
		Async(d, func(co *Co) struct{} {
			r := Await(co, Fetch(d, base+"/hello", nil, nil))
			h := r.Headers()
			log(fmt.Sprintf("%v %v %q %v %v", r.Status(), r.OK(), r.StatusText(), r.URL() == base+"/hello", r.Redirected()))
			for _, name := range []string{"X-Custom", "x-multi", "set-cookie", "missing"} {
				log(name + " " + MaybeToString(h.Get(name, nil), func(s string) string { return s }))
			}
			log(strings.Join(h.GetSetCookie().items, "|"))
			log(fmt.Sprint(h.Has("X-CUSTOM", nil), r.BodyUsed()))
			log(Await(co, r.Text(d, nil)))
			log(strconv.FormatBool(r.BodyUsed()))
			return struct{}{}
		})
	})
	want := "200 true \"OK\" true false\nX-Custom Mixed Case Value\nx-multi one, two\nset-cookie a=1; Path=/, b=2\nmissing null\na=1; Path=/|b=2\ntrue false\nhello, world\ntrue\n"
	if got != want {
		t.Fatalf("printed\n%s\nwant\n%s", got, want)
	}
}
