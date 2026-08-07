package correlation

import (
	"fmt"
	"net"
	"net/url"
)

// URLPolicy accepts only openable local HTTP(S) endpoints. Launching is deliberately owned by PR3 actions.
type URLPolicy struct{ AllowedHosts []string }

func (p URLPolicy) Validate(raw string) (string, error) {
	if containsControl(raw) {
		return "", fmt.Errorf("URL contains control characters")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("URL scheme %q is not allowed", parsed.Scheme)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("credentialed URLs are not allowed")
	}
	host := parsed.Hostname()
	if !p.allowed(host) {
		return "", fmt.Errorf("URL host %q is not allowlisted", host)
	}
	return parsed.String(), nil
}

func containsControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func (p URLPolicy) allowed(host string) bool {
	if len(p.AllowedHosts) > 0 {
		for _, allowed := range p.AllowedHosts {
			if host == allowed {
				return true
			}
		}
		return false
	}
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}
