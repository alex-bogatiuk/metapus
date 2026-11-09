//go:build !linux

package pdfrender

import "os/exec"

// NewBrowser rejects other platforms; keep the server and adapter buildable there.
func isolateBrowserProcess(*exec.Cmd) func() { return func() {} }
