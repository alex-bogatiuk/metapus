package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseTargetsRejectsBroadOrNonTLSDestinations(t *testing.T) {
	t.Parallel()

	targets, err := parseTargets("registry.npmjs.org:443,acme-v02.api.letsencrypt.org:443")
	require.NoError(t, err)
	require.Len(t, targets, 2)
	require.Error(t, func() error {
		_, parseErr := parseTargets("registry.npmjs.org:80")
		return parseErr
	}())
	require.Error(t, func() error {
		_, parseErr := parseTargets("127.0.0.1:443")
		return parseErr
	}())
}

func TestConnectHandlerAcknowledgesAndTunnelsAllowedTarget(t *testing.T) {
	t.Parallel()

	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = upstream.Close() }()

	proxy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = proxy.Close() }()

	target := upstream.Addr().String()
	_, allowedCIDR, err := net.ParseCIDR("127.0.0.0/8")
	require.NoError(t, err)
	server := &http.Server{
		Handler: &connectHandler{
			allowed: []*net.IPNet{allowedCIDR},
			targets: map[string]struct{}{target: {}},
		},
		ReadHeaderTimeout: 2 * time.Second,
	}
	defer func() { _ = server.Close() }()
	go func() { _ = server.Serve(proxy) }()

	upstreamPayload := make(chan string, 1)
	go func() {
		connection, acceptErr := upstream.Accept()
		if acceptErr != nil {
			upstreamPayload <- acceptErr.Error()
			return
		}
		defer func() { _ = connection.Close() }()
		_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
		payload := make([]byte, len("hello"))
		_, readErr := io.ReadFull(connection, payload)
		if readErr != nil {
			upstreamPayload <- readErr.Error()
			return
		}
		upstreamPayload <- string(payload)
	}()

	client, err := net.DialTimeout("tcp", proxy.Addr().String(), 5*time.Second)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = fmt.Fprintf(client, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	require.NoError(t, err)

	reader := bufio.NewReader(client)
	status, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "HTTP/1.1 200 Connection Established\r\n", status)
	for {
		line, readErr := reader.ReadString('\n')
		require.NoError(t, readErr)
		if line == "\r\n" {
			break
		}
	}
	_, err = client.Write([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, "hello", <-upstreamPayload)
}
