package main

import (
	"testing"
	"time"
)

// v0.50: the view command channel's sticky delivery -- a command
// broadcast while no desktop view is connected (the F8 summon
// recreates the view, which connects only afterwards) must be
// handed to the next connecting view exactly once, and only
// while fresh.
func TestViewCmdStickyPending(t *testing.T) {
	viewCmdState.Lock()
	viewCmdState.subs = map[chan viewCommand]struct{}{}
	viewCmdState.pending = nil
	viewCmdState.Unlock()

	// No subscribers: the command becomes pending.
	viewCmdBroadcast("spotlight")
	p := viewCmdTakePending()
	if p == nil || p.Cmd != "spotlight" {
		t.Fatalf("expected pending spotlight command, got %v", p)
	}
	// Taken exactly once.
	if p := viewCmdTakePending(); p != nil {
		t.Fatalf("pending must be consumed once, got second %v", p)
	}

	// A stale pending command is dropped, not delivered.
	viewCmdState.Lock()
	viewCmdState.pending = &viewCommand{Cmd: "spotlight", Ts: time.Now().Add(-viewCmdStickyWindow - time.Second).UnixMilli()}
	viewCmdState.Unlock()
	if p := viewCmdTakePending(); p != nil {
		t.Fatalf("stale pending must be dropped, got %v", p)
	}

	// With a live subscriber the command is delivered directly
	// and nothing stays pending.
	ch := make(chan viewCommand, 4)
	viewCmdState.Lock()
	viewCmdState.subs[ch] = struct{}{}
	viewCmdState.Unlock()
	viewCmdBroadcast("spotlight")
	select {
	case vc := <-ch:
		if vc.Cmd != "spotlight" {
			t.Fatalf("delivered cmd = %q", vc.Cmd)
		}
	default:
		t.Fatal("subscriber received nothing")
	}
	if p := viewCmdTakePending(); p != nil {
		t.Fatalf("delivered command must not stay pending, got %v", p)
	}
	viewCmdState.Lock()
	delete(viewCmdState.subs, ch)
	viewCmdState.Unlock()
}
