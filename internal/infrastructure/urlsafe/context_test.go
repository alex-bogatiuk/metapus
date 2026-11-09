package urlsafe

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

func TestPublicURLDNSHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	started := time.Now()
	_, err := ResolvePublicURLContext(ctx, "https://does-not-exist.metapus.example/", "url")
	require.Error(t, err)
	require.Less(t, time.Since(started), time.Second)
}

func TestDedicatedResolverPreservesPublicIPPolicy(t *testing.T) {
	for _, tt := range []struct {
		name    string
		ips     []net.IP
		allowed bool
	}{
		{"public", []net.IP{net.ParseIP("1.1.1.1")}, true},
		{"vpn fake IP", []net.IP{net.ParseIP("198.18.1.5")}, false},
		{"mixed public and private", []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("10.0.0.1")}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
				client, server := net.Pipe()
				go func() {
					defer func() { _ = server.Close() }()
					_ = server.SetDeadline(time.Now().Add(5 * time.Second))
					var prefix [2]byte
					if _, err := io.ReadFull(server, prefix[:]); err != nil {
						return
					}
					buf := make([]byte, binary.BigEndian.Uint16(prefix[:]))
					if _, err := io.ReadFull(server, buf); err != nil {
						return
					}
					var query dnsmessage.Message
					if query.Unpack(buf) != nil || len(query.Questions) != 1 {
						return
					}
					response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, RecursionAvailable: true}, Questions: query.Questions}
					if query.Questions[0].Type == dnsmessage.TypeA {
						for _, ip := range tt.ips {
							var a [4]byte
							copy(a[:], ip.To4())
							response.Answers = append(response.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: query.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: a}})
						}
					}
					packed, err := response.Pack()
					if err != nil {
						return
					}
					binary.BigEndian.PutUint16(prefix[:], uint16(len(packed)))
					_, _ = server.Write(append(prefix[:], packed...))
				}()
				return client, nil
			}}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			resolved, err := ResolvePublicURLWithResolverContext(ctx, "https://rpc.example/", "url", resolver)
			if tt.allowed {
				require.NoError(t, err)
				require.Equal(t, "1.1.1.1", resolved.ResolvedIP)
			} else {
				require.Error(t, err)
			}
		})
	}
}
