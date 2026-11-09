// Command public-edge-proxy preserves the original client address across
// Docker Desktop's host-port NAT. It forwards raw TCP and prepends a PROXY
// protocol v1 header; TLS is still terminated by the containerized Nginx.
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultHTTPListen    = "0.0.0.0:80"
	defaultHTTPUpstream  = "127.0.0.1:18080"
	defaultHTTPSListen   = "0.0.0.0:443"
	defaultHTTPSUpstream = "127.0.0.1:18443"
)

type endpoint struct {
	name     string
	listen   string
	upstream string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("public-edge-proxy", flag.ContinueOnError)
	httpListen := fs.String("http-listen", defaultHTTPListen, "public HTTP listen address")
	httpUpstream := fs.String("http-upstream", defaultHTTPUpstream, "loopback HTTP upstream")
	httpsListen := fs.String("https-listen", defaultHTTPSListen, "public HTTPS listen address")
	httpsUpstream := fs.String("https-upstream", defaultHTTPSUpstream, "loopback HTTPS upstream")
	maxConnections := fs.Int("max-connections", 512, "maximum concurrent connections")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *maxConnections < 1 || *maxConnections > 4096 {
		return errors.New("max-connections must be between 1 and 4096")
	}

	endpoints := []endpoint{
		{name: "HTTP", listen: *httpListen, upstream: *httpUpstream},
		{name: "HTTPS", listen: *httpsListen, upstream: *httpsUpstream},
	}
	for _, ep := range endpoints {
		if err := validateLoopbackUpstream(ep.upstream); err != nil {
			return fmt.Errorf("%s upstream: %w", ep.name, err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sem := make(chan struct{}, *maxConnections)
	errs := make(chan error, len(endpoints))
	var listeners sync.WaitGroup
	for _, ep := range endpoints {
		listeners.Go(func() {
			errs <- serveEndpoint(ctx, ep, sem)
		})
	}

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errs:
		stop()
	}
	listeners.Wait()
	if runErr != nil {
		return runErr
	}
	return nil
}

func serveEndpoint(ctx context.Context, ep endpoint, sem chan struct{}) error {
	listener, err := net.Listen("tcp", ep.listen)
	if err != nil {
		return fmt.Errorf("listen for %s on %s: %w", ep.name, ep.listen, err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	log.Printf("%s edge listening on %s and forwarding to %s", ep.name, listener.Addr(), ep.upstream)

	var workers sync.WaitGroup
	defer workers.Wait()
	for {
		client, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil || errors.Is(acceptErr, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept %s client: %w", ep.name, acceptErr)
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			_ = client.Close()
			return nil
		}
		workers.Go(func() {
			defer func() { <-sem }()
			if relayErr := relay(ctx, client, ep.upstream); relayErr != nil && ctx.Err() == nil {
				log.Printf("%s edge connection failed: %v", ep.name, relayErr)
			}
		})
	}
}

func validateLoopbackUpstream(address string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("invalid address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("address %q must use a numeric loopback IP", address)
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil || port == "0" {
		return fmt.Errorf("address %q has an invalid port", address)
	}
	return nil
}

func relay(ctx context.Context, client net.Conn, upstreamAddress string) error {
	defer func() { _ = client.Close() }()
	dialer := net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	upstream, err := dialer.DialContext(ctx, "tcp", upstreamAddress)
	if err != nil {
		return fmt.Errorf("connect loopback upstream: %w", err)
	}
	defer func() { _ = upstream.Close() }()

	header, err := proxyHeader(client.RemoteAddr(), client.LocalAddr())
	if err != nil {
		return err
	}
	if err := writeAll(upstream, header); err != nil {
		return fmt.Errorf("write PROXY header: %w", err)
	}

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

	select {
	case <-ctx.Done():
		return ctx.Err()
	case copyErr := <-errs:
		if copyErr != nil && !errors.Is(copyErr, net.ErrClosed) {
			return copyErr
		}
		return nil
	}
}

func proxyHeader(remote, local net.Addr) ([]byte, error) {
	source, ok := remote.(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("unsupported remote address %T", remote)
	}
	destination, ok := local.(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("unsupported local address %T", local)
	}

	family := "TCP6"
	sourceIP := source.IP
	destinationIP := destination.IP
	if sourceIP.To4() != nil && destinationIP.To4() != nil {
		family = "TCP4"
		sourceIP = sourceIP.To4()
		destinationIP = destinationIP.To4()
	} else if sourceIP.To16() == nil || destinationIP.To16() == nil {
		return nil, errors.New("invalid TCP address in PROXY header")
	}

	line := fmt.Sprintf(
		"PROXY %s %s %s %d %d\r\n",
		family,
		sourceIP.String(),
		destinationIP.String(),
		source.Port,
		destination.Port,
	)
	return []byte(line), nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrUnexpectedEOF
		}
		data = data[written:]
	}
	return nil
}
