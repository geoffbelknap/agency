package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var buildID = "unknown"

const (
	defaultPort            = "3128"
	defaultConstraintPort  = "8081"
	defaultRoutingCfg      = "/agency/enforcer/routing.yaml"
	defaultAPIKeysFile     = "/agency/enforcer/auth/api_keys.yaml"
	defaultAuditDir        = "/agency/enforcer/audit"
	defaultServicesDir     = "/agency/enforcer/services"
	defaultAgentDir        = "/agency/agent"
	defaultKeysFile        = "/agency/enforcer/service-keys.env"
	defaultDomainsFile     = "/agency/agent/egress-domains.yaml"
	defaultBodyNotifyURL   = "http://workspace:8090/hooks/constraint-change"
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
		log.Printf("warning: could not load routing config: %v", err)
		routing = &RoutingConfig{
			Providers: make(map[string]Provider),
			Models:    make(map[string]Model),
		}
	}

	// Load API keys for auth
	apiKeysFile := envOr("API_KEYS_FILE", defaultAPIKeysFile)
	apiKeys, err := LoadAPIKeys(apiKeysFile)
	if err != nil {
		log.Printf("warning: could not load API keys: %v", err)
	}
	auth := NewAuthMiddleware(apiKeys)

	// Load domain gate
	domains := NewDomainGate()
	domainsFile := envOr("EGRESS_DOMAINS_FILE", defaultDomainsFile)
	if err := domains.LoadFromFile(domainsFile); err != nil {
		log.Printf("warning: could not load egress domains: %v", err)
	}

	// Load service registry
	services := NewServiceRegistry()
	servicesDir := envOr("SERVICES_DIR", defaultServicesDir)
	agentDir := envOr("AGENT_DIR", defaultAgentDir)
	keysFile := envOr("SERVICE_KEYS_FILE", defaultKeysFile)
	if err := services.LoadFromFiles(servicesDir, agentDir, keysFile); err != nil {
		log.Printf("warning: could not load services: %v", err)
	}

	egressProxy := envOr("EGRESS_PROXY", defaultEgressProxy)

	proxy := NewProxyHandler(domains, services, audit, agentName)
	llm := NewLLMHandler(routing, egressProxy, audit)

	bodyNotifyURL := envOr("BODY_NOTIFY_URL", defaultBodyNotifyURL)
	constraint := NewConstraintHandler(agentName, audit, bodyNotifyURL)

	// Budget tracker
	budgetTracker := NewBudgetTracker(agentName)
	llm.SetBudget(budgetTracker)

	return &Enforcer{
		auth:       auth,
		proxy:      proxy,
		llm:        llm,
		audit:      audit,
		domains:    domains,
		services:   services,
		routing:    routing,
		constraint: constraint,
		budget:     budgetTracker,
	}
}

// Reload reloads all configuration files (triggered by SIGHUP).
func (e *Enforcer) Reload() {
	log.Println("SIGHUP received, reloading configuration")

	// Reload routing config
	routingFile := envOr("ROUTING_CONFIG", defaultRoutingCfg)
	if rc, err := LoadRoutingConfig(routingFile); err == nil {
		e.routing = rc
		e.llm.SetRouting(rc)
		log.Println("reloaded routing config")
	} else {
		log.Printf("warning: failed to reload routing: %v", err)
	}

	// Reload API keys
	apiKeysFile := envOr("API_KEYS_FILE", defaultAPIKeysFile)
	if keys, err := LoadAPIKeys(apiKeysFile); err == nil {
		e.auth.SetKeys(keys)
		log.Println("reloaded API keys")
	} else {
		log.Printf("warning: failed to reload API keys: %v", err)
	}

	// Reload domain gate
	domainsFile := envOr("EGRESS_DOMAINS_FILE", defaultDomainsFile)
	if err := e.domains.LoadFromFile(domainsFile); err != nil {
		log.Printf("warning: failed to reload egress domains: %v", err)
	} else {
		log.Println("reloaded egress domains")
	}

	// Reload service registry
	servicesDir := envOr("SERVICES_DIR", defaultServicesDir)
	agentDir := envOr("AGENT_DIR", defaultAgentDir)
	keysFile := envOr("SERVICE_KEYS_FILE", defaultKeysFile)
	if err := e.services.LoadFromFiles(servicesDir, agentDir, keysFile); err != nil {
		log.Printf("warning: failed to reload services: %v", err)
	} else {
		log.Println("reloaded services")
	}

	e.audit.Log(AuditEntry{
		Type: "CONFIG_RELOAD",
	})
}

// Handler returns the top-level HTTP handler with auth wrapping.
func (e *Enforcer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","build_id":%q}`, buildID)
	})
	mux.Handle("/v1/", e.llm)
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

	agentName := os.Getenv("AGENT_NAME")
	gatewayURL := os.Getenv("GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://gateway:8200"
	}
	url := fmt.Sprintf("%s/api/v1/agents/%s/signal", gatewayURL, agentName)

	// Best-effort relay — don't block the body runtime
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		req, err := http.NewRequest("POST", url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		// Gateway auth token — passed via env var at container creation
		if token := os.Getenv("GATEWAY_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("signal relay failed: %v", err)
			return
		}
		resp.Body.Close()
	}()

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
	port := envOr("ENFORCER_PORT", defaultPort)
	constraintPort := envOr("CONSTRAINT_WS_PORT", defaultConstraintPort)

	enforcer := NewEnforcer()

	// ASK Tenet 25: memory mutation audit — watch agent memory directory for
	// identity writes and log provenance for every change.
	const memDir = "/agency/memory"
	var memAuditor *MemoryAuditor
	if _, err := os.Stat(memDir); err == nil {
		agentName := os.Getenv("AGENT_NAME")
		if agentName == "" {
			agentName = "unknown"
		}
		memAuditor = NewMemoryAuditor(memDir, agentName, enforcer.audit)
		go memAuditor.Start()
	}

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
		log.Println("shutting down enforcer")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(ctx)
		constraintServer.Shutdown(ctx)
		if memAuditor != nil {
			memAuditor.Stop()
		}
		enforcer.audit.Close()
	}()

	// Start constraint server in background.
	go func() {
		log.Printf("constraint server listening on :%s", constraintPort)
		if err := constraintServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("constraint server error: %v", err)
		}
	}()

	log.Printf("enforcer listening on :%s", port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
	log.Println("enforcer stopped")
}
