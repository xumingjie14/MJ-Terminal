package main

// cdpload_test.go -- v0.71 block 1: the DevTools-pipe bridge
// loader against scripted fake transports. The real browser
// conversation cannot run headless (it needs a branded
// Chrome on Windows), so everything provable without one is
// pinned here: message framing (JSON + NUL), response
// matching by id, event frames being skipped, protocol
// errors surfacing verbatim, a closed pipe mapping to
// errCDPPipeClosed, and timeout resends.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeCDPTransport replays scripted read chunks and records
// everything written to it.
type fakeCDPTransport struct {
	chunks [][]byte
	wrote  bytes.Buffer
	closed bool
}

func (f *fakeCDPTransport) Read(p []byte) (int, error) {
	for len(f.chunks) > 0 && len(f.chunks[0]) == 0 {
		f.chunks = f.chunks[1:]
	}
	if len(f.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, f.chunks[0])
	f.chunks[0] = f.chunks[0][n:]
	return n, nil
}

func (f *fakeCDPTransport) Write(p []byte) (int, error) { return f.wrote.Write(p) }
func (f *fakeCDPTransport) Close() error                { f.closed = true; return nil }

func cdpFrameBytes(s string) []byte { return append([]byte(s), 0) }

func TestCDPLoadUnpackedSuccess(t *testing.T) {
	ft := &fakeCDPTransport{chunks: [][]byte{
		cdpFrameBytes(`{"id":1,"result":{"id":"abcdefghijklmnopabcdefghijklmnop"}}`),
	}}
	id, err := cdpLoadUnpacked(ft, `D:\Data\extensions\webnotify`, 2*time.Second, 1)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if id != "abcdefghijklmnopabcdefghijklmnop" {
		t.Fatalf("extension id = %q", id)
	}
	// The written command must be ONE NUL-terminated JSON
	// object naming the method and the path.
	w := ft.wrote.Bytes()
	if len(w) == 0 || w[len(w)-1] != 0 {
		t.Fatalf("command not NUL-terminated: %q", w)
	}
	var cmd struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
		Params struct {
			Path string `json:"path"`
		} `json:"params"`
	}
	if err := json.Unmarshal(w[:len(w)-1], &cmd); err != nil {
		t.Fatalf("command not JSON: %v", err)
	}
	if cmd.ID != 1 || cmd.Method != "Extensions.loadUnpacked" || cmd.Params.Path != `D:\Data\extensions\webnotify` {
		t.Fatalf("command shape wrong: %+v", cmd)
	}
}

func TestCDPLoadUnpackedSkipsEventFrames(t *testing.T) {
	ft := &fakeCDPTransport{chunks: [][]byte{
		cdpFrameBytes(`{"method":"Target.targetCreated","params":{}}`),
		cdpFrameBytes(`{"id":7,"result":{}}`),
		cdpFrameBytes(`{"id":1,"result":{"id":"ext42"}}`),
	}}
	id, err := cdpLoadUnpacked(ft, "/tmp/ext", 2*time.Second, 1)
	if err != nil || id != "ext42" {
		t.Fatalf("got (%q, %v), want (ext42, nil)", id, err)
	}
}

func TestCDPLoadUnpackedSplitAndBatchedFrames(t *testing.T) {
	// The response arrives split mid-token across reads, with
	// an event frame glued to its tail in the same chunk.
	full := cdpFrameBytes(`{"id":1,"result":{"id":"splitid"}}`)
	ev := cdpFrameBytes(`{"method":"Page.loadEventFired","params":{}}`)
	ft := &fakeCDPTransport{chunks: [][]byte{
		full[:7], full[7:20], append(full[20:], ev...),
	}}
	id, err := cdpLoadUnpacked(ft, "/tmp/ext", 2*time.Second, 1)
	if err != nil || id != "splitid" {
		t.Fatalf("got (%q, %v), want (splitid, nil)", id, err)
	}
}

func TestCDPLoadUnpackedProtocolError(t *testing.T) {
	ft := &fakeCDPTransport{chunks: [][]byte{
		cdpFrameBytes(`{"id":1,"error":{"code":-32601,"message":"Method not available."}}`),
	}}
	_, err := cdpLoadUnpacked(ft, "/tmp/ext", 2*time.Second, 3)
	var ce *cdpError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v (%T), want *cdpError", err, err)
	}
	if !strings.Contains(ce.Message, "Method not available") {
		t.Fatalf("message = %q", ce.Message)
	}
	// A protocol error is final: exactly one command written.
	if n := bytes.Count(ft.wrote.Bytes(), []byte{0}); n != 1 {
		t.Fatalf("commands written = %d, want 1 (no retry on protocol error)", n)
	}
}

func TestCDPLoadUnpackedPipeClosed(t *testing.T) {
	ft := &fakeCDPTransport{} // immediate EOF
	_, err := cdpLoadUnpacked(ft, "/tmp/ext", 2*time.Second, 1)
	if !errors.Is(err, errCDPPipeClosed) {
		t.Fatalf("err = %v, want errCDPPipeClosed", err)
	}
}

// silentCDPTransport never answers; reads block briefly then
// report EOF once the test's patience runs out is NOT what we
// want -- reads just sleep, so the loader's own timeout is
// what fires.
type silentCDPTransport struct{ wrote bytes.Buffer }

func (s *silentCDPTransport) Read(p []byte) (int, error) {
	time.Sleep(50 * time.Millisecond)
	return 0, nil // zero-byte read: the frame reader retries
}
func (s *silentCDPTransport) Write(p []byte) (int, error) { return s.wrote.Write(p) }
func (s *silentCDPTransport) Close() error                { return nil }

func TestCDPLoadUnpackedTimeoutResends(t *testing.T) {
	st := &silentCDPTransport{}
	_, err := cdpLoadUnpacked(st, "/tmp/ext", 60*time.Millisecond, 3)
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("err = %v, want timeout", err)
	}
	if n := bytes.Count(st.wrote.Bytes(), []byte{0}); n != 3 {
		t.Fatalf("commands written = %d, want 3 (one per attempt)", n)
	}
}

func TestCDPEncodeShape(t *testing.T) {
	b := cdpEncode(9, "Extensions.loadUnpacked", map[string]any{"path": "/x"})
	if b[len(b)-1] != 0 {
		t.Fatal("missing NUL terminator")
	}
	var m map[string]any
	if err := json.Unmarshal(b[:len(b)-1], &m); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if m["method"] != "Extensions.loadUnpacked" {
		t.Fatalf("method = %v", m["method"])
	}
}
