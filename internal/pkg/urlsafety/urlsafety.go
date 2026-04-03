package urlsafety

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Validate checks that a URL is safe for outbound requests.
func Validate(raw string) error {
	if raw == "" {
		return fmt.Errorf("empty URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	host := parsed.Hostname()
	isLocalhost := host == "localhost" || host == "127.0.0.1" || host == "::1"

	switch parsed.Scheme {
	case "https":
		if isLocalhost {
			return fmt.Errorf("https to localhost is not allowed; use http")
		}
	case "http":
		if !isLocalhost {
			return fmt.Errorf("http scheme only allowed for localhost, got host %q", host)
		}
	default:
		return fmt.Errorf("scheme %q not allowed; use https", parsed.Scheme)
	}

	if isBlockedHostname(host) {
		return fmt.Errorf("host %q is blocked", host)
	}

	// Localhost IPs are allowed on http (already validated above); skip private check.
	if !isLocalhost && IsPrivateIP(host) {
		return fmt.Errorf("private/reserved IP %q is blocked", host)
	}

	return nil
}

func isBlockedHostname(host string) bool {
	lower := strings.ToLower(host)
	return strings.HasSuffix(lower, ".internal") || lower == "metadata.google.internal"
}

// IsPrivateIP returns true if the string is a private, loopback, or link-local IP.
func IsPrivateIP(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

// SafeClient returns an http.Client that rejects connections to private IPs at connect time.
func SafeClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if IsPrivateIP(host) {
				return fmt.Errorf("connection to private IP %s blocked", host)
			}
			return nil
		},
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
		},
		Timeout: 10 * time.Second,
	}
}
