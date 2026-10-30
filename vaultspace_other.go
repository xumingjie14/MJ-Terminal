//go:build !windows

package main

import "syscall"

// diskFreeSpaceBytes returns the bytes available to an
// unprivileged process on the volume holding dir (statfs Bavail).
func diskFreeSpaceBytes(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
