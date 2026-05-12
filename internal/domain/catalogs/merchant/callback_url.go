package merchant

import "metapus/internal/core/urlsafe"

// ValidateCallbackURL checks that a webhook callback URL is safe to use.
//
// Delegates to urlsafe.ValidatePublicURL for full SSRF prevention (CWE-918):
// HTTPS-only, DNS resolution to public IPs, metadata/localhost blocking.
//
// Returns nil if rawURL is empty (field is optional).
func ValidateCallbackURL(rawURL string) error {
	return urlsafe.ValidatePublicURL(rawURL, "callbackUrl")
}
