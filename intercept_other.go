//go:build !windows

package main

// ---------- v0.71 test3, layer 2: hook chassis (non-Windows) ----------
//
// No host windows exist off Windows, so the interception layer
// is inert: nothing installs, nothing degrades (there is
// nothing to degrade FROM -- the desired-state rule in
// intercept.go already gates on GOOS), and the patrol's
// reconcile is a no-op. The Linux build keeps compiling and
// behaving exactly as before.

import "time"

const interceptPlatformSupported = false

func interceptPlatformSetWanted(want bool) (bool, bool) { return false, false }
func interceptPlatformStop()                            {}
func interceptPlatformReinstall()                       {}
func interceptPlatformRefreshMetrics()                  {}
func interceptPlatformLastEvent() time.Time             { return time.Time{} }
