package rpcpolicy

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"time"

	"metapus/internal/infrastructure/urlsafe"
)

// PublicDialContext pins the validated destination IP. An optional DNS-over-TLS
// server avoids a local VPN's fake-IP DNS without permitting reserved RPC IPs.
// The resolver is an operator-controlled public IP:853 with a valid TLS identity.
func PublicDialContext(dnsTLSServer string) func(context.Context, string, string) (net.Conn, error) {
	resolver, configErr := rpcResolver(dnsTLSServer)
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if configErr != nil {
			return nil, configErr
		}
		resolved, err := urlsafe.ResolvePublicURLWithResolverContext(ctx, "https://"+address+"/", "url", resolver)
		if err != nil {
			return nil, errors.New("RPC destination unavailable")
		}
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid RPC destination")
		}
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(resolved.ResolvedIP, port))
	}
}

func rpcResolver(server string) (*net.Resolver, error) {
	if server == "" {
		return net.DefaultResolver, nil
	}
	host, port, err := net.SplitHostPort(server)
	if err != nil || net.ParseIP(host) == nil || port != "853" {
		return nil, errors.New("RPC DNS TLS server must be a public IP:853")
	}
	if _, err = urlsafe.ResolvePublicURLContext(context.Background(), "https://"+net.JoinHostPort(host, "443")+"/", "dnsServer"); err != nil {
		return nil, errors.New("RPC DNS TLS server must use a public IP")
	}
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}, Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}}
		return dialer.DialContext(ctx, "tcp", server)
	}}, nil
}
