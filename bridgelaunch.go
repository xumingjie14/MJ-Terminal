package main

// bridgelaunch.go -- v0.71 test2: which channel the bridge
// load rides for one browser launch, decided as a pure
// function of (platform, extension dir, debug port) so the
// decision itself is unit-testable on any OS.
//
// The routes:
//
//   - "pipe" (non-Windows): the DevTools pipe over child
//     fds 3/4, handed to the browser through
//     exec.Cmd.ExtraFiles (cdpload.go). This is the channel
//     Chromium's own tooling uses and the one
//     Extensions.loadUnpacked is documented to answer on.
//
//   - "port" (Windows): Go's standard library does NOT
//     support ExtraFiles on Windows -- Cmd.Start() fails
//     with "not supported by windows" before the process
//     even exists. v0.71-test1 attached pipe ends
//     unconditionally and EVERY isolated-browser launch on
//     the user's machine died that way. Windows therefore
//     launches with --remote-debugging-port=<loopback port
//     allocated for this launch> and the load is attempted
//     afterwards over a WebSocket (cdpws.go). The port is
//     ephemeral state of one launch: never persisted, never
//     in settings, never in user-facing text.
//
//   - "none": no extension released (or no usable port) --
//     the launch carries no bridge machinery at all.
//
// The invariant the tests pin: on Windows the plan NEVER
// asks for pipe ends, so the launch in main.go can never
// reach the ExtraFiles assignment there; and the bridge
// attempt is decoupled from the launch -- it runs after a
// successful Start() and its failures only ever reach the
// console log.

import (
	"fmt"
	"net"
)

const (
	bridgeRouteNone = "none"
	bridgeRoutePipe = "pipe"
	bridgeRoutePort = "port"
)

// bridgeLaunchPlan is the bridge slice of one browser
// launch: which route, the extra command-line flags it
// needs, and whether the caller must create CDP pipe ends
// and attach them to the child process (pipe route only).
type bridgeLaunchPlan struct {
	route      string
	args       []string
	attachPipe bool
	debugPort  int // port route only; 0 until a port is allocated
}

// bridgeRouteFor picks the channel for a platform. It is
// the single place the Windows/ExtraFiles fact lives.
func bridgeRouteFor(goos, extDir string) string {
	if extDir == "" {
		return bridgeRouteNone
	}
	if goos == "windows" {
		return bridgeRoutePort
	}
	return bridgeRoutePipe
}

// planBridgeLaunch renders the plan for one launch.
// debugPort is the already-allocated loopback port for the
// port route (0 = not allocated yet: the plan then carries
// the route but no port flag, and the caller must re-plan
// once it holds a real port -- a port flag is never emitted
// without one).
func planBridgeLaunch(goos, extDir string, debugPort int) bridgeLaunchPlan {
	plan := bridgeLaunchPlan{route: bridgeRouteFor(goos, extDir)}
	switch plan.route {
	case bridgeRoutePipe:
		plan.args = []string{"--enable-unsafe-extension-debugging", "--remote-debugging-pipe"}
		plan.attachPipe = true
	case bridgeRoutePort:
		plan.args = []string{"--enable-unsafe-extension-debugging"}
		if debugPort > 0 {
			plan.debugPort = debugPort
			plan.args = append(plan.args, fmt.Sprintf("--remote-debugging-port=%d", debugPort))
		}
	}
	return plan
}

// allocLoopbackPort reserves an ephemeral TCP port on
// 127.0.0.1 and releases it, returning the number for the
// browser to bind. (The usual tiny race -- someone else
// grabbing the number in between -- just makes the bridge
// load fail honestly in the log; the browser launch does
// not depend on it.)
func allocLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port, nil
}
