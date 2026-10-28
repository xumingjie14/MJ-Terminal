//go:build !((windows && amd64) || (linux && amd64))

package main

// Stub backend for platforms upstream Tailscale does not support
// (notably windows/386): remote assistance compiles in, the
// settings page explains itself, and nothing ever listens.

import (
	"fmt"
	"net/http"
)

const remotePlatformSupported = false

type stubBackend struct{}

func newRemoteBackend() remoteBackend { return stubBackend{} }

func (stubBackend) Start(http.Handler) error {
	return fmt.Errorf("当前平台不支持远程协助（需要 64 位构建）")
}

func (stubBackend) Stop() {}

func (stubBackend) State() remoteNetState {
	return remoteNetState{State: "unsupported", Err: "当前平台不支持远程协助（需要 64 位构建）"}
}

func (stubBackend) WhoIs(string) (remotePeer, error) {
	return remotePeer{}, fmt.Errorf("当前平台不支持远程协助")
}
