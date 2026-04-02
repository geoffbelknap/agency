package cli

import (
	"github.com/geoffbelknap/agency/internal/apiclient"
)

// Client is a thin REST client for the Agency gateway API.
// It is an alias for apiclient.Client, which holds the full implementation.
type Client = apiclient.Client

// NewClient creates a client for the given gateway base URL.
// It loads the operator token from ~/.agency/config.yaml if present.
func NewClient(baseURL string) *Client {
	return apiclient.NewClient(baseURL)
}
