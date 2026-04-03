package api

import "github.com/geoffbelknap/agency/internal/pkg/urlsafety"

// validateOutboundURL checks that a URL is safe for gateway outbound requests.
func validateOutboundURL(raw string) error {
	return urlsafety.Validate(raw)
}
