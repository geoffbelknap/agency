// agency-gateway/internal/models/validate.go
package models

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Common regex patterns used across models.
var (
	reCredentialEnv = regexp.MustCompile(`^[A-Z][A-Z0-9_]*_(API_KEY|TOKEN|SECRET|KEY)$`)
	reHierarchyName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

// Blocked hosts that must never appear as API base URLs.
var blockedHosts = map[string]bool{
	"169.254.169.254":          true, // AWS/GCP metadata
	"metadata.google.internal": true,
	"100.100.100.200":          true, // Alibaba metadata
	"0.0.0.0":                  true,
}

// ValidateAPIBase checks that an API base URL uses http(s), is not empty,
// and does not target a blocked host.
func ValidateAPIBase(apiBase string) error {
	apiBase = strings.TrimSpace(apiBase)
	if apiBase == "" {
		return fmt.Errorf("api_base must not be empty")
	}
	parsed, err := url.Parse(apiBase)
	if err != nil {
		return fmt.Errorf("api_base is not a valid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("api_base must use http:// or https:// scheme, got %s://", parsed.Scheme)
	}
	host := parsed.Hostname()
	if blockedHosts[host] {
		return fmt.Errorf("api_base must not target blocked host: %s", host)
	}
	if parsed.Scheme == "https" {
		if matched, _ := regexp.MatchString(`^\d+\.\d+\.\d+\.\d+$`, host); matched {
			return fmt.Errorf("api_base must use a domain name, not a raw IP, for HTTPS")
		}
	}
	return nil
}

// ValidateCredentialEnv checks that a credential env var name follows the
// naming convention: PREFIX_API_KEY, PREFIX_TOKEN, PREFIX_SECRET, or PREFIX_KEY.
func ValidateCredentialEnv(envVar string) error {
	if envVar == "" {
		return nil // empty is OK — means local provider
	}
	if !reCredentialEnv.MatchString(envVar) {
		return fmt.Errorf(
			"auth_env must reference a credential variable (pattern: *_API_KEY, *_TOKEN, *_SECRET, *_KEY), got: %s",
			envVar,
		)
	}
	return nil
}

// ValidateNotEmpty returns an error if the string is empty or whitespace-only.
func ValidateNotEmpty(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	return nil
}

// ValidateHierarchyName validates a department/team name.
func ValidateHierarchyName(name string) bool {
	return reHierarchyName.MatchString(name) && len(name) >= 2
}
