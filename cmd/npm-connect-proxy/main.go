// Command npm-connect-proxy is a restricted HTTP CONNECT proxy for Docker
// Desktop workloads whose Linux VM cannot reliably reach selected TLS APIs.
// TLS remains end-to-end between the client and explicitly allowed targets.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultProxyListen   = "0.0.0.0:19444"
	defaultAllowedTarget = "registry.npmjs.org:443"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("npm-connect-proxy", flag.ContinueOnError)
	listenAddr := fs.String("listen", defaultProxyListen, "HTTP proxy listen address")
	allowedRaw := fs.String("allowed-cidrs", "127.0.0.0/8", "comma-separated client CIDRs")
	targetsRaw := fs.String("allowed-targets", defaultAllowedTarget, "comma-separated TLS host:port targets")
	if err := fs.Parse(args); err != nil {
		return err
	}
	allowed, err := parseCIDRs(*allowedRaw)
	if err != nil {
		return err
	}
	targets, err := parseTargets(*targetsRaw)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *listenAddr, err)
	}
	defer func() { _ = listener.Close() }()

	handler := &connectHandler{allowed: allowed, targets: targets}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("restricted TLS CONNECT proxy listening on %s", listener.Addr())
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

type connectHandler struct {
	allowed []*net.IPNet
	targets map[string]struct{}
}

func (h *connectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !allowedClient(r.RemoteAddr, h.allowed) {
		log.Printf("rejected client %s outside allowed CIDRs", r.RemoteAddr)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	target := strings.ToLower(strings.TrimSpace(r.Host))
	if r.Method != http.MethodConnect {
		log.Printf("rejected non-CONNECT request")
		http.Error(w, "only CONNECT is allowed", http.StatusForbidden)
		return
	}
	if _, ok := h.targets[target]; !ok {
		log.Printf("rejected CONNECT target %q", target)
		http.Error(w, "CONNECT target is not allowed", http.StatusForbidden)
		return
	}
	upstream, err := dialTarget(r.Context(), target)
	if err != nil {
		log.Printf("allowed target %q is unavailable: %v", target, err)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		log.Printf("HTTP hijacking is unavailable for target %q", target)
		http.Error(w, "proxy unavailable", http.StatusInternalServerError)
		return
	}
	client, _, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		log.Printf("failed to hijack CONNECT client for target %q: %v", target, err)
		return
	}
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		_ = client.Close()
		_ = upstream.Close()
		log.Printf("failed to acknowledge CONNECT for target %q: %v", target, err)
		return
	}
	log.Printf("CONNECT established for allowed target %q", target)
	go tunnel(client, upstream)
}

func dialTarget(ctx context.Context, target string) (net.Conn, error) {
	var lastErr error
	for attempt := range 4 {
		dialer := net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		connection, err := dialer.DialContext(ctx, "tcp", target)
		if err == nil {
			return connection, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastErr
}

func parseTargets(raw string) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	for value := range strings.SplitSeq(raw, ",") {
		value = strings.ToLower(strings.TrimSpace(value))
		host, port, err := net.SplitHostPort(value)
		if err != nil || host == "" || port != "443" {
			return nil, fmt.Errorf("invalid allowed TLS target %q", value)
		}
		if net.ParseIP(host) != nil {
			return nil, fmt.Errorf("allowed TLS target %q must use a DNS name", value)
		}
		result[value] = struct{}{}
	}
	if len(result) == 0 {
		return nil, errors.New("at least one allowed TLS target is required")
	}
	return result, nil
}

func tunnel(client, upstream net.Conn) {
	defer func() { _ = client.Close() }()
	defer func() { _ = upstream.Close() }()
	var workers sync.WaitGroup
	workers.Add(2)
	copyStream := func(dst, src net.Conn) {
		defer workers.Done()
		_, _ = io.Copy(dst, src)
		if tcp, ok := dst.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}
	go copyStream(upstream, client)
	go copyStream(client, upstream)
	workers.Wait()
}

func parseCIDRs(raw string) ([]*net.IPNet, error) {
	var result []*net.IPNet
	for value := range strings.SplitSeq(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		_, cidr, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("invalid allowed CIDR %q: %w", value, err)
		}
		result = append(result, cidr)
	}
	if len(result) == 0 {
		return nil, errors.New("at least one allowed client CIDR is required")
	}
	return result, nil
}

func allowedClient(remote string, allowed []*net.IPNet) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	for _, cidr := range allowed {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}
