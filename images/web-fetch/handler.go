package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Service holds all components for the web-fetch handler.
type Service struct {
	cfg         Config
	blocklist   *Blocklist
	rateLimiter *RateLimiter
	cache       *Cache
	audit       *AuditLogger
	httpClient  *http.Client

	// Metrics counters
	totalRequests  int64
	blockedCount   int64
	rateLimitCount int64
	cacheHits      int64
	fetchErrors    int64
}

// FetchRequest is the JSON body for POST /fetch.
type FetchRequest struct {
	URL     string       `json:"url"`
	Options FetchOptions `json:"options,omitempty"`
}

// FetchOptions controls per-request fetch behavior.
type FetchOptions struct {
	IncludeLinks     *bool `json:"include_links,omitempty"`
	MaxContentLength int64 `json:"max_content_length,omitempty"`
	TimeoutSeconds   int   `json:"timeout_seconds,omitempty"`
	NoCache          bool  `json:"no_cache,omitempty"`
}

// FetchResponse is the JSON response for POST /fetch.
type FetchResponse struct {
	URL           string      `json:"url"`
	FinalURL      string      `json:"final_url,omitempty"`
	StatusCode    int         `json:"status_code,omitempty"`
	Metadata      *Metadata   `json:"metadata,omitempty"`
	Content       string      `json:"content,omitempty"`
	ContentLength int         `json:"content_length,omitempty"`
	Cached        bool        `json:"cached,omitempty"`
	XPIAScan      *XPIAResult `json:"xpia_scan,omitempty"`
	Error         string      `json:"error,omitempty"`
	Blocked       bool        `json:"blocked,omitempty"`
	BlockReason   string      `json:"block_reason,omitempty"`
}

// XPIAResult holds the XPIA scan outcome.
type XPIAResult struct {
	Clean bool     `json:"clean"`
	Flags []string `json:"flags,omitempty"`
}

