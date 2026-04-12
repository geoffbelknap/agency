package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var buildID = "unknown"

const (
	defaultPort           = "3128"
	defaultConstraintPort = "8081"
	defaultRoutingCfg     = "/agency/enforcer/routing.yaml"
	defaultAPIKeysFile    = "/agency/enforcer/auth/api_keys.yaml"
	defaultAuditDir       = "/agency/enforcer/audit"
	defaultServicesDir    = "/agency/enforcer/services"
	defaultAgentDir       = "/agency/agent"
	defaultDomainsFile    = "/agency/agent/egress-domains.yaml"
	defaultBodyNotifyURL  = "http://workspace:8090/hooks/constraint-change"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Enforcer holds all the wired components.
type Enforcer struct {
	auth       *AuthMiddleware
	proxy      *ProxyHandler
	llm        *LLMHandler
	audit      *AuditLogger
	domains    *DomainGate
	services   *ServiceRegistry
	routing    *RoutingConfig
	constraint *ConstraintHandler
	budget     *BudgetTracker
	mediation  *MediationProxy
	trajectory *TrajectoryMonitor
}

// NewEnforcer creates and wires all enforcer components.
func NewEnforcer() *Enforcer {
	agentName := os.Getenv("AGENT_NAME")
	if agentName == "" {
		agentName = "unknown"
	}

	auditDir := envOr("ENFORCER_LOG_DIR", defaultAuditDir)
	audit := NewAuditLogger(auditDir, agentName)

	lifecycleID := os.Getenv("AGENCY_LIFECYCLE_ID")
	if lifecycleID != "" {
		audit.SetLifecycleID(lifecycleID)
	}

	// Load routing config
	routingFile := envOr("ROUTING_CONFIG", defaultRoutingCfg)
	routing, err := LoadRoutingConfig(routingFile)
	if err != nil {
		slog.Warn("could not load routing config", "error", err)
		routing = &RoutingConfig{
			Providers: make(map[string]Provider),
			Models:    make(map[string]Model),
		}
	}

	// Load API keys for auth
	apiKeysFile := envOr("API_KEYS_FILE", defaultAPIKeysFile)
	apiKeys, err := LoadAPIKeys(apiKeysFile)
	if err != nil {
		slog.Warn("could not load API keys", "error", err)
	}
	auth := NewAuthMiddleware(apiKeys)

	// Load domain gate
	domains := NewDomainGate()
	domainsFile := envOr("EGRESS_DOMAINS_FILE", defaultDomainsFile)
	if err := domains.LoadFromFile(domainsFile); err != nil {
		slog.Warn("could not load egress domains", "error", err)
	}

	// Load service registry (scope metadata only — no real keys needed)
	services := NewServiceRegistry()
	servicesDir := envOr("SERVICES_DIR", defaultServicesDir)
	agentDir := envOr("AGENT_DIR", defaultAgentDir)
	if err := services.LoadFromFiles(servicesDir, agentDir); err != nil {
		slog.Warn("could not load services", "error", err)
	}

	egressProxy := envOr("EGRESS_PROXY", defaultEgressProxy)

	enforcer := &Enforcer{
		auth:     auth,
		audit:    audit,
		domains:  domains,
		services: services,
		routing:  routing,
	}

	proxy := NewProxyHandler(domains, services, audit, agentName, enforcer.emitSignal)
	llm := NewLLMHandler(routing, egressProxy, audit)

	rateLimiter := NewRateLimiter(50, 60) // 50 rpm default, 60s window
	llm.SetRateLimiter(rateLimiter, agentName)

	bodyNotifyURL := envOr("BODY_NOTIFY_URL", defaultBodyNotifyURL)
	constraint := NewConstraintHandler(agentName, audit, bodyNotifyURL)

	// Budget tracker
	budgetTracker := NewBudgetTracker(agentName)
	llm.SetBudget(budgetTracker)

	// Trajectory monitor
	trajectory := NewTrajectoryMonitor(DefaultTrajectoryConfig())
	llm.SetTrajectory(trajectory)

	// Mediation proxy — reverse proxy to comms and knowledge on mediation network.
	// Workspace reaches these via /mediation/{service}/... instead of direct DNS.
	commsURL := envOr("COMMS_URL", "http://comms:8080")
	knowledgeURL := envOr("KNOWLEDGE_URL", "http://knowledge:8080")
	webFetchURL := envOr("WEB_FETCH_URL", "http://web-fetch:8080")
	gatewayURL := envOr("GATEWAY_URL", "http://gateway:8200")
	mediationHeaders := map[string]map[string]string{}
	if token := os.Getenv("GATEWAY_TOKEN"); token != "" {
		mediationHeaders["runtime"] = map[string]string{
			"Authorization":  "Bearer " + token,
			"X-Agency-Agent": agentName,
		}
	}
	mediationProxy := NewMediationProxy(map[string]string{
		"comms":     commsURL,
		"knowledge": knowledgeURL,
		"web-fetch": webFetchURL,
		"runtime":   gatewayURL,
	}, mediationHeaders, audit)

	enforcer.proxy = proxy
	enforcer.llm = llm
	enforcer.constraint = constraint
	enforcer.budget = budgetTracker
	enforcer.mediation = mediationProxy
	enforcer.trajectory = trajectory
	return enforcer
}

func (e *Enforcer) emitSignal(signalType string, data map[string]interface{}) {
	if strings.TrimSpace(signalType) == "" {
		return
	}
	body, err := json.Marshal(map[string]interface{}{
		"signal_type": signalType,
		"data":        data,
	})
	if err != nil {
		return
	}
	e.relaySignal(body)
}

func (e *Enforcer) relaySignal(body []byte) {
	agentName := os.Getenv("AGENT_NAME")
	gatewayURL := os.Getenv("GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://gateway:8200"
	}
	url := fmt.Sprintf("%s/api/v1/agents/%s/signal", gatewayURL, agentName)

	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		req, err := http.NewRequest("POST", url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if token := os.Getenv("GATEWAY_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("signal relay failed", "error", err)
			return
		}
		resp.Body.Close()
	}()
}

// Reload reloads all configuration files (triggered by SIGHUP).
func (e *Enforcer) Reload() {
	slog.Info("SIGHUP received, reloading configuration")

	// Reload routing config
	routingFile := envOr("ROUTING_CONFIG", defaultRoutingCfg)
	if rc, err := LoadRoutingConfig(routingFile); err == nil {
		e.routing = rc
		e.llm.SetRouting(rc)
		slog.Info("reloaded routing config")
	} else {
		slog.Warn("failed to reload routing", "error", err)
	}

	// Reload API keys
	apiKeysFile := envOr("API_KEYS_FILE", defaultAPIKeysFile)
	if keys, err := LoadAPIKeys(apiKeysFile); err == nil {
		e.auth.SetKeys(keys)
		slog.Info("reloaded API keys")
	} else {
		slog.Warn("failed to reload API keys", "error", err)
	}

	// Reload domain gate
	domainsFile := envOr("EGRESS_DOMAINS_FILE", defaultDomainsFile)
	if err := e.domains.LoadFromFile(domainsFile); err != nil {
		slog.Warn("failed to reload egress domains", "error", err)
	} else {
		slog.Info("reloaded egress domains")
	}

	// Reload service registry (scope metadata only — no real keys needed)
	servicesDir := envOr("SERVICES_DIR", defaultServicesDir)
	agentDir := envOr("AGENT_DIR", defaultAgentDir)
	if err := e.services.LoadFromFiles(servicesDir, agentDir); err != nil {
		slog.Warn("failed to reload services", "error", err)
	} else {
		slog.Info("reloaded services")
	}

	e.audit.Log(AuditEntry{
		Type: "CONFIG_RELOAD",
	})

	// Notify body of config changes so it re-fetches from /config/ endpoint
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		bodyURL := envOr("BODY_NOTIFY_URL", defaultBodyNotifyURL)
		configURL := strings.TrimSuffix(bodyURL, "/constraint-change") + "/config-change"
		payload := bytes.NewReader([]byte(`{"type":"config_change"}`))
		resp, err := client.Post(configURL, "application/json", payload)
		if err != nil {
			slog.Warn("config notify: body unreachable", "error", err)
			return
		}
		resp.Body.Close()
	}()
}

