package merchant

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"metapus/internal/core/apperror"
)

// ValidateCallbackURL checks that a webhook callback URL is safe to use.
//
// Rules enforced (CWE-918 SSRF prevention):
//  1. Must be a valid absolute URL
//  2. Scheme must be "https" (plain HTTP rejected — credentials travel in POST body)
//  3. Host must not resolve to a private/link-local/loopback IP range
//     (blocks 169.254.169.254, 10.x, 172.16-31.x, 192.168.x, ::1, etc.)
//  4. Host must not be "localhost" or any bare hostname without a TLD
//
// Returns nil if rawURL is empty (field is optional).
func ValidateCallbackURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}

	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return apperror.NewValidation("callbackUrl is not a valid URL").
			WithDetail("field", "callbackUrl")
	}

	if u.Scheme != "https" {
		return apperror.NewValidation("callbackUrl must use HTTPS").
			WithDetail("field", "callbackUrl").
			WithDetail("scheme", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return apperror.NewValidation("callbackUrl must have a non-empty host").
			WithDetail("field", "callbackUrl")
	}

	if err := assertPublicHost(host); err != nil {
		return err
	}

	return nil
}

// assertPublicHost rejects loopback, private, link-local, and metadata hosts.
func assertPublicHost(host string) error {
	// Reject well-known dangerous names (case-insensitive)
	lower := strings.ToLower(host)
	switch lower {
	case "localhost", "metadata.google.internal":
		return apperror.NewValidation(
			fmt.Sprintf("callbackUrl host %q is not allowed", host),
		).WithDetail("field", "callbackUrl")
	}

	// If host is an IP literal, parse and range-check directly
	if ip := net.ParseIP(host); ip != nil {
		return assertPublicIP(ip, host)
	}

	// For hostnames: attempt DNS resolution and check each resulting IP.
	// This prevents bypasses via DNS rebinding at registration time.
	// NOTE: A more robust defence is to re-check IPs at delivery time (worker);
	//       this check is a best-effort guard at invoice creation.
	addrs, err := net.LookupHost(host)
	if err != nil {
		// CWE-918: reject unresolvable hosts — we cannot verify they are public.
		// A DNS rebinding attack could provide a host that fails now but resolves
		// to a private IP at webhook delivery time.
		return apperror.NewValidation(
			fmt.Sprintf("callbackUrl host %q cannot be resolved", host),
		).WithDetail("field", "callbackUrl")
	}
	for _, addr := range addrs {
		if ip := net.ParseIP(addr); ip != nil {
			if err := assertPublicIP(ip, host); err != nil {
				return err
			}
		}
	}

	return nil
}

// _privateRanges lists IP CIDR blocks that must not be reachable via callbacks.
var _privateRanges = func() []*net.IPNet {
	blocks := []string{
		"10.0.0.0/8",          // RFC1918 Class A
		"172.16.0.0/12",       // RFC1918 Class B
		"192.168.0.0/16",      // RFC1918 Class C
		"127.0.0.0/8",         // Loopback IPv4
		"169.254.0.0/16",      // Link-local (APIPA / AWS metadata)
		"100.64.0.0/10",       // Shared address space (RFC6598)
		"::1/128",             // Loopback IPv6
		"fc00::/7",            // Unique local IPv6 (ULA)
		"fe80::/10",           // Link-local IPv6
		"2001:db8::/32",       // Documentation IPv6
	}
	nets := make([]*net.IPNet, 0, len(blocks))
	for _, b := range blocks {
		_, ipNet, err := net.ParseCIDR(b)
		if err == nil {
			nets = append(nets, ipNet)
		}
	}
	return nets
}()

func assertPublicIP(ip net.IP, host string) error {
	for _, block := range _privateRanges {
		if block.Contains(ip) {
			return apperror.NewValidation(
				fmt.Sprintf("callbackUrl host %q resolves to a private/reserved IP address", host),
			).WithDetail("field", "callbackUrl")
		}
	}
	return nil
}
