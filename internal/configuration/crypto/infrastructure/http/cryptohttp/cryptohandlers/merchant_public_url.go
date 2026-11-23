package cryptohandlers

import "github.com/alex-bogatiuk/metapus/pkg/sdk/urlsafe"

// ValidateNotificationURL enforces the public HTTPS boundary for an
// invoice-specific webhook destination.
func ValidateNotificationURL(rawURL string) error {
	return urlsafe.ValidatePublicURL(rawURL, "notificationUrl")
}

// ValidateReturnURL enforces the public HTTPS boundary for the browser-only
// post-checkout destination.
func ValidateReturnURL(rawURL string) error {
	return urlsafe.ValidatePublicURL(rawURL, "returnUrl")
}