// Handler returns the top-level HTTP handler with auth wrapping.
func (e *Enforcer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","build_id":%q}`, buildID)
	})
	// LLM paths — only match specific OpenAI-compatible endpoints, not all /v1/.
	// Service tool calls (e.g., /v1/hive/fp/...) use absolute-form URLs through
	// the proxy and must reach the proxy handler, not the LLM handler.
	mux.Handle("/v1/chat/completions", e.llm)
	mux.Handle("/v1/models", e.llm)
	mux.Handle("/", e.proxy)

	return e.auth.Wrap(NewHeaderStripper(mux))
}

// handleSignalRelay forwards agent signals to the gateway for WebSocket broadcast.
// The body runtime POSTs signals here; the enforcer relays to the gateway's
// /api/v1/agents/{name}/signal endpoint on the mediation network.
func (e *Enforcer) handleSignalRelay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	e.relaySignal(body)

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprint(w, `{"status":"accepted"}`)
}

// ConnectHandler returns an HTTP handler that routes CONNECT to the proxy handler.
func (e *Enforcer) ConnectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			e.proxy.HandleConnect(w, r)
			return
		}
		e.Handler().ServeHTTP(w, r)
	})
}

func main() {
	initLogging()

	port := envOr("ENFORCER_PORT", defaultPort)
	constraintPort := envOr("CONSTRAINT_WS_PORT", defaultConstraintPort)

	enforcer := NewEnforcer()

	server := &http.Server{
		Addr:           ":" + port,
		Handler:        enforcer.ConnectHandler(),
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   0, // No write timeout for streaming
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MB
	}

	// Constraint delivery server on separate port (gateway WebSocket + Body REST).
	// No auth: /ws is gateway-to-enforcer on the mediation network;
	// /constraints and /constraints/ack are enforcer-to-Body on agent-internal.
	constraintMux := http.NewServeMux()
	constraintMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","build_id":%q}`, buildID)
	})
	enforcer.constraint.RegisterRoutes(constraintMux)

	// Config server — body runtime fetches agent config files over HTTP.
	// No auth required: this port is agent-internal only.
	configServer := NewConfigServer(envOr("AGENT_DIR", defaultAgentDir))
	constraintMux.Handle("/config/", configServer)

	// Mediation proxy — body runtime reaches comms/knowledge through here.
	// No auth required: this port is agent-internal only.
	constraintMux.Handle("/mediation/", enforcer.mediation)

	// Budget endpoint — body runtime queries before starting a task.
	constraintMux.HandleFunc("/budget", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		remaining := enforcer.budget.GetRemaining()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(remaining)
	})

	// Trajectory endpoint — gateway queries current trajectory monitor state.
	constraintMux.HandleFunc("/trajectory", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := os.Getenv("AGENT_NAME")
		state := enforcer.trajectory.GetState(name)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)
	})

	constraintServer := &http.Server{
		Addr:           ":" + constraintPort,
		Handler:        constraintMux,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   0, // No write timeout for WebSocket
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// SIGHUP reload
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go func() {
		for range sighup {
			enforcer.Reload()
		}
	}()

	// Graceful shutdown on SIGTERM/SIGINT
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-done
		slog.Info("shutting down enforcer")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(ctx)
		constraintServer.Shutdown(ctx)
		enforcer.audit.Close()
	}()

	// Start constraint server in background.
	go func() {
		slog.Info("constraint server listening on :" + constraintPort + "")
		if err := constraintServer.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("constraint server error", "error", err)
			os.Exit(1)
		}
	}()

	slog.Info("enforcer listening on :" + port + "")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
	slog.Info("enforcer stopped")
}
