package main

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateLoopbackUpstream(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateLoopbackUpstream("127.0.0.1:18080"))
	require.NoError(t, validateLoopbackUpstream("[::1]:18443"))
	require.Error(t, validateLoopbackUpstream("localhost:18080"))
	require.Error(t, validateLoopbackUpstream("192.0.2.10:18080"))
	require.Error(t, validateLoopbackUpstream("127.0.0.1:0"))
}

func TestProxyHeader(t *testing.T) {
	t.Parallel()

	header, err := proxyHeader(
		&net.TCPAddr{IP: net.ParseIP("192.0.2.25"), Port: 54321},
		&net.TCPAddr{IP: net.ParseIP("198.51.100.10"), Port: 443},
	)
	require.NoError(t, err)
	require.Equal(t, "PROXY TCP4 192.0.2.25 198.51.100.10 54321 443\r\n", string(header))
}

func TestRelayPrependsProxyHeaderAndPreservesPayload(t *testing.T) {
	t.Parallel()

	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = upstream.Close() }()

	received := make(chan string, 1)
	go func() {
		conn, acceptErr := upstream.Accept()
		if acceptErr != nil {
			received <- acceptErr.Error()
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		data, readErr := io.ReadAll(io.LimitReader(conn, 128))
		if readErr != nil {
			received <- readErr.Error()
			return
		}
		received <- string(data)
	}()

	clientSide, relaySide := net.Pipe()
	relayDone := make(chan error, 1)
	go func() {
		relayDone <- relay(context.Background(), &tcpAddressConn{
			Conn:   relaySide,
			remote: &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 45678},
			local:  &net.TCPAddr{IP: net.ParseIP("198.51.100.9"), Port: 443},
		}, upstream.Addr().String())
	}()

	_, err = clientSide.Write([]byte("payload"))
	require.NoError(t, err)
	require.NoError(t, clientSide.Close())
	require.NoError(t, <-relayDone)

	value := <-received
	require.True(t, strings.HasPrefix(value, "PROXY TCP4 203.0.113.7 198.51.100.9 45678 443\r\n"))
	require.True(t, strings.HasSuffix(value, "payload"))
}

type tcpAddressConn struct {
	net.Conn
	remote net.Addr
	local  net.Addr
}

func (c *tcpAddressConn) RemoteAddr() net.Addr { return c.remote }
func (c *tcpAddressConn) LocalAddr() net.Addr  { return c.local }
