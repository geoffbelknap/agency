package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var buildID = "dev"

func init() {
	if buildID != "dev" {
		return
	}
	if envBuildID := os.Getenv("BUILD_ID"); strings.TrimSpace(envBuildID) != "" {
		buildID = envBuildID
	}
}

func main() {
	initLogging()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	configDir := os.Getenv("CONFIG_DIR")
	if configDir == "" {
		configDir = "/agency/web-fetch/config"
	}

	// Load config.
	cfg, err := LoadConfig(filepath.Join(configDir, "config.yaml"))
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Load audit logger.
	auditDir := os.Getenv("AUDIT_DIR")
	if auditDir == "" {
		auditDir = "/agency/web-fetch/audit"
	}
	auditHMACKey := os.Getenv("WEB_FETCH_AUDIT_HMAC_KEY")
	auditLogger, err := NewAuditLogger(auditDir, auditHMACKey)
	if err != nil {
		slog.Error("failed to create audit logger", "error", err)
		os.Exit(1)
	}

	// Build blocklists.
	bl := buildBlocklists(configDir)

	// Create rate limiter.
	rl := NewRateLimiter(cfg.RateLimits.GlobalRPM, cfg.RateLimits.PerDomainRPM)

	// Create cache.
	cache := NewCache(
		cfg.Cache.MaxEntries,
		time.Duration(cfg.Cache.TTLMinutes)*time.Minute,
		cfg.Cache.MaxEntryBytes,
	)

	// Create HTTP client.
	client := buildHTTPClient(cfg, bl)

	// Build service.
	svc := &Service{
		cfg:         cfg,
		blocklist:   bl,
		rateLimiter: rl,
		cache:       cache,
		audit:       auditLogger,
		httpClient:  client,
	}

	// Register routes.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /fetch", svc.handleFetch)
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /metrics", svc.handleMetrics)
	mux.HandleFunc("POST /blocklists/reload", func(w http.ResponseWriter, r *http.Request) {
		svc.blocklist = buildBlocklists(configDir)
		writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
	})

	// SIGHUP handler: reload config, blocklists, recreate rate limiter.
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGHUP)
		for range ch {
			slog.Info("SIGHUP received — reloading config and blocklists")
			newCfg, cfgErr := LoadConfig(filepath.Join(configDir, "config.yaml"))
			if cfgErr != nil {
				slog.Warn("reload: failed to load config", "error", cfgErr)
				continue
			}
			svc.cfg = newCfg
			svc.blocklist = buildBlocklists(configDir)
			svc.rateLimiter = NewRateLimiter(newCfg.RateLimits.GlobalRPM, newCfg.RateLimits.PerDomainRPM)
			slog.Info("reload complete")
		}
	}()

	slog.Info("web-fetch listening", "port", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

// handleHealth returns a simple health check response.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "build_id": buildID})
}

// buildBlocklists loads the platform static blocklist and operator blocklist from configDir.
func buildBlocklists(configDir string) *Blocklist {
	// Platform static blocklist.
	platform := NewBlocklist()
	for _, p := range []string{
		"*.onion",
		"169.254.*",
		"metadata.google.internal",
		"*.internal",
	} {
		platform.AddDeny(p)
	}

	// Operator blocklist from config dir.
	operator, _ := LoadBlocklistFile(filepath.Join(configDir, "blocklist.yaml"))
	if operator == nil {
		operator = NewBlocklist()
	}

	return MergeBlocklists(platform, operator)
}

// buildHTTPClient creates an http.Client with proxy, redirect policy, and user agent.
func buildHTTPClient(cfg Config, blocklist *Blocklist) *http.Client {
	transport := &http.Transport{}

	proxyURL := os.Getenv("HTTP_PROXY")
	if proxyURL == "" {
		proxyURL = os.Getenv("http_proxy")
	}
	if proxyURL != "" {
		if parsed, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}

	userAgent := cfg.Fetch.UserAgent
	if userAgent == "" {
		userAgent = "Agency/1.0 (web-fetch)"
	}

	maxRedirects := cfg.Fetch.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = 5
	}

	client := &http.Client{
		Transport: &userAgentTransport{
			inner:     transport,
			userAgent: userAgent,
		},
		Timeout: time.Duration(cfg.Fetch.TimeoutSeconds) * time.Second,
	}

	if !cfg.Fetch.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return http.ErrUseLastResponse
			}
			if blocklist != nil && blocklist.IsBlocked(req.URL.Hostname()) {
				return http.ErrUseLastResponse
			}
			return nil
		}
	}

	return client
}

// userAgentTransport injects a User-Agent header on every request.
type userAgentTransport struct {
	inner     http.RoundTripper
	userAgent string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("User-Agent", t.userAgent)
	return t.inner.RoundTrip(clone)
}
