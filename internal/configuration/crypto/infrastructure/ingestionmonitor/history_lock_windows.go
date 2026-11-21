//go:build windows

package ingestionmonitor

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func lockHistory(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("monitor history already locked: %w", err)
	}
	return file, nil
}

func syncHistoryDir(string) error { return nil }
