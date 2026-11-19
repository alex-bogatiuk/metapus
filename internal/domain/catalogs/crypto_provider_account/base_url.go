package crypto_provider_account

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateBaseURL accepts an absolute HTTP(S) provider URL whose host is a
// domain name, an IP address or localhost. url.Parse alone accepts a
// duplicated scheme such as "https://https//api.example" with host "https";
// every request to such a URL fails after the provider order is placed.
func ValidateBaseURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return fmt.Errorf("provider base URL must be an absolute HTTP(S) URL without userinfo")
	}
	host := parsed.Hostname()
	if net.ParseIP(host) == nil && !strings.EqualFold(host, "localhost") && (!strings.Contains(host, ".") || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".")) {
		return fmt.Errorf("provider base URL host %q is not a domain name or IP address", host)
	}
	if strings.HasPrefix(parsed.Path, "//") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("provider base URL must not contain an empty path segment, query or fragment")
	}
	return nil
}
