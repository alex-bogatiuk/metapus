package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"metapus/internal/domain/printing"
	"metapus/internal/infrastructure/pdfrender"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		client, _ := pdfrender.NewClient("http://127.0.0.1:8081")
		return client.Ready(context.Background())
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	browser, err := pdfrender.NewBrowser("chromium")
	if err != nil {
		return err
	}
	handler := pdfrender.NewServer(browser)
	// Readiness proves that the shipped sandbox, browser and Cyrillic fonts can
	// actually render. A broken sandbox cannot be hidden by a live HTTP listener.
	if _, err := browser.Render(ctx, []byte("<!doctype html><meta charset=utf-8><p>Проверка печати</p>"), printing.PDFOptions{PaperSize: "A4"}); err != nil {
		return err
	}
	handler.SetReady(true)
	server := &http.Server{Addr: ":8081", Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		handler.SetReady(false)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}
