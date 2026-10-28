//go:build !windows

package main

// Tray stub off Windows: there is no host notification area to
// park an icon in, and the desktop view never exists here, so the
// close-to-tray state is unreachable in the first place. The
// state machine still runs (harmlessly) against these no-ops.
func platformTraySetVisible(v bool) {}
