// Package urlsafe validates and resolves public URLs (no private
// addresses).
package urlsafe

import (
	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/urlsafe"
)

type (
	// ResolvedURL contains a validated URL with a pre-resolved public IP.
	// Use this to eliminate DNS rebinding TOCTOU: resolve DNS once, validate the IP,
	// then pass ResolvedIP to a custom DialContext that bypasses the Go client's DNS.
	ResolvedURL = core.ResolvedURL
)

// ResolvePublicURL validates a URL and resolves DNS in a single step,
// returning the validated IP to use for the actual connection.
//
// This eliminates the DNS rebinding TOCTOU vulnerability (CWE-367):
// ValidatePublicURL resolves DNS for checking, but http.Client.Do resolves
// DNS again — an attacker with TTL=0 can return a different (private) IP
// on the second lookup. By resolving once and pinning the IP, we close
// the TOCTOU window.
//
// Returns error if the URL is invalid, not HTTPS, or resolves to a private IP.
func ResolvePublicURL(rawURL string, fieldName string) (*core.ResolvedURL, error) {
	return core.ResolvePublicURL(rawURL, fieldName)
}

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
func ValidatePublicURL(rawURL string, fieldName string) error {
	return core.ValidatePublicURL(rawURL, fieldName)
}
