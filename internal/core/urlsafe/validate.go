// Package urlsafe provides SSRF-safe URL validation for outbound server requests.
// Used by both merchant callback URLs and webhook dispatch URLs.
package urlsafe

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"metapus/internal/core/apperror"
)

// ValidatePublicURL checks that a URL is safe for outbound server requests.
//
// Prevents SSRF (CWE-918) by enforcing:
//  1. Must be a valid absolute URL
//  2. Scheme must be "https" (plain HTTP rejected)
//  3. Host must not be localhost, *.internal, or any bare hostname without TLD
//  4. Host must not resolve to a private/link-local/loopback/metadata IP range
//  5. Unresolvable hostnames are rejected (prevents DNS rebinding at delivery time)
//
// fieldName is used in validation error details (e.g. "callbackUrl", "webhookUrl").
// Returns nil if rawURL is empty (field is optional).
func ValidatePublicURL(rawURL, fieldName string) error {
	if rawURL == "" {
		return nil
	}

	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return apperror.NewValidation(fieldName + " is not a valid URL").
			WithDetail("field", fieldName)
	}

	if u.Scheme != "https" {
		return apperror.NewValidation(fieldName + " must use HTTPS").
			WithDetail("field", fieldName).
			WithDetail("scheme", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return apperror.NewValidation(fieldName + " must have a non-empty host").
			WithDetail("field", fieldName)
	}

	return assertPublicHost(host, fieldName)
}

// assertPublicHost rejects loopback, private, link-local, and metadata hosts.
func assertPublicHost(host, fieldName string) error {
	// Reject well-known dangerous names (case-insensitive).
	lower := strings.ToLower(host)
	if lower == "localhost" ||
		lower == "metadata.google.internal" ||
		strings.HasSuffix(lower, ".internal") {
		return apperror.NewValidation(
			fmt.Sprintf("%s host %q is not allowed", fieldName, host),
		).WithDetail("field", fieldName)
	}

	// If host is an IP literal, parse and range-check directly.
	if ip := net.ParseIP(host); ip != nil {
		return assertPublicIP(ip, host, fieldName)
	}

	// For hostnames: attempt DNS resolution and check each resulting IP.
	// This prevents SSRF via DNS rebinding: attacker registers a domain that
	// initially resolves to a public IP (passes validation at creation time),
	// then changes DNS to a private IP (exploited at webhook delivery time).
	addrs, err := net.LookupHost(host)
	if err != nil {
		// Reject unresolvable hosts — we cannot verify they are public.
		return apperror.NewValidation(
			fmt.Sprintf("%s host %q cannot be resolved", fieldName, host),
		).WithDetail("field", fieldName)
	}
	for _, addr := range addrs {
		if ip := net.ParseIP(addr); ip != nil {
			if err := assertPublicIP(ip, host, fieldName); err != nil {
				return err
			}
		}
	}

	return nil
}

// _privateRanges lists IP CIDR blocks that must not be reachable via outbound requests.
var _privateRanges = func() []*net.IPNet {
	blocks := []string{
		"10.0.0.0/8",     // RFC1918 Class A
		"172.16.0.0/12",  // RFC1918 Class B
		"192.168.0.0/16", // RFC1918 Class C
		"127.0.0.0/8",    // Loopback IPv4
		"169.254.0.0/16", // Link-local (APIPA / AWS metadata)
		"100.64.0.0/10",  // Shared address space (RFC6598)
		"::1/128",        // Loopback IPv6
		"fc00::/7",       // Unique local IPv6 (ULA)
		"fe80::/10",      // Link-local IPv6
		"2001:db8::/32",  // Documentation IPv6
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

// assertPublicIP checks that an IP is not in any blocked range.
func assertPublicIP(ip net.IP, host, fieldName string) error {
	// CIDR range check (private, loopback, link-local, documentation).
	for _, block := range _privateRanges {
		if block.Contains(ip) {
			return apperror.NewValidation(
				fmt.Sprintf("%s host %q resolves to a private/reserved IP address", fieldName, host),
			).WithDetail("field", fieldName)
		}
	}

	// Stdlib checks for edge cases not covered by CIDR list:
	// - 0.0.0.0 / :: (unspecified)
	// - ff02::* (link-local multicast)
	if ip.IsUnspecified() || ip.IsLinkLocalMulticast() {
		return apperror.NewValidation(
			fmt.Sprintf("%s host %q resolves to a reserved IP address", fieldName, host),
		).WithDetail("field", fieldName)
	}

	return nil
}
