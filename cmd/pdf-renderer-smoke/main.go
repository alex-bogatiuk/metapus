// pdf-renderer-smoke runs the same container check locally and in CI.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain/printing"
	"metapus/internal/infrastructure/pdfrender"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var err error
	if len(os.Args) == 2 && os.Args[1] == "verify" {
		err = verify(ctx)
	} else {
		err = runContainer(ctx)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("PDF smoke passed: sandbox, Cyrillic, exact amounts, active content, cleanup and shutdown")
}

func command(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func runContainer(ctx context.Context) error {
	name := "metapus-pdf-smoke-" + id.New().String()
	image := name + ":test"
	// All names are generated here; no existing user container/image is removed.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanup, "docker", "rm", "-f", name).Run()
		_ = exec.CommandContext(cleanup, "docker", "image", "rm", image).Run()
	}()
	if err := command(ctx, "docker", "build", "--target", "smoke", "-f", "deployments/pdf-renderer/Dockerfile", "-t", image, "."); err != nil {
		return fmt.Errorf("build PDF smoke image: %w", err)
	}
	return command(ctx, "docker", "run", "--rm", "--name", name,
		"--label", "metapus.task=pdf-smoke", "--network", "none", "--init", "--read-only",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=256m", "--pids-limit", "256", "--memory", "512m",
		"--security-opt", "seccomp=deployments/pdf-renderer/seccomp_profile.json", image, "verify")
}

func verify(ctx context.Context) error {
	if os.Geteuid() == 0 {
		return fmt.Errorf("smoke must run as nonroot")
	}
	server := exec.CommandContext(ctx, "/usr/local/bin/pdf-renderer")
	server.Stdout, server.Stderr = os.Stdout, os.Stderr
	if err := server.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- server.Wait() }()
	defer func() { _ = server.Process.Kill() }()
	client, err := pdfrender.NewClient("http://127.0.0.1:8081")
	if err != nil {
		return err
	}
	ready, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for client.Ready(ready) != nil {
		select {
		case err := <-done:
			return fmt.Errorf("renderer exited before ready: %v", err)
		case <-ready.Done():
			return fmt.Errorf("renderer readiness: %w", ready.Err())
		case <-tick.C:
		}
	}
	const content = `<!doctype html><html><head><style>body{font:16px sans-serif;background-image:url(http://127.0.0.1:8081/forbidden)} </style></head><body onload="document.body.innerText='SCRIPT_EXECUTED'"><h1>Проверка печати</h1><p>Сумма 9007199.254740993</p><script>document.body.innerText='SCRIPT_EXECUTED'</script><iframe src="file:///etc/passwd"></iframe><img src="http://127.0.0.1:8081/forbidden" onerror="document.body.innerText='SCRIPT_EXECUTED'"></body></html>`
	for i := 0; i < 3; i++ {
		pdf, err := client.Render(ctx, []byte(content), printing.PDFOptions{PaperSize: "A4"})
		if err != nil {
			return fmt.Errorf("render %d: %w", i, err)
		}
		if err := verifyPDF(ctx, pdf); err != nil {
			return err
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8081/render", strings.NewReader(`{"url":"file:///etc/passwd"}`))
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		return fmt.Errorf("URL navigation was accepted")
	}
	remaining, err := filepath.Glob("/tmp/metapus-pdf-*")
	if err != nil || len(remaining) != 0 {
		return fmt.Errorf("renderer left temporary directories: %v, %v", remaining, err)
	}
	if err := server.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
	case <-time.After(12 * time.Second):
		return fmt.Errorf("renderer did not drain shutdown")
	}
	// docker --init reaps terminated grandchildren. No live browser may survive.
	processes, err := filepath.Glob("/proc/[0-9]*/comm")
	if err != nil {
		return err
	}
	for _, path := range processes {
		name, _ := os.ReadFile(path)
		if strings.Contains(string(name), "chrom") {
			status, _ := os.ReadFile(filepath.Join(filepath.Dir(path), "status"))
			if !bytes.Contains(status, []byte("State:\tZ")) {
				return fmt.Errorf("browser process survived shutdown: %s", name)
			}
		}
	}
	return nil
}

func verifyPDF(ctx context.Context, pdf []byte) (resultErr error) {
	file, err := os.CreateTemp("", "smoke-*.pdf")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, os.Remove(file.Name())) }()
	if _, err := file.Write(pdf); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	text, err := exec.CommandContext(ctx, "pdftotext", "-enc", "UTF-8", file.Name(), "-").Output()
	if err != nil {
		return fmt.Errorf("read PDF text: %w", err)
	}
	for _, expected := range []string{"Проверка печати", "Сумма 9007199.254740993"} {
		if !bytes.Contains(text, []byte(expected)) {
			return fmt.Errorf("PDF lost text %q: %s", expected, text)
		}
	}
	for _, forbidden := range []string{"SCRIPT_EXECUTED", "root:x:"} {
		if bytes.Contains(text, []byte(forbidden)) {
			return fmt.Errorf("active content reached PDF: %s", forbidden)
		}
	}
	return nil
}
