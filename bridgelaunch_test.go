package main

// bridgelaunch_test.go -- v0.71 test2: the launch plan is
// the fault line of the v0.71-test1 incident (a Windows
// launch carrying pipe ends died in Cmd.Start()), so the
// plan's platform guarantees are pinned here, on any OS:
// Windows never gets pipe ends or the pipe flag, the port
// route never emits a port flag without a port, and the
// bridge attempt after a successful start is contained
// (log-only) even when the browser side is unreachable.

import (
	"strings"
	"testing"
	"time"
)

func TestBridgeRouteFor(t *testing.T) {
	cases := []struct {
		goos, extDir, want string
	}{
		{"windows", "/ext", bridgeRoutePort},
		{"linux", "/ext", bridgeRoutePipe},
		{"darwin", "/ext", bridgeRoutePipe},
		{"windows", "", bridgeRouteNone},
		{"linux", "", bridgeRouteNone},
	}
	for _, c := range cases {
		if got := bridgeRouteFor(c.goos, c.extDir); got != c.want {
			t.Errorf("bridgeRouteFor(%q, %q) = %q, want %q", c.goos, c.extDir, got, c.want)
		}
	}
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func hasArgPrefix(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

// The incident regression: a Windows launch plan must
// NEVER ask for pipe ends -- that is what put ExtraFiles
// on the Windows command and killed every browser launch
// in v0.71-test1.
func TestPlanBridgeLaunchWindowsNeverPipe(t *testing.T) {
	for _, port := range []int{0, 9222, 49152} {
		plan := planBridgeLaunch("windows", "/ext", port)
		if plan.route != bridgeRoutePort {
			t.Fatalf("port %d: route = %q, want %q", port, plan.route, bridgeRoutePort)
		}
		if plan.attachPipe {
			t.Fatalf("port %d: attachPipe = true on Windows (the test1 incident)", port)
		}
		if hasArg(plan.args, "--remote-debugging-pipe") {
			t.Fatalf("port %d: Windows plan carries the pipe flag: %v", port, plan.args)
		}
		if !hasArg(plan.args, "--enable-unsafe-extension-debugging") {
			t.Fatalf("port %d: missing --enable-unsafe-extension-debugging: %v", port, plan.args)
		}
		if port > 0 {
			want := "--remote-debugging-port=9222"
			if port != 9222 {
				want = "--remote-debugging-port=49152"
			}
			if !hasArg(plan.args, want) {
				t.Fatalf("port %d: missing %q in %v", port, want, plan.args)
			}
			if plan.debugPort != port {
				t.Fatalf("plan.debugPort = %d, want %d", plan.debugPort, port)
			}
		} else if hasArgPrefix(plan.args, "--remote-debugging-port=") {
			t.Fatalf("port flag emitted without an allocated port: %v", plan.args)
		}
	}
}

func TestPlanBridgeLaunchNonWindowsPipe(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		plan := planBridgeLaunch(goos, "/ext", 0)
		if plan.route != bridgeRoutePipe || !plan.attachPipe {
			t.Fatalf("%s: route=%q attachPipe=%v, want pipe/true", goos, plan.route, plan.attachPipe)
		}
		if !hasArg(plan.args, "--remote-debugging-pipe") || !hasArg(plan.args, "--enable-unsafe-extension-debugging") {
			t.Fatalf("%s: args = %v", goos, plan.args)
		}
		if hasArgPrefix(plan.args, "--remote-debugging-port=") {
			t.Fatalf("%s: pipe plan carries a port flag: %v", goos, plan.args)
		}
	}
}

func TestPlanBridgeLaunchNoExtNoBridge(t *testing.T) {
	for _, goos := range []string{"windows", "linux"} {
		plan := planBridgeLaunch(goos, "", 9222)
		if plan.route != bridgeRouteNone || plan.attachPipe || len(plan.args) != 0 {
			t.Fatalf("%s: plan = %+v, want an empty none plan", goos, plan)
		}
	}
}

func TestAllocLoopbackPort(t *testing.T) {
	port, err := allocLoopbackPort()
	if err != nil {
		t.Fatalf("alloc: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("port = %d", port)
	}
}

// Requirement (b): a bridge-load failure must never bubble
// into the browser launch. The driver the launch fires
// after Start() returns nothing at all, so containment is
// structural -- this test proves the worst realistic case
// (endpoint never comes up) terminates promptly and
// quietly instead of hanging or panicking.
func TestBridgeLoadViaPortUnreachableIsContained(t *testing.T) {
	dead, err := allocLoopbackPort() // free => nothing listens there
	if err != nil {
		t.Fatalf("alloc: %v", err)
	}
	old := cdpVersionWait
	cdpVersionWait = 300 * time.Millisecond
	defer func() { cdpVersionWait = old }()

	done := make(chan struct{})
	go func() {
		bridgeLoadViaPort(dead, "/nonexistent-ext", "chrome.exe")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("bridgeLoadViaPort hung on an unreachable endpoint")
	}
}