// handleFetch is the main POST /fetch handler.
func (s *Service) handleFetch(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	reqID := randomID()
	atomic.AddInt64(&s.totalRequests, 1)

	// Parse request body.
	var req FetchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, FetchResponse{Error: "invalid request body: " + err.Error()})
		return
	}

	// Validate URL.
	if req.URL == "" {
		writeJSON(w, http.StatusBadRequest, FetchResponse{Error: "url is required"})
		return
	}
	parsed, err := url.Parse(req.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		writeJSON(w, http.StatusBadRequest, FetchResponse{URL: req.URL, Error: "invalid url: must be http or https"})
		return
	}
	host := parsed.Hostname()

	// Agent header (optional, for audit).
	agent := r.Header.Get("X-Agency-Agent")

	// DNS/blocklist check.
	if s.blocklist.IsBlocked(host) {
		atomic.AddInt64(&s.blockedCount, 1)
		reason := "host is blocked"
		_ = s.audit.Log(&AuditEntry{
			Timestamp:   time.Now(),
			Agent:       agent,
			URL:         req.URL,
			Method:      "GET",
			Blocked:     true,
			BlockReason: reason,
			DNSHit:      true,
			Duration:    time.Since(start).Milliseconds(),
			RequestID:   reqID,
		})
		writeJSON(w, http.StatusForbidden, FetchResponse{
			URL:         req.URL,
			Blocked:     true,
			BlockReason: reason,
			Error:       reason,
		})
		return
	}

	// Rate limit check.
	if !s.rateLimiter.Allow(host) {
		atomic.AddInt64(&s.rateLimitCount, 1)
		w.Header().Set("Retry-After", "60")
		_ = s.audit.Log(&AuditEntry{
			Timestamp: time.Now(),
			Agent:     agent,
			URL:       req.URL,
			Method:    "GET",
			Status:    http.StatusTooManyRequests,
			Duration:  time.Since(start).Milliseconds(),
			RequestID: reqID,
		})
		writeJSON(w, http.StatusTooManyRequests, FetchResponse{
			URL:   req.URL,
			Error: "rate limit exceeded",
		})
		return
	}

	// Cache check.
	if !req.Options.NoCache {
		if cached, ok := s.cache.Get(req.URL); ok {
			atomic.AddInt64(&s.cacheHits, 1)
			var xpiaResult *XPIAResult
			if s.cfg.XPIA.Enabled {
				xpiaResult = &XPIAResult{
					Clean: len(cached.XPIAFlags) == 0,
					Flags: cached.XPIAFlags,
				}
			}
			meta := cached.Metadata
			_ = s.audit.Log(&AuditEntry{
				Timestamp:      time.Now(),
				Agent:          agent,
				URL:            req.URL,
				Method:         "GET",
				Status:         cached.StatusCode,
				ContentType:    "",
				ExtractedBytes: int64(len(cached.Content)),
				Cached:         true,
				XPIAFlags:      cached.XPIAFlags,
				Duration:       time.Since(start).Milliseconds(),
				RequestID:      reqID,
			})
			writeJSON(w, http.StatusOK, FetchResponse{
				URL:           req.URL,
				StatusCode:    cached.StatusCode,
				Metadata:      &meta,
				Content:       cached.Content,
				ContentLength: len(cached.Content),
				Cached:        true,
				XPIAScan:      xpiaResult,
			})
			return
		}
	}

	// Build HTTP client with optional per-request timeout.
	client := s.httpClient
	if req.Options.TimeoutSeconds > 0 {
		client = &http.Client{
			Transport:     s.httpClient.Transport,
			CheckRedirect: s.httpClient.CheckRedirect,
			Timeout:       time.Duration(req.Options.TimeoutSeconds) * time.Second,
		}
	}

	// Content-type pre-check via HEAD.
	if headCT, ok := probeContentType(client, req.URL); ok {
		if !isAllowedContentType(headCT, s.cfg.ContentTypes.Allowed) {
			writeJSON(w, http.StatusUnsupportedMediaType, FetchResponse{
				URL:   req.URL,
				Error: fmt.Sprintf("content type not allowed: %s", headCT),
			})
			return
		}
	}

	// Fetch the URL.
	resp, err := client.Get(req.URL)
	if err != nil {
		atomic.AddInt64(&s.fetchErrors, 1)
		_ = s.audit.Log(&AuditEntry{
			Timestamp: time.Now(),
			Agent:     agent,
			URL:       req.URL,
			Method:    "GET",
			Duration:  time.Since(start).Milliseconds(),
			RequestID: reqID,
		})
		writeJSON(w, http.StatusBadGateway, FetchResponse{
			URL:   req.URL,
			Error: "fetch failed: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	finalURL := resp.Request.URL.String()
	contentType := resp.Header.Get("Content-Type")
	// Strip charset etc.
	if idx := strings.IndexByte(contentType, ';'); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	// Verify content-type from response.
	if !isAllowedContentType(contentType, s.cfg.ContentTypes.Allowed) {
		writeJSON(w, http.StatusUnsupportedMediaType, FetchResponse{
			URL:      req.URL,
			FinalURL: finalURL,
			Error:    fmt.Sprintf("content type not allowed: %s", contentType),
		})
		return
	}

	// Determine size limit.
	maxBytes := s.cfg.Fetch.MaxResponseBytes
	if req.Options.MaxContentLength > 0 && req.Options.MaxContentLength < maxBytes {
		maxBytes = req.Options.MaxContentLength
	}

	// Read body with size limit.
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		atomic.AddInt64(&s.fetchErrors, 1)
		writeJSON(w, http.StatusBadGateway, FetchResponse{
			URL:      req.URL,
			FinalURL: finalURL,
			Error:    "failed to read response: " + err.Error(),
		})
		return
	}
	rawBytes := int64(len(bodyBytes))

	// Extract content.
	includeLinks := false
	if req.Options.IncludeLinks != nil {
		includeLinks = *req.Options.IncludeLinks
	}
	maxContent := s.cfg.Fetch.MaxContentBytes
	if req.Options.MaxContentLength > 0 && req.Options.MaxContentLength < maxContent {
		maxContent = req.Options.MaxContentLength
	}
	extracted, err := Extract(bodyBytes, finalURL, ExtractOptions{
		IncludeLinks:    includeLinks,
		ContentType:     contentType,
		MaxContentBytes: maxContent,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, FetchResponse{
			URL:      req.URL,
			FinalURL: finalURL,
			Error:    "extraction failed: " + err.Error(),
		})
		return
	}

	// XPIA scan.
	var xpiaFlags []string
	var xpiaResult *XPIAResult
	if s.cfg.XPIA.Enabled {
		xpiaFlags = XPIAScan(extracted.Content)
		xpiaResult = &XPIAResult{
			Clean: len(xpiaFlags) == 0,
			Flags: xpiaFlags,
		}
		if s.cfg.XPIA.BlockOnFlag && len(xpiaFlags) > 0 {
			atomic.AddInt64(&s.blockedCount, 1)
			reason := "xpia flags detected"
			_ = s.audit.Log(&AuditEntry{
				Timestamp:      time.Now(),
				Agent:          agent,
				URL:            req.URL,
				FinalURL:       finalURL,
				Method:         "GET",
				Status:         resp.StatusCode,
				ContentType:    contentType,
				RawBytes:       rawBytes,
				ExtractedBytes: int64(len(extracted.Content)),
				Blocked:        true,
				BlockReason:    reason,
				XPIAFlags:      xpiaFlags,
				Duration:       time.Since(start).Milliseconds(),
				RequestID:      reqID,
			})
			writeJSON(w, http.StatusForbidden, FetchResponse{
				URL:         req.URL,
				FinalURL:    finalURL,
				Blocked:     true,
				BlockReason: reason,
				Error:       reason,
				XPIAScan:    xpiaResult,
			})
			return
		}
	}

	// Cache result for 2xx responses (unless Cache-Control: no-store).
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		cc := strings.ToLower(resp.Header.Get("Cache-Control"))
		if !strings.Contains(cc, "no-store") {
			s.cache.Set(req.URL, &CacheEntry{
				Content:    extracted.Content,
				StatusCode: resp.StatusCode,
				Metadata:   extracted.Metadata,
				XPIAFlags:  xpiaFlags,
				FetchedAt:  time.Now(),
			})
		}
	}

	// Audit log.
	_ = s.audit.Log(&AuditEntry{
		Timestamp:      time.Now(),
		Agent:          agent,
		URL:            req.URL,
		FinalURL:       finalURL,
		Method:         "GET",
		Status:         resp.StatusCode,
		ContentType:    contentType,
		RawBytes:       rawBytes,
		ExtractedBytes: int64(len(extracted.Content)),
		Cached:         false,
		XPIAFlags:      xpiaFlags,
		Duration:       time.Since(start).Milliseconds(),
		RequestID:      reqID,
	})

	meta := extracted.Metadata
	writeJSON(w, http.StatusOK, FetchResponse{
		URL:           req.URL,
		FinalURL:      finalURL,
		StatusCode:    resp.StatusCode,
		Metadata:      &meta,
		Content:       extracted.Content,
		ContentLength: len(extracted.Content),
		Cached:        false,
		XPIAScan:      xpiaResult,
	})
}

