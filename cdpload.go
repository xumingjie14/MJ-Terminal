package main

// cdpload.go -- v0.71 (detail-fix ⑧ lower half, block 1): load
// the web-notification bridge extension into branded Chrome
// through the DevTools protocol.
//
// Why this file exists: branded Google Chrome removed
// --load-extension in Chrome 137 ("--load-extension is not
// allowed in Google Chrome, ignoring."), so the v0.68 bridge
// never loaded on the user's machine. The sanctioned
// replacement is the CDP command Extensions.loadUnpacked,
// gated by the --enable-unsafe-extension-debugging launch
// flag. bridgelaunch.go picks the transport per platform:
//
//   - non-Windows: --remote-debugging-pipe. The browser
//     reads JSON commands from fd 3 of the child process
//     and writes responses to fd 4; every message is one
//     JSON object terminated by a NUL byte. The daemon
//     hands the pipe ends to the child through
//     exec.Cmd.ExtraFiles (see launchIsolatedBrowser), so
//     no TCP port is opened at all.
//
//   - Windows: exec.Cmd.ExtraFiles is NOT supported by
//     Go's standard library there -- Cmd.Start() fails
//     outright ("not supported by windows"). v0.71-test1
//     attached the pipe ends unconditionally and every
//     browser launch on the user's Windows machine died
//     before Chrome even started. Windows therefore uses
//     a loopback --remote-debugging-port and speaks CDP
//     over a WebSocket instead (cdpws.go).
//
// The command conversation itself (one loadUnpacked call,
// response matched by id, event frames skipped, protocol
// errors final, timeouts resent) is transport-agnostic and
// lives in cdpCallLoadUnpacked over the cdpMsgIO seam, so
// both transports share it and the fake-transport tests pin
// it once.
//
// --load-extension stays on the command line as well: Edge
// and Chromium builds still honour it, and on branded Chrome
// it is simply ignored. loadUnpacked registers the
// extension in the profile's Secure Preferences, so later
// launches keep it even before this session runs.
//
// A bridge-load failure NEVER fails the browser launch:
// both drivers below only log their outcome.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// cdpTransport is the byte stream the DevTools pipe speaks
// over: an interface so unit tests can script a fake browser.
type cdpTransport interface {
	io.Reader
	io.Writer
	io.Closer
}

// cdpPipeTransport adapts the two *os.File pipe ends the
// daemon keeps for itself (it writes commands to w, the
// browser's responses arrive on r).
type cdpPipeTransport struct {
	r, w *os.File
}

func (t *cdpPipeTransport) Read(p []byte) (int, error)  { return t.r.Read(p) }
func (t *cdpPipeTransport) Write(p []byte) (int, error) { return t.w.Write(p) }
func (t *cdpPipeTransport) Close() error {
	_ = t.r.Close()
	_ = t.w.Close()
	return nil
}

// newCDPPipeTransport creates the two OS pipes for one
// browser launch and returns the parent's transport plus the
// child ends, in ExtraFiles order: index 0 becomes the
// child's fd 3 (the browser READS commands from it), index 1
// becomes fd 4 (the browser WRITES responses to it). The
// caller closes the child ends once the process has started.
// Only ever called when the launch plan asks for pipe ends,
// which bridgelaunch.go guarantees never happens on Windows.
func newCDPPipeTransport() (*cdpPipeTransport, []*os.File, error) {
	cmdInR, cmdInW, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	cmdOutR, cmdOutW, err := os.Pipe()
	if err != nil {
		_ = cmdInR.Close()
		_ = cmdInW.Close()
		return nil, nil, err
	}
	return &cdpPipeTransport{r: cmdOutR, w: cmdInW}, []*os.File{cmdInR, cmdOutW}, nil
}

// errCDPPipeClosed marks "the pipe ended before the browser
// answered": almost always the launched process handed the
// profile off to an already-running instance and exited (the
// bridge then lives -- or not -- in THAT instance), or the
// browser died during startup.
var errCDPPipeClosed = errors.New("调试管道已关闭（浏览器进程可能已把档案交给一个正在运行的实例，或已退出）")

// cdpError is a DevTools protocol error object.
type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string { return fmt.Sprintf("CDP 错误 %d：%s", e.Code, e.Message) }

// cdpFrame is one decoded DevTools message. Frames with no
// ID are protocol events (the loader skips them); a response
// carries either Result or Error.
type cdpFrame struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *cdpError       `json:"error"`
}

// cdpCommandJSON renders one command message as bare JSON.
// Each transport frames it its own way: the pipe appends a
// NUL terminator, a WebSocket message needs nothing more.
func cdpCommandJSON(id int, method string, params any) []byte {
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	b, _ := json.Marshal(msg)
	return b
}

// cdpEncode renders one PIPE command message: the JSON
// object plus the NUL terminator the pipe framing requires.
func cdpEncode(id int, method string, params any) []byte {
	return append(cdpCommandJSON(id, method, params), 0)
}

// cdpFrameReader splits the pipe's byte stream back into
// NUL-terminated frames. It tolerates a frame split across
// any number of reads and several frames arriving in one.
type cdpFrameReader struct {
	r   io.Reader
	buf []byte
}

func newCDPFrameReader(r io.Reader) *cdpFrameReader { return &cdpFrameReader{r: r} }

