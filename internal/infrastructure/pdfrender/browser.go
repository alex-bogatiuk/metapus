package pdfrender

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/domain/printing"
)

type Browser struct {
	path  string
	slots chan struct{}
}

func NewBrowser(path string) (*Browser, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("PDF renderer requires its Linux container")
	}
	if os.Geteuid() == 0 {
		return nil, fmt.Errorf("PDF renderer must run as nonroot")
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return nil, err
	}
	return &Browser{path: resolved, slots: make(chan struct{}, 2)}, nil
}

func (b *Browser) Render(ctx context.Context, source []byte, options printing.PDFOptions) (result []byte, renderErr error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	select {
	case b.slots <- struct{}{}:
		defer func() { <-b.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	prepared, err := prepareHTML(source, options)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "metapus-pdf-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			result = nil
			renderErr = errors.Join(renderErr, fmt.Errorf("remove browser profile: %w", err))
		}
	}()
	input, output := filepath.Join(dir, "print.html"), filepath.Join(dir, "output.pdf")
	if err := os.WriteFile(input, prepared, 0600); err != nil {
		return nil, err
	}
	// The browser sandbox stays enabled. No root/no-sandbox escape hatch exists.
	cmd := exec.CommandContext(ctx, b.path,
		"--headless", "--disable-gpu", "--disable-background-networking", "--disable-extensions",
		"--no-first-run", "--no-default-browser-check", "--disable-sync", "--disable-breakpad",
		"--disable-dev-shm-usage", "--host-resolver-rules=MAP * ~NOTFOUND",
		"--user-data-dir="+filepath.Join(dir, "profile"), "--no-pdf-header-footer",
		"--print-to-pdf="+output, "file://"+filepath.ToSlash(input))
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.WaitDelay = time.Second
	cleanup := isolateBrowserProcess(cmd)
	defer cleanup()
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sandboxed browser failed: %w", err)
	}
	stat, err := os.Stat(output)
	if err != nil {
		return nil, err
	}
	if stat.Size() > MaxPDF {
		return nil, fmt.Errorf("PDF exceeds the supported size")
	}
	pdf, err := os.ReadFile(output)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		return nil, fmt.Errorf("invalid browser output")
	}
	return pdf, nil
}