// handleMetrics returns service metrics as JSON.
func (s *Service) handleMetrics(w http.ResponseWriter, r *http.Request) {
	entries, hitRate := s.cache.Stats()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_requests":  atomic.LoadInt64(&s.totalRequests),
		"blocked_count":   atomic.LoadInt64(&s.blockedCount),
		"rate_limited":    atomic.LoadInt64(&s.rateLimitCount),
		"cache_hits":      atomic.LoadInt64(&s.cacheHits),
		"fetch_errors":    atomic.LoadInt64(&s.fetchErrors),
		"cache_entries":   entries,
		"cache_hit_rate":  hitRate,
	})
}

// probeContentType performs a HEAD request to check content-type before fetching.
// Returns the content-type and true if the HEAD succeeded; returns "", false otherwise.
func probeContentType(client *http.Client, rawURL string) (string, bool) {
	resp, err := client.Head(rawURL)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	return ct, ct != ""
}

// isAllowedContentType checks whether the given content-type is in the allowed list.
// If the allowed list is empty, all types are permitted.
func isAllowedContentType(ct string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	if ct == "" {
		return true
	}
	ct = strings.ToLower(strings.TrimSpace(ct))
	for _, a := range allowed {
		if strings.ToLower(a) == ct {
			return true
		}
	}
	return false
}

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// randomID generates a short random hex request ID.
func randomID() string {
	return fmt.Sprintf("%016x", rand.Int63())
}
