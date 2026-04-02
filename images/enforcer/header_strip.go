package main

import "net/http"

// internalHeaders are headers that only infrastructure may set.
// The enforcer strips these from agent-originated requests to prevent
// agents from forging platform-level authorization (ASK T5).
var internalHeaders = []string{
	"X-Agency-Platform",
	"X-Agency-Cache-Relay",
}

// HeaderStripper removes internal headers from requests before forwarding.
type HeaderStripper struct {
	next http.Handler
}

// NewHeaderStripper creates a new HeaderStripper wrapping next.
func NewHeaderStripper(next http.Handler) *HeaderStripper {
	return &HeaderStripper{next: next}
}

func (hs *HeaderStripper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, h := range internalHeaders {
		r.Header.Del(h)
	}
	hs.next.ServeHTTP(w, r)
}
