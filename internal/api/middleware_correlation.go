package api

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"

	agencylog "github.com/geoffbelknap/agency/internal/logging"
)

const correlationHeader = "X-Correlation-Id"

// CorrelationID is a chi middleware that ensures every request has a
// correlation ID. If the client provides X-Correlation-Id, it is reused;
// otherwise a compact random ID is generated (gw-{8 hex chars}).
//
// The correlation ID is:
//   - Stored in the request context (accessible via logging.FromContext)
//   - Set on the response header
//   - Attached to the request-scoped slog logger
func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cid := r.Header.Get(correlationHeader)
		if cid == "" {
			cid = generateCorrelationID()
		}

		w.Header().Set(correlationHeader, cid)

		// Create a request-scoped logger with the correlation ID attached.
		logger := slog.Default().With("correlation_id", cid)
		ctx := agencylog.WithContext(r.Context(), logger)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func generateCorrelationID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "gw-00000000"
	}
	return "gw-" + hex.EncodeToString(b)
}