// next returns the next complete frame. io.EOF (or any read
// error) means the browser side of the pipe is gone.
func (fr *cdpFrameReader) next() (cdpFrame, error) {
	for {
		if i := indexByte(fr.buf, 0); i >= 0 {
			raw := fr.buf[:i]
			fr.buf = fr.buf[i+1:]
			if len(raw) == 0 {
				continue // stray terminator: skip
			}
			var f cdpFrame
			if err := json.Unmarshal(raw, &f); err != nil {
				return cdpFrame{}, fmt.Errorf("CDP 帧解析失败：%w", err)
			}
			return f, nil
		}
		chunk := make([]byte, 4096)
		n, err := fr.r.Read(chunk)
		if n > 0 {
			fr.buf = append(fr.buf, chunk[:n]...)
		}
		if err != nil {
			return cdpFrame{}, err
		}
	}
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// cdpMsgIO is one DevTools message channel, framing and
// all: the seam both transports (pipe, WebSocket) plug
// into, and the seam the tests script. send takes one bare
// JSON command; recv returns the next decoded message. An
// error from either means the channel is gone, and the
// error value itself says which transport died (each
// adapter wraps its own sentinel).
type cdpMsgIO interface {
	send(msg []byte) error
	recv() (cdpFrame, error)
	close() error
}

// pipeMsgIO adapts a cdpTransport (the DevTools pipe):
// sends gain the NUL terminator, receives come from the
// frame reader, and every failure collapses into
// errCDPPipeClosed -- the semantics the pipe loader has
// always reported.
type pipeMsgIO struct {
	t  cdpTransport
	fr *cdpFrameReader
}

func newPipeMsgIO(t cdpTransport) *pipeMsgIO {
	return &pipeMsgIO{t: t, fr: newCDPFrameReader(t)}
}

func (io *pipeMsgIO) send(msg []byte) error {
	if _, err := io.t.Write(append(msg, 0)); err != nil {
		return errCDPPipeClosed
	}
	return nil
}

func (io *pipeMsgIO) recv() (cdpFrame, error) {
	f, err := io.fr.next()
	if err != nil {
		return cdpFrame{}, errCDPPipeClosed
	}
	return f, nil
}

func (io *pipeMsgIO) close() error { return io.t.Close() }

// cdpCallLoadUnpacked issues Extensions.loadUnpacked for
// path on an open message channel and waits (up to timeout
// per attempt) for the matching response. On success it
// returns the extension id the browser assigned. Retries: a
// timeout resends the command (the browser may still be
// bringing its DevTools handler up), at most maxAttempts
// sends total; a protocol error or a dead channel is final.
func cdpCallLoadUnpacked(mio cdpMsgIO, path string, timeout time.Duration, maxAttempts int) (string, error) {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	type readRes struct {
		f   cdpFrame
		err error
	}
	frames := make(chan readRes, 1)
	go func() {
		for {
			f, err := mio.recv()
			frames <- readRes{f, err}
			if err != nil {
				return
			}
		}
	}()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := mio.send(cdpCommandJSON(1, "Extensions.loadUnpacked", map[string]any{"path": path})); err != nil {
			return "", err
		}
		deadline := time.NewTimer(timeout)
		for {
			select {
			case rr := <-frames:
				if rr.err != nil {
					return "", rr.err
				}
				if rr.f.ID != 1 {
					continue // an event or another command's frame: keep waiting
				}
				if rr.f.Error != nil {
					return "", rr.f.Error
				}
				var res struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(rr.f.Result, &res); err != nil || res.ID == "" {
					return "", fmt.Errorf("CDP loadUnpacked 应答无法解析：%s", string(rr.f.Result))
				}
				return res.ID, nil
			case <-deadline.C:
				goto resend // attempt timed out: resend while attempts remain
			}
		}
	resend:
		deadline.Stop()
	}
	return "", fmt.Errorf("CDP loadUnpacked 超时（%d 次尝试无应答）", maxAttempts)
}

// cdpLoadUnpacked is the pipe transport's spelling of the
// shared conversation (kept as the entry the pipe tests
// exercise).
func cdpLoadUnpacked(t cdpTransport, path string, timeout time.Duration, maxAttempts int) (string, error) {
	return cdpCallLoadUnpacked(newPipeMsgIO(t), path, timeout, maxAttempts)
}

// bridgeLoadViaCDP is the pipe route's launch-path driver:
// it runs the load over the pipe of a just-started browser
// and reports the outcome to the console (module "notify",
// the bridge's home). The transport is always closed when
// it returns. A failure never reaches the browser launch
// itself -- it only means this session has no bridge, which
// the launch check (webNotifyLaunchCheck) then diagnoses as
// before, now with this loader's precise reason already in
// the log above it.
func bridgeLoadViaCDP(t cdpTransport, extDir, browserName string) {
	defer t.Close()
	id, err := cdpLoadUnpacked(t, extDir, 6*time.Second, 3)
	if err != nil {
		if errors.Is(err, errCDPPipeClosed) {
			logf("notify", "info", "网页通知桥：%s（浏览器：%s）——若桥未在既有会话中，本次网页通知不可用，下次完全退出浏览器再打开时会重试", err, browserName)
			return
		}
		logf("notify", "warn", "网页通知桥加载失败（浏览器：%s）：%v", browserName, err)
		return
	}
	logf("notify", "info", "网页通知桥已通过 DevTools 协议装入隔离浏览器（浏览器：%s，扩展 %s）", browserName, id)
}
