package main

// cdpws.go -- v0.71 test2: the Windows bridge-load route.
// Windows cannot receive the DevTools pipe (Go's
// exec.Cmd.ExtraFiles is unsupported there -- the
// v0.71-test1 incident, see cdpload.go), so the isolated
// browser is launched with --remote-debugging-port=<an
// ephemeral 127.0.0.1 port allocated for that launch> and
// the bridge load rides the DevTools WebSocket instead:
//
//  1. poll http://127.0.0.1:<port>/json/version until the
//     browser's DevTools endpoint answers with its
//     webSocketDebuggerUrl;
//  2. dial that WebSocket and run the same loadUnpacked
//     conversation as the pipe route (cdpCallLoadUnpacked).
//
// The WebSocket client is gorilla/websocket -- the stack
// this project already ships for the SSH terminal
// (ssh.go); no hand-rolled frame codec and no new
// dependency. Client masking, fragmentation and ping/pong
// are the library's job; one WS text message carries one
// DevTools JSON message, which is exactly the cdpMsgIO
// shape.
//
// Everything here runs AFTER the browser has started, in a
// goroutine the launch fires and forgets: any failure --
// endpoint never comes up (the launch handed off to an
// already-running instance whose debugging port is some
// earlier launch's, or the browser died), dial refused,
// the browser answering "Method not available" -- is
// logged truthfully and costs only the bridge for this
// session. The browser itself is never touched.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// errCDPWSClosed marks "the DevTools WebSocket ended
// before the browser answered".
var errCDPWSClosed = errors.New("调试连接已断开（浏览器可能已退出，或把档案交给了另一个实例）")

// cdpVersionWait bounds how long bridgeLoadViaPort waits
// for a freshly launched browser's DevTools endpoint to
// come up. A package variable so tests can shrink it.
var cdpVersionWait = 20 * time.Second

// cdpDevToolsWSURL polls the browser's DevTools HTTP
// endpoint until it answers /json/version, and returns
// its webSocketDebuggerUrl. wait is the overall budget;
// individual probes time out fast and retry.
func cdpDevToolsWSURL(port int, wait time.Duration) (string, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(wait)
	var lastErr error
	for {
		resp, err := client.Get(url)
		if err == nil {
			var body struct {
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			derr := json.NewDecoder(resp.Body).Decode(&body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && derr == nil && body.WebSocketDebuggerURL != "" {
				return body.WebSocketDebuggerURL, nil
			}
			lastErr = fmt.Errorf("调试接口应答异常（HTTP %d）", resp.StatusCode)
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("等待浏览器调试接口超时：%w", lastErr)
		}
		time.Sleep(400 * time.Millisecond)
	}
}

// wsMsgIO adapts a gorilla WebSocket connection to
// cdpMsgIO: one text message out per command, one message
// in per DevTools frame.
type wsMsgIO struct {
	conn *websocket.Conn
}

func (io *wsMsgIO) send(msg []byte) error {
	if err := io.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		return fmt.Errorf("%w：%v", errCDPWSClosed, err)
	}
	return nil
}

func (io *wsMsgIO) recv() (cdpFrame, error) {
	_, data, err := io.conn.ReadMessage()
	if err != nil {
		return cdpFrame{}, fmt.Errorf("%w：%v", errCDPWSClosed, err)
	}
	var f cdpFrame
	if err := json.Unmarshal(data, &f); err != nil {
		return cdpFrame{}, fmt.Errorf("调试接口应答解析失败：%w", err)
	}
	return f, nil
}

func (io *wsMsgIO) close() error { return io.conn.Close() }

// cdpDialWS opens the DevTools WebSocket. A variable so
// tests can substitute a scripted message channel.
var cdpDialWS = func(url string) (cdpMsgIO, error) {
	dialer := &websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	return &wsMsgIO{conn: conn}, nil
}

// cdpLoadUnpackedWS runs the shared loadUnpacked
// conversation over the browser's DevTools WebSocket.
func cdpLoadUnpackedWS(wsURL, path string, timeout time.Duration, maxAttempts int) (string, error) {
	mio, err := cdpDialWS(wsURL)
	if err != nil {
		return "", fmt.Errorf("连接浏览器调试接口失败：%w", err)
	}
	defer func() { _ = mio.close() }()
	return cdpCallLoadUnpacked(mio, path, timeout, maxAttempts)
}

// bridgeLoadViaPort is the port route's launch-path
// driver, the mirror of bridgeLoadViaCDP: it runs after a
// successful browser start and only ever logs. Its
// outcomes:
//
//   - success: the bridge is in, web notifications flow;
//   - the browser refusing the command (a protocol error,
//     e.g. "Method not available"): logged as exactly
//     that -- this browser could not load the bridge, web
//     notifications are unavailable with it, the browser
//     itself is unaffected. Never dressed up as success;
//   - the endpoint never answering / the socket dying:
//     logged with the reason; the bridge may already live
//     in a handed-off instance, which the launch check
//     (webNotifyLaunchCheck) arbitrates via the bridge's
//     own pings.
func bridgeLoadViaPort(port int, extDir, browserName string) {
	wsURL, err := cdpDevToolsWSURL(port, cdpVersionWait)
	if err != nil {
		logf("notify", "warn", "网页通知桥未能装入（浏览器：%s）：%v——本次网页通知不可用，浏览器本身不受影响", browserName, err)
		return
	}
	id, err := cdpLoadUnpackedWS(wsURL, extDir, 6*time.Second, 3)
	if err != nil {
		var ce *cdpError
		if errors.As(err, &ce) {
			logf("notify", "warn", "网页通知桥未能装入（浏览器：%s）：浏览器拒绝了装入请求（%s）——这个浏览器下网页通知暂不可用，浏览器本身不受影响", browserName, ce.Message)
			return
		}
		logf("notify", "warn", "网页通知桥未能装入（浏览器：%s）：%v——本次网页通知不可用，浏览器本身不受影响", browserName, err)
		return
	}
	logf("notify", "info", "网页通知桥已装入隔离浏览器（浏览器：%s，扩展 %s）", browserName, id)
}
