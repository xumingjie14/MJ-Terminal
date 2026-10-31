package main

// ---------- view command channel (v0.50) ----------
//
// A one-way SSE stream from the daemon to the desktop view for
// commands that originate OUTSIDE the view -- today exactly one:
// "spotlight" (the F8 global summon hotkey asks the recalled
// desktop to open Spotlight). This is deliberately a separate
// stream from /api/notify/stream: notifications are user-facing
// records with dedupe, persistence and per-source switches, while
// a view command is an ephemeral UI instruction -- overloading the
// banner stream would have forced a fake notification record for
// every summon.
//
// Delivery has a race the plain broadcast cannot cover: F8 first
// recalls the view set (the desktop view may have to be created),
// and a freshly created view connects to this stream only after
// the summon already fired. So a command broadcast with no live
// subscriber is kept as a sticky pending command for a short
// window and handed to the next view that connects. Commands are
// idempotent UI opens, so a rare duplicate delivery is harmless.
//
// GET /api/view/stream -> SSE, one JSON object per message.
// Two message families share the stream since v0.59 (block 3):
//   - view commands (this file, pre-existing): {"cmd":"spotlight","ts":<unix ms>}
//   - change-bus events (events.go): {"topic":"files","id":"x.png","ts":<unix ms>}
// The one pre-existing consumer (Spotlight's F8 summon) reads
// only "cmd" and is unaffected; the view's event client
// (web/src/b1/events.ts) reads only "topic". Local mux only --
// this route must never appear on the remote-assist mux (a test
// in viewcmd_test.go pins that).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// viewCmdStickyWindow: how long an undelivered command stays
// pending for the next connecting view. Long enough for a cold
// Edge --app window to load the shell page, short enough that a
// much later manual reopen never surprises the user with a stale
// Spotlight.
const viewCmdStickyWindow = 20 * time.Second

type viewCommand struct {
	Cmd string `json:"cmd"`
	Ts  int64  `json:"ts"` // unix ms
}

var viewCmdState = struct {
	sync.Mutex
	subs    map[chan viewCommand]struct{}
	pending *viewCommand
}{subs: map[chan viewCommand]struct{}{}}

// viewCmdBroadcast delivers cmd to every connected view; with no
// view connected it becomes the sticky pending command instead.
func viewCmdBroadcast(cmd string) {
	vc := viewCommand{Cmd: cmd, Ts: time.Now().UnixMilli()}
	viewCmdState.Lock()
	defer viewCmdState.Unlock()
	delivered := false
	for ch := range viewCmdState.subs {
		select {
		case ch <- vc:
			delivered = true
		default: // a stuck view must never block the daemon
		}
	}
	if !delivered {
		viewCmdState.pending = &vc
	}
}

// viewCmdTakePending returns (and clears) the sticky command if
// it is still fresh. Called by the stream handler on connect.
func viewCmdTakePending() *viewCommand {
	viewCmdState.Lock()
	defer viewCmdState.Unlock()
	p := viewCmdState.pending
	viewCmdState.pending = nil
	if p == nil {
		return nil
	}
	if time.Since(time.UnixMilli(p.Ts)) > viewCmdStickyWindow {
		return nil
	}
	return p
}

func viewStreamHandler(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, map[string]any{"error": "stream unsupported"})
		return
	}
	ch := make(chan viewCommand, 4)
	viewCmdState.Lock()
	viewCmdState.subs[ch] = struct{}{}
	viewCmdState.Unlock()
	defer func() {
		viewCmdState.Lock()
		delete(viewCmdState.subs, ch)
		viewCmdState.Unlock()
	}()
	// v0.59: the same connection also carries the change bus
	// (all topics). Unsubscribe on return; the bus side is
	// drop-tolerant, so a slow view never stalls a mutation.
	busCh, busDone := busSubscribe()
	defer busDone()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	if p := viewCmdTakePending(); p != nil {
		b, _ := json.Marshal(p)
		fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}
	hb := time.NewTicker(25 * time.Second)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-hb.C:
			fmt.Fprint(w, ": hb\n\n")
			fl.Flush()
		case vc := <-ch:
			b, _ := json.Marshal(vc)
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		case ev := <-busCh:
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		}
	}
}
