//go:build windows

package main

import "golang.org/x/sys/windows"

// diskFreeSpaceBytes returns the bytes available to this process
// on the volume holding dir (GetDiskFreeSpaceEx, caller view).
func diskFreeSpaceBytes(dir string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, err
	}
	return free, nil
}
