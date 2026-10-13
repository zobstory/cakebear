package runtime

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// serveAsync is serveWith for a listener that runs in its request's domain.
func serveAsync(t *testing.T, handler func(*Domain, *IncomingMessage, *ServerResponse)) string {
	t.Helper()
	s := createServerD(handler)
	ts := httptest.NewUnstartedServer(http.HandlerFunc(s.serve))
	ts.Config.ErrorLog = log.New(io.Discard, "", 0) // panics are expected in some tests
	ts.Start()
	t.Cleanup(ts.Close)
	return ts.URL
}

// waitUntil polls cond, failing the test if it does not hold within a second.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// Until the response ends, the request's goroutine drives its domain: an
// async handler that awaits a timer responds once the timer has fired.
func TestAsyncHandlerRespondsAfterItsAwait(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	isolate(t)
	base := serveAsync(t, func(d *Domain, _ *IncomingMessage, res *ServerResponse) {
		Async(d, func(co *Co) struct{} {
			Await(co, sleepFn(d, 30))
			res.End("done")
			return struct{}{}
		})
	})

	start := time.Now()
	r := fetch(http.DefaultClient, "GET", base+"/")
	if r.err != nil || r.body != "done" {
		t.Fatalf("got %v %q, want the body sent after the await", r.err, r.body)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Errorf("responded after %v, before the 30 ms timer it awaited", elapsed)
	}
}

// Node finishes a response at res.end() and lets the handler keep working;
// Go finishes one when the handler returns. So work scheduled after the end
// must not hold the response back, must still run, and must keep the process
// alive while it does (M2 plan, D4).
func TestWorkAfterEndDoesNotDelayTheResponse(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	isolate(t)
	fired := make(chan struct{})
	base := serveAsync(t, func(d *Domain, _ *IncomingMessage, res *ServerResponse) {
		res.End("ok")
		SetTimeout(d, func(*Domain) { close(fired) }, 300)
	})

	start := time.Now()
	r := fetch(http.DefaultClient, "GET", base+"/")
	if r.err != nil || r.body != "ok" {
		t.Fatalf("got %v %q, want ok", r.err, r.body)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("responded after %v; a timer set after res.end() held the response back", elapsed)
	}
	if liveHandles.Load() != 1 {
		t.Errorf("liveHandles = %d while the request's timer is pending, want 1", liveHandles.Load())
	}
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("the timer set after res.end() never fired")
	}
	waitUntil(t, "the finished request to stop keeping the process alive", func() bool { return liveHandles.Load() == 0 })
}

// Requests are domains of their own, so they run in parallel: 50 handlers
// each awaiting a 50 ms timer finish in far less than 50 × 50 ms, and each
// sees only its own work. Under -race nothing they touch may be shared.
func TestAsyncRequestsRunInParallel(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	isolate(t)
	base := serveAsync(t, func(d *Domain, req *IncomingMessage, res *ServerResponse) {
		var steps []string
		Async(d, func(co *Co) struct{} {
			steps = append(steps, "start "+req.URL())
			Await(co, sleepFn(d, 50))
			steps = append(steps, "end "+req.URL())
			res.End(strings.Join(steps, ","))
			return struct{}{}
		})
	})

	const n = 50
	bodies := make([]string, n)
	var wg sync.WaitGroup
	start := time.Now()
	for i := range n {
		wg.Go(func() {
			path := "/" + NumberToString(float64(i))
			if r := fetch(http.DefaultClient, "GET", base+path); r.err == nil {
				bodies[i] = r.body
			}
		})
	}
	wg.Wait()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("%d concurrent 50 ms handlers took %v; they did not overlap", n, elapsed)
	}
	for i, body := range bodies {
		path := "/" + NumberToString(float64(i))
		if want := "start " + path + ",end " + path; body != want {
			t.Errorf("request %s got %q, want %q", path, body, want)
		}
	}
}

// A panic after an await, where Node would throw, still drops only that
// request's connection: it surfaces on the request's goroutine, which
// net/http recovers.
func TestAsyncHandlerPanicDropsOnlyItsConnection(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	isolate(t)
	base := serveAsync(t, func(d *Domain, req *IncomingMessage, res *ServerResponse) {
		Async(d, func(co *Co) struct{} {
			Await(co, sleepFn(d, 1))
			if req.URL() == "/boom" {
				res.WriteHead(200).SetHeader("Late", "x") // Node throws here
			}
			res.End("fine")
			return struct{}{}
		})
	})
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

	if r := fetch(client, "GET", base+"/boom"); r.err == nil {
		t.Errorf("/boom got %d %q, want the connection dropped", r.status, r.body)
	}
	if r := fetch(client, "GET", base+"/"); r.err != nil || r.body != "fine" {
		t.Errorf("after a panic, / got %v %q; the server should still serve", r.err, r.body)
	}
}

// A handler that never ends its response is warned about once its domain has
// nothing left to do, not before: a pending timer could still end it.
func TestUnendedResponseWarnsAfterItsTimers(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	_, stderr, _ := isolate(t)
	ran := false
	base := serveAsync(t, func(d *Domain, _ *IncomingMessage, _ *ServerResponse) {
		SetTimeout(d, func(*Domain) { ran = true }, 20)
	})

	if r := fetch(http.DefaultClient, "GET", base+"/x"); r.err != nil || r.status != 200 || r.body != "" {
		t.Errorf("got %v %d %q, want an empty 200", r.err, r.status, r.body)
	}
	if !ran {
		t.Error("the response was sent before the handler's timer ran")
	}
	if !strings.Contains(stderr.String(), "returned without calling res.end()") {
		t.Errorf("stderr = %q, want the missing-end warning", stderr.String())
	}
}

// A panic in work that runs after the response has nothing to drop and
// nothing to recover it, so it is reported on stderr, and the process stays
// up.
func TestPanicAfterTheResponseIsReported(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	_, stderr, _ := isolate(t)
	base := serveAsync(t, func(d *Domain, _ *IncomingMessage, res *ServerResponse) {
		res.End("ok")
		SetTimeout(d, func(*Domain) { res.End("again") }, 5) // write after end
	})

	if r := fetch(http.DefaultClient, "GET", base+"/"); r.err != nil || r.body != "ok" {
		t.Fatalf("got %v %q, want ok", r.err, r.body)
	}
	waitUntil(t, "the panic to be reported", func() bool {
		return strings.Contains(stderr.String(), "after a response was sent: Error [ERR_STREAM_WRITE_AFTER_END]: write after end")
	})
	waitUntil(t, "the request to stop keeping the process alive", func() bool { return liveHandles.Load() == 0 })
}
