// Package principal provides a typed context key for carrying the
// authenticated Principal across HTTP handlers.
//
// BearerAuth middleware (internal/api) sets the principal via With().
// Downstream handlers read it via Get(). Placing this in its own package
// avoids an import cycle between handler subpackages (e.g. internal/api/platform)
// and internal/api itself.
package principal

import (
	"context"
	"net/http"

	"github.com/geoffbelknap/agency/internal/registry"
)

// contextKey is a private type so only helpers in this package can read or
// write the principal slot on a request context.
type contextKey struct{}

// key is the singleton context key used to store the Principal.
var key contextKey

// Get returns the Principal resolved by middleware, or nil if none is set.
func Get(r *http.Request) *registry.Principal {
	p, _ := r.Context().Value(key).(*registry.Principal)
	return p
}

// With returns a new request with the Principal attached to its context.
func With(r *http.Request, p *registry.Principal) *http.Request {
	if p == nil {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), key, p))
}
