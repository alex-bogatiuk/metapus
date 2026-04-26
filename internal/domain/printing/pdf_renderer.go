package printing

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// RenderPDF converts the already-rendered HTML print form to a PDF
// using the system-installed Chromium-based browser (Edge on Windows, Chrome/Chromium on Linux).
// This produces pixel-perfect output identical to what the user sees in the browser.
func RenderPDF(w io.Writer, htmlContent []byte) error {
	browserPath, err := findBrowser()
	if err != nil {
		return err
	}

	// Write HTML to a temp file (headless Chrome needs a file:// URL)
	tmpDir, err := os.MkdirTemp("", "metapus-pdf-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	htmlPath := filepath.Join(tmpDir, "print.html")
	if err := os.WriteFile(htmlPath, htmlContent, 0644); err != nil {
		return fmt.Errorf("write HTML temp file: %w", err)
	}

	pdfPath := filepath.Join(tmpDir, "output.pdf")

	// Run headless browser with --print-to-pdf
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []string{
		"--headless",
		"--disable-gpu",
		"--disable-javascript",                  // Prevent script execution in user-controlled HTML
		"--disable-software-rasterizer",
		"--run-all-compositor-stages-before-draw",
		"--print-to-pdf=" + pdfPath,
		"--no-pdf-header-footer",
		"--print-to-pdf-no-header",
	}
	// --no-sandbox only when explicitly opted in (e.g., Docker root user).
	// In production prefer running Chrome as non-root with seccomp profile.
	if os.Getenv("CHROME_NO_SANDBOX") == "true" {
		args = append(args, "--no-sandbox")
	}
	args = append(args, "file://"+filepath.ToSlash(htmlPath))

	cmd := exec.CommandContext(ctx, browserPath, args...)
	cmd.Stderr = io.Discard
	cmd.Stdout = io.Discard

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("browser print-to-pdf failed: %w", err)
	}

	// Read the generated PDF
	pdfBytes, err := os.ReadFile(pdfPath)
	if err != nil {
		return fmt.Errorf("read generated PDF: %w", err)
	}

	_, err = w.Write(pdfBytes)
	return err
}

// findBrowser locates a Chromium-based browser on the system.
func findBrowser() (string, error) {
	// Check environment override first
	if p := os.Getenv("CHROME_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	var candidates []string
	switch runtime.GOOS {
	case "windows":
		candidates = []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("LocalAppData"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
		}
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default: // linux
		candidates = []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
		}
		// On Linux, also check PATH via LookPath
		for _, name := range candidates {
			if p, err := exec.LookPath(name); err == nil {
				return p, nil
			}
		}
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("no Chromium-based browser found; set CHROME_PATH environment variable")
}
