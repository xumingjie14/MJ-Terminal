package main

// v0.59 block 3 event-bus tests: the pub/sub core (subscribe,
// topic filter, ordering, unsubscribe, slow-consumer drop), the
// datastore -> bus wiring, the SSE transport carrying a real
// mutation's event, and the route-absence rule (the stream is
// LOCAL-mux only; the remote-assist mux must never grow it).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func drainBus(ch chan busEvent, n int, timeout time.Duration) []busEvent {
	out := []busEvent{}
	deadline := time.After(timeout)
	for len(out) < n {
		select {
		case ev := <-ch:
			out = append(out, ev)
		case <-deadline:
			return out
		}
	}
	return out
}

func TestBusPublishSubscribeOrder(t *testing.T) {
	ch, done := busSubscribe()
	defer done()
	busPublish("layout", "a")
	busPublish("files", "b")
	busPublish("layout", "c")
	got := drainBus(ch, 3, 2*time.Second)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "b" || got[2].ID != "c" {
		t.Fatalf("order broken: %+v", got)
	}
	if got[0].Ts == 0 {
		t.Fatal("events must carry a timestamp")
	}
}

func TestBusTopicFilter(t *testing.T) {
	ch, done := busSubscribe("files")
	defer done()
	busPublish("layout", "x")
	busPublish("files", "y")
	got := drainBus(ch, 1, 2*time.Second)
	if len(got) != 1 || got[0].Topic != "files" {
		t.Fatalf("topic filter broken: %+v", got)
	}
	select {
	case ev := <-ch:
		t.Fatalf("unexpected extra event: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestBusUnsubscribe(t *testing.T) {
	before := busSubscriberCount()
	ch, done := busSubscribe()
	if busSubscriberCount() != before+1 {
		t.Fatal("subscriber count did not rise")
	}
	done()
	if busSubscriberCount() != before {
		t.Fatal("subscriber count did not fall after unsubscribe")
	}
	done() // idempotent
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("channel must be closed after unsubscribe")
		}
	default:
	}
}

func TestBusSlowConsumerDrops(t *testing.T) {
	ch, done := busSubscribe("files")
	defer done()
	dropsBefore := busDropCount()
	// Far more than the per-subscriber buffer, without reading:
	// publishing must never block; the overflow is dropped and
	// counted.
	for i := 0; i < busSubBuffer*4; i++ {
		busPublish("files", "f")
	}
	if busDropCount() <= dropsBefore {
		t.Fatal("slow consumer overflow was not counted as dropped")
	}
	got := drainBus(ch, busSubBuffer, 2*time.Second)
	if len(got) != busSubBuffer {
		t.Fatalf("buffered %d events, want the full buffer %d", len(got), busSubBuffer)
	}
}

func TestBusDatastoreEmitsMappedTopics(t *testing.T) {
	dsTestRoot(t)
	ch, done := busSubscribe()
	defer done()
	if err := dsSave("layout", []byte(`{"pos":{}}`)); err != nil {
		t.Fatalf("dsSave layout: %v", err)
	}
	if err := dsSave("settings", []byte(`{}`)); err != nil {
		t.Fatalf("dsSave settings: %v", err)
	}
	got := drainBus(ch, 2, 2*time.Second)
	if len(got) != 2 || got[0].Topic != "layout" || got[1].Topic != "settings" {
		t.Fatalf("datastore emits broken: %+v", got)
	}
	// Unmapped stores stay silent: wingeom is high-frequency
	// bookkeeping, update-token is a credential.
	if err := dsSave("wingeom", []byte(`{}`)); err != nil {
		t.Fatalf("dsSave wingeom: %v", err)
	}
	select {
	case ev := <-ch:
		t.Fatalf("unmapped store emitted: %+v", ev)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestViewStreamCarriesBusEvents is the SSE integration test: a
// REAL mutation (a datastore save) must surface on
// /api/view/stream as a typed bus event, alongside the stream's
// original view-command family.
func TestViewStreamCarriesBusEvents(t *testing.T) {
	dsTestRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/view/stream", nil).WithContext(ctx)
	rec := &streamRecorder{ResponseRecorder: httptest.NewRecorder(), chunk: make(chan string, 64)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		viewStreamHandler(rec, req)
	}()
	// Give the handler a moment to subscribe, then mutate.
	time.Sleep(100 * time.Millisecond)
	if err := dsSave("layout", []byte(`{"pos":{}}`)); err != nil {
		t.Fatalf("dsSave: %v", err)
	}
	var body strings.Builder
	deadline := time.After(5 * time.Second)
	for !strings.Contains(body.String(), `"topic":"layout"`) {
		select {
		case c := <-rec.chunk:
			body.WriteString(c)
		case <-deadline:
			t.Fatalf("no layout event on the stream; got: %q", body.String())
		}
	}
	// The client leaving must end the handler (and its bus
	// subscription) -- no leaked goroutine/subscriber.
	subsBefore := busSubscriberCount()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream handler did not exit after client disconnect")
	}
	if busSubscriberCount() >= subsBefore {
		t.Fatal("bus subscription leaked after stream disconnect")
	}
}

// streamRecorder is an http.ResponseWriter + Flusher that hands
// each Write to the test as it happens (httptest's recorder only
// accumulates, which cannot observe a long-lived stream).
type streamRecorder struct {
	*httptest.ResponseRecorder
	chunk chan string
}

func (s *streamRecorder) Write(b []byte) (int, error) {
	cp := string(b)
	select {
	case s.chunk <- cp:
	default:
		// A full test buffer must not wedge the handler.
	}
	return s.ResponseRecorder.Write(b)
}

func (s *streamRecorder) Flush() {}

// TestViewStreamLocalMuxOnly pins the security rule: the change
// feed is registered on the LOCAL mux and must never appear in
// the remote-assist mux (same pattern as the thumb route test).
func TestViewStreamLocalMuxOnly(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `mux.HandleFunc("/api/view/stream", viewStreamHandler)`) {
		t.Fatal("/api/view/stream is not registered on the local mux")
	}
	rb, err := os.ReadFile("remote.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rb), "view/stream") {
		t.Fatal("/api/view/stream leaked into the remote-assist mux -- the change feed is local-only")
	}
}
