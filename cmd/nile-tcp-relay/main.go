// Command nile-tcp-relay is a narrow raw TCP relay for Docker Desktop hosts
// whose Linux VM cannot reach Nile directly. It never terminates TLS.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultListen   = "127.0.0.1:19443"
	defaultUpstream = "nile.trongrid.io:443"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("nile-tcp-relay", flag.ContinueOnError)
	listenAddr := fs.String("listen", defaultListen, "TCP listen address")
	upstreamAddr := fs.String("upstream", defaultUpstream, "fixed upstream address")
	allowedRaw := fs.String("allowed-cidrs", "127.0.0.0/8", "comma-separated client CIDRs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(*upstreamAddr), defaultUpstream) {
		return fmt.Errorf("upstream must be exactly %s", defaultUpstream)
	}
	allowed, err := parseCIDRs(*allowedRaw)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *listenAddr, err)
	}
	defer func() { _ = listener.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	log.Printf("Nile TLS relay listening on %s", listener.Addr())
	sem := make(chan struct{}, 64)
	var workers sync.WaitGroup
	defer workers.Wait()
	for {
		client, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil || errors.Is(acceptErr, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept client: %w", acceptErr)
		}
		if !allowedClient(client.RemoteAddr(), allowed) {
			_ = client.Close()
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			_ = client.Close()
			return nil
		}
		workers.Go(func() {
			defer func() { <-sem }()
			if err := relay(ctx, client, *upstreamAddr); err != nil && ctx.Err() == nil {
				log.Printf("relay connection failed: %v", err)
			}
		})
	}
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

func allowedClient(remote net.Addr, allowed []*net.IPNet) bool {
	host, _, err := net.SplitHostPort(remote.String())
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

func relay(ctx context.Context, client net.Conn, upstreamAddr string) error {
	defer func() { _ = client.Close() }()
	dialer := net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	upstream, err := dialer.DialContext(ctx, "tcp", upstreamAddr)
	if err != nil {
		return fmt.Errorf("connect fixed Nile upstream: %w", err)
	}
	defer func() { _ = upstream.Close() }()

	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
			_ = upstream.Close()
		case <-finished:
		}
	}()

	errs := make(chan error, 2)
	copyStream := func(dst, src net.Conn) {
		_, copyErr := io.Copy(dst, src)
		if tcp, ok := dst.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		errs <- copyErr
	}
	go copyStream(upstream, client)
	go copyStream(client, upstream)

	first := <-errs
	second := <-errs
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if first != nil && !errors.Is(first, net.ErrClosed) {
		return first
	}
	if second != nil && !errors.Is(second, net.ErrClosed) {
		return second
	}
	return nil
}
