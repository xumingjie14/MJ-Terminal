package main

// cdpws_test.go -- v0.71 test2: the Windows (debugging
// port / WebSocket) bridge-load route against scripted
// fakes and one real loopback socket. The branded-Chrome
// conversation itself cannot run headless, so what is
// pinned here is everything this side of it: the /json/
// version probe, the shared loadUnpacked conversation
// over the WebSocket seam -- including the browser
// answering "Method not available", which must surface
// verbatim and finally (no retries) -- and a real
// gorilla dial + frame round-trip against a fake DevTools
// endpoint.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func testServerPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return p
}

// fakeMsgIO scripts recv() results and records sends.
type fakeMsgIO struct {
	script []fakeMsgIOResult
	sends  [][]byte
	closed bool
}

type fakeMsgIOResult struct {
	f   cdpFrame
	err error
}

func (f *fakeMsgIO) send(msg []byte) error {
	f.sends = append(f.sends, append([]byte(nil), msg...))
	return nil
}

func (f *fakeMsgIO) recv() (cdpFrame, error) {
	if len(f.script) == 0 {
		// Nothing scripted: behave like a live but silent
		// channel (zero frames are events to the loader).
		time.Sleep(20 * time.Millisecond)
		return cdpFrame{}, nil
	}
	r := f.script[0]
	f.script = f.script[1:]
	return r.f, r.err
}

func (f *fakeMsgIO) close() error { f.closed = true; return nil }

func swapCDPDial(t *testing.T, mio cdpMsgIO) {
	t.Helper()
	old := cdpDialWS
	cdpDialWS = func(string) (cdpMsgIO, error) { return mio, nil }
	t.Cleanup(func() { cdpDialWS = old })
}

func TestCDPLoadUnpackedWSSuccess(t *testing.T) {
	fake := &fakeMsgIO{script: []fakeMsgIOResult{
		{f: cdpFrame{ID: 1, Result: json.RawMessage(`{"id":"ws-ext-id"}`)}},
	}}
	swapCDPDial(t, fake)
	id, err := cdpLoadUnpackedWS("ws://127.0.0.1:9222/devtools/browser/x", `D:\Data\extensions\webnotify`, 2*time.Second, 1)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if id != "ws-ext-id" {
		t.Fatalf("extension id = %q", id)
	}
	if !fake.closed {
		t.Fatal("channel not closed after the load")
	}
	// The sent command is bare JSON: a WebSocket message
	// carries NO NUL terminator (that is pipe framing).
	if len(fake.sends) != 1 {
		t.Fatalf("sends = %d, want 1", len(fake.sends))
	}
	w := fake.sends[0]
	if len(w) == 0 || w[len(w)-1] == 0 {
		t.Fatalf("WS command must not be NUL-terminated: %q", w)
	}
	var cmd struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
		Params struct {
			Path string `json:"path"`
		} `json:"params"`
	}
	if err := json.Unmarshal(w, &cmd); err != nil {
		t.Fatalf("command not JSON: %v", err)
	}
	if cmd.ID != 1 || cmd.Method != "Extensions.loadUnpacked" || cmd.Params.Path != `D:\Data\extensions\webnotify` {
		t.Fatalf("command shape wrong: %+v", cmd)
	}
}

// The honest-degradation case: the browser answers the
// command with a protocol error. It must surface verbatim
// (the driver logs it as "this browser could not load the
// bridge") and it must be final -- no retries.
func TestCDPLoadUnpackedWSMethodNotAvailable(t *testing.T) {
	fake := &fakeMsgIO{script: []fakeMsgIOResult{
		{f: cdpFrame{ID: 1, Error: &cdpError{Code: -32601, Message: "Method not available."}}},
	}}
	swapCDPDial(t, fake)
	_, err := cdpLoadUnpackedWS("ws://127.0.0.1:9222/devtools/browser/x", "/ext", 2*time.Second, 3)
	var ce *cdpError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v (%T), want *cdpError", err, err)
	}
	if !strings.Contains(ce.Message, "Method not available") {
		t.Fatalf("message = %q", ce.Message)
	}
	if len(fake.sends) != 1 {
		t.Fatalf("sends = %d, want 1 (protocol errors are final)", len(fake.sends))
	}
}

func TestCDPLoadUnpackedWSChannelClosed(t *testing.T) {
	fake := &fakeMsgIO{script: []fakeMsgIOResult{
		{err: errCDPWSClosed},
	}}
	swapCDPDial(t, fake)
	_, err := cdpLoadUnpackedWS("ws://127.0.0.1:9222/devtools/browser/x", "/ext", 2*time.Second, 3)
	if !errors.Is(err, errCDPWSClosed) {
		t.Fatalf("err = %v, want errCDPWSClosed", err)
	}
	if len(fake.sends) != 1 {
		t.Fatalf("sends = %d, want 1 (a dead channel is final)", len(fake.sends))
	}
}

func TestCDPDevToolsWSURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Browser":"Chrome/141.0","webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/abc"}`))
	}))
	defer srv.Close()
	url, err := cdpDevToolsWSURL(testServerPort(t, srv), 2*time.Second)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if url != "ws://127.0.0.1:9222/devtools/browser/abc" {
		t.Fatalf("url = %q", url)
	}
	if gotPath != "/json/version" {
		t.Fatalf("probe path = %q", gotPath)
	}
}

func TestCDPDevToolsWSURLTimeout(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	start := time.Now()
	_, err := cdpDevToolsWSURL(testServerPort(t, srv), 600*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("err = %v, want a timeout error", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("probe ignored its wait budget: %v", time.Since(start))
	}
}

// End to end over a real socket: a fake DevTools endpoint
// (gorilla upgrade, one command in, one result out) and
// the REAL dialer -- this is the exact code path the
// Windows launch fires.
func TestCDPLoadUnpackedWSRealSocket(t *testing.T) {
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var cmd struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(data, &cmd); err != nil || cmd.Method != "Extensions.loadUnpacked" {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"id":1,"result":{"id":"real-sock-ext"}}`))
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/browser/abc"
	id, err := cdpLoadUnpackedWS(wsURL, "/ext", 2*time.Second, 1)
	if err != nil {
		t.Fatalf("load over real socket: %v", err)
	}
	if id != "real-sock-ext" {
		t.Fatalf("extension id = %q", id)
	}
}
