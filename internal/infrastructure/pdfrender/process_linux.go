package pdfrender

import (
	"os/exec"
	"syscall"
)

// Own a process group so cancellation kills Chromium's renderer/utility children
// as well as its parent, before the request's private directory is removed.
func isolateBrowserProcess(cmd *exec.Cmd) func() {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	kill := func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.Cancel = kill
	return func() { _ = kill() }
}
