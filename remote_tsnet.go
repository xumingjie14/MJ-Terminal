//go:build (windows && amd64) || (linux && amd64)

package main

// tsnet backend for remote assistance (v0.33). The whole Tailscale
// node lives INSIDE the daemon as a library: userspace networking
// only -- no TUN driver, no system service, no admin rights, no
// installed client on the host PC. Its state directory is
// Data/tsnet, so the node's identity travels with the portable
// folder like everything else.
//
// Platforms without a tsnet build (notably windows/386, which
// upstream Tailscale does not support) compile remote_stub.go
// instead and report the feature as unsupported.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/tsnet"
)

const remotePlatformSupported = true

type tsnetBackend struct {
	mu     sync.Mutex
	srv    *tsnet.Server
	ln     net.Listener
	done   chan struct{}
	state  remoteNetState
	logged map[string]bool // one-shot transition logs
}

func newRemoteBackend() remoteBackend {
	return &tsnetBackend{state: remoteNetState{State: "off"}, logged: map[string]bool{}}
}

// Start brings the node up and serves the remote mux on the
// tailnet-only listener. It may block while the node has no
// address yet (nobody logged in), so the reconcile loop always
// calls it from a goroutine.
func (b *tsnetBackend) Start(handler http.Handler) error {
	b.mu.Lock()
	if b.srv != nil {
		b.mu.Unlock()
		return nil
	}
	dir := filepath.Join(dataDir(), "tsnet")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		b.mu.Unlock()
		return fmt.Errorf("创建 tsnet 状态目录失败：%w", err)
	}
	srv := &tsnet.Server{
		Dir:      dir,
		Hostname: "mj-terminal",
		Logf: func(format string, args ...any) {
			// tsnet's own diagnostics join the console stream
			// under the remote module, lightly prefixed.
			logf("remote", "info", "[tsnet] %s", fmt.Sprintf(format, args...))
		},
	}
	b.srv = srv
	b.done = make(chan struct{})
	b.setStateLocked(remoteNetState{State: "starting"})
	b.mu.Unlock()

	go b.watch()
	ln, err := srv.Listen("tcp", fmt.Sprintf(":%d", remotePort))
	if err != nil {
		b.mu.Lock()
		if b.srv == srv {
			b.srv = nil
			b.setStateLocked(remoteNetState{State: "error", Err: err.Error()})
		}
		b.mu.Unlock()
		return err
	}
	b.mu.Lock()
	if b.srv != srv {
		// Stopped again while Listen was blocked.
		b.mu.Unlock()
		_ = ln.Close()
		return fmt.Errorf("组网启动期间被停止")
	}
	b.ln = ln
	b.mu.Unlock()
	go func() { _ = http.Serve(ln, handler) }()
	return nil
}

func (b *tsnetBackend) Stop() {
	b.mu.Lock()
	srv, ln, done := b.srv, b.ln, b.done
	b.srv, b.ln, b.done = nil, nil, nil
	b.setStateLocked(remoteNetState{State: "off"})
	b.logged = map[string]bool{}
	b.mu.Unlock()
	if done != nil {
		close(done)
	}
	if ln != nil {
		_ = ln.Close()
	}
	if srv != nil {
		_ = srv.Close()
	}
}

func (b *tsnetBackend) setStateLocked(s remoteNetState) { b.state = s }

func (b *tsnetBackend) State() remoteNetState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// watch polls the local status while the node is up: it keeps the
// cached net state fresh for the Settings page (auth URL while
// login is pending, addresses once online) and logs the two
// transitions the user acts on -- "login link is ready" and "we
// are on the tailnet".
func (b *tsnetBackend) watch() {
	for {
		b.mu.Lock()
		done, srv := b.done, b.srv
		b.mu.Unlock()
		if srv == nil {
			return
		}
		select {
		case <-done:
			return
		case <-time.After(2 * time.Second):
		}
		lc, err := srv.LocalClient()
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		st, err := lc.Status(ctx)
		cancel()
		if err != nil || st == nil {
			continue
		}
		ns := remoteNetState{State: "starting"}
		// ipnstate carries BackendState as the ipn.State's String()
		// form, so compare against the constants' string forms.
		switch st.BackendState {
		case ipn.NeedsLogin.String(), ipn.NeedsMachineAuth.String():
			ns.State = "needslogin"
			ns.AuthURL = st.AuthURL
		case ipn.Running.String():
			ns.State = "online"
			for _, a := range st.TailscaleIPs {
				ns.IPs = append(ns.IPs, a.String())
			}
			if st.Self != nil {
				ns.Device = strings.TrimSuffix(st.Self.DNSName, ".")
				if ns.Device == "" {
					ns.Device = st.Self.HostName
				}
			}
		case ipn.Stopped.String(), ipn.NoState.String():
			ns.State = "starting"
		}
		b.mu.Lock()
		if b.srv == srv {
			b.setStateLocked(ns)
			if ns.State == "needslogin" && !b.logged["needslogin"] {
				b.logged["needslogin"] = true
				b.mu.Unlock()
				logf("remote", "info", "远程协助等待登录：在终端的 设置 → 安全与隐私 里打开登录链接，用你自己的 Tailscale 账号登录一次即可")
				continue
			}
			if ns.State == "online" && !b.logged["online"] {
				b.logged["online"] = true
				ips := strings.Join(ns.IPs, ", ")
				dev := ns.Device
				b.mu.Unlock()
				logf("remote", "info", "远程协助已组网：设备名 %s，tailnet 地址 %s", dev, ips)
				continue
			}
		}
		b.mu.Unlock()
	}
}

// WhoIs resolves a tailnet source address to the peer's node
// identity via the local API. The StableID is the pairing
// identity: it survives node-key rotation, so an operator paired
// once stays paired.
func (b *tsnetBackend) WhoIs(remoteAddr string) (remotePeer, error) {
	b.mu.Lock()
	srv := b.srv
	b.mu.Unlock()
	if srv == nil {
		return remotePeer{}, fmt.Errorf("组网未启动")
	}
	lc, err := srv.LocalClient()
	if err != nil {
		return remotePeer{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := lc.WhoIs(ctx, remoteAddr)
	if err != nil {
		return remotePeer{}, err
	}
	if resp == nil || resp.Node == nil {
		return remotePeer{}, fmt.Errorf("来源不在 tailnet 内")
	}
	n := resp.Node
	device := strings.TrimSuffix(n.Name, ".")
	name := device
	if i := strings.IndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	if name == "" {
		name = n.ComputedName
	}
	if name == "" {
		name = "未知设备"
	}
	id := string(n.StableID)
	keyStr := n.Key.String()
	if id == "" {
		id = keyStr // very old nodes: fall back to the node key itself
	}
	return remotePeer{ID: id, Name: name, Device: device, KeyFP: keyStr}, nil
}
