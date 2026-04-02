package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultEgressProxy = "http://egress:3128"
const maxRequestBodySize = 10 << 20 // 10 MB

// validateHost rejects hosts containing CRLF or non-printable characters.
func validateHost(host string) error {
	for _, c := range host {
		if c == '\r' || c == '\n' || c < 0x20 || c == 0x7f {
			return fmt.Errorf("invalid character in host")
		}
	}
	return nil
}

// dangerousHeaders are stripped from proxy responses.
var dangerousHeaders = []string{
	"transfer-encoding",
	"connection",
	"set-cookie",
	"proxy-authenticate",
	"proxy-authorization",
	"proxy-connection",
	"keep-alive",
	"upgrade",
}

// allowedForwardHeaders lists the only request headers forwarded to upstream.
// Allowlist approach prevents leaking hop-by-hop, proxy, privacy, or unknown
// headers to external APIs (ASK Tenet 7: least privilege).
var allowedForwardHeaders = []string{
	"Content-Type",
	"Content-Length",
	"Accept",
	"Accept-Encoding",
	"Accept-Language",
	"Authorization",
	"User-Agent",
	"X-Request-Id",
	"X-Agency-Service",
	"X-Agency-Task-Id",
	"X-Agency-Cost-Source",
	"Cache-Control",
	"If-None-Match",
	"If-Modified-Since",
}

// ProxyHandler handles non-LLM HTTP traffic: domain gating, service credential
// swap, header filtering, and forwarding through egress proxy.
type ProxyHandler struct {
	egressProxy string
	domainGate  *DomainGate
	services    *ServiceRegistry
	audit       *AuditLogger
	agentName   string
	transport   *http.Transport
}

// NewProxyHandler creates a proxy handler for non-LLM traffic.
func NewProxyHandler(domainGate *DomainGate, services *ServiceRegistry, audit *AuditLogger, agentName string) *ProxyHandler {
	egressProxy := os.Getenv("EGRESS_PROXY")
	if egressProxy == "" {
		egressProxy = defaultEgressProxy
	}

	transport := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	return &ProxyHandler{
		egressProxy: egressProxy,
		domainGate:  domainGate,
		services:    services,
		audit:       audit,
		agentName:   agentName,
		transport:   transport,
	}
}

// ServeHTTP handles regular HTTP proxy requests (non-CONNECT).
func (ph *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Extract target host
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}

	if err := validateHost(host); err != nil {
		http.Error(w, `{"error":"invalid target host"}`, http.StatusBadRequest)
		return
	}

	eventID := r.Header.Get("X-Agency-Event-Id")

	// Domain gate check
	if !ph.domainGate.Allowed(host) {
		ph.audit.Log(AuditEntry{
			Type:    "DOMAIN_BLOCKED",
			Method:  r.Method,
			URL:     r.URL.String(),
			Host:    host,
			EventID: eventID,
			Status:  403,
		})
		http.Error(w, `{"error":"domain blocked by policy"}`, http.StatusForbidden)
		return
	}

	// Consume X-Agency-Tool header — never leak to external API
	toolName := r.Header.Get("X-Agency-Tool")
	r.Header.Del("X-Agency-Tool")

	// Service scope enforcement — validate the agent is granted this service
	// and the tool has the required scope. The enforcer does NOT inject real
	// credentials (ASK Tenet 4: least privilege). X-Agency-Service passes
	// through to the egress proxy, which resolves credentials from
	// .service-keys.env. Real API keys never enter the enforcer boundary.
	if svcName := r.Header.Get("X-Agency-Service"); svcName != "" {
		if ph.services != nil {
			cred := ph.services.Lookup(svcName)
			if cred != nil {
				// Scope check: validate tool has required scope (ASK Tenet 3+4)
				if toolName != "" {
					requiredScope, allowed := ph.services.CheckScope(svcName, toolName)
					if !allowed {
						ph.audit.Log(AuditEntry{
							Type:    "SCOPE_DENIED",
							EventID: eventID,
							Method:  r.Method,
							URL:     r.URL.String(),
							Host:    host,
							Status:  403,
							Extra: map[string]string{
								"service":        svcName,
								"tool":           toolName,
								"required_scope": requiredScope,
							},
						})
						http.Error(w, fmt.Sprintf(`{"error":"scope_denied","tool":%q,"required_scope":%q,"service":%q}`, toolName, requiredScope, svcName), http.StatusForbidden)
						return
					}
					ph.audit.Log(AuditEntry{
						Type:    "SCOPE_CHECK",
						EventID: eventID,
						Method:  r.Method,
						URL:     r.URL.String(),
						Host:    host,
						Status:  200,
					})
				}
				// Log scope pass — no credential injection (egress handles that)
				ph.audit.Log(AuditEntry{
					Type:    "SERVICE_SCOPE_PASSED",
					EventID: eventID,
					Method:  r.Method,
					URL:     r.URL.String(),
					Host:    host,
					Status:  200,
					Extra: map[string]string{
						"service":  svcName,
						"tool":     toolName,
						"api_base": cred.APIBase,
					},
				})
				// Rewrite URL to the service's API base (routing only, no credentials)
				if cred.APIBase != "" {
					// Parse APIBase to extract host and scheme.
					// The service definition stores a full URL like "https://slack.com".
					// r.URL.Host must be just the hostname; we also upgrade the scheme
					// so the egress proxy makes a proper HTTPS connection (body.py
					// downgrades https:// to http:// so httpx uses regular proxy
					// routing instead of CONNECT tunneling to the enforcer).
					if parsed, err := url.Parse(cred.APIBase); err == nil && parsed.Host != "" {
						r.URL.Host = parsed.Host
						if parsed.Scheme != "" {
							r.URL.Scheme = parsed.Scheme
						}
					} else {
						r.URL.Host = cred.APIBase
					}
				}
				// X-Agency-Service passes through to egress for credential resolution
			} else {
				ph.audit.Log(AuditEntry{
					Type:    "SERVICE_DENIED",
					EventID: eventID,
					Method:  r.Method,
					URL:     r.URL.String(),
					Host:    host,
					Status:  403,
				})
				http.Error(w, `{"error":"service not granted"}`, http.StatusForbidden)
				return
			}
		} else {
			ph.audit.Log(AuditEntry{
				Type:    "SERVICE_DENIED",
				EventID: eventID,
				Method:  r.Method,
				URL:     r.URL.String(),
				Host:    host,
				Status:  403,
			})
			http.Error(w, `{"error":"service not granted"}`, http.StatusForbidden)
			return
		}
	}

	// Build the outgoing request through egress proxy
	outURL := r.URL.String()
	if !strings.HasPrefix(outURL, "http") {
		outURL = "http://" + host + r.URL.RequestURI()
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, outURL, r.Body)
	if err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	// Forward only allowlisted headers (ASK Tenet 7: least privilege).
	// Prevents leaking hop-by-hop, proxy, privacy, or unknown headers.
	for _, allowed := range allowedForwardHeaders {
		if vals := r.Header.Values(allowed); len(vals) > 0 {
			for _, v := range vals {
				outReq.Header.Add(allowed, v)
			}
		}
	}
	// Inject agent identity on outgoing request (not in allowlist — enforcer-set)
	if ph.agentName != "" {
		outReq.Header.Set("X-Agency-Agent", ph.agentName)
	}

	// Use a transport that proxies through egress.
	// Force HTTP/1.1 to avoid ALPN negotiating HTTP/2 with the mitmproxy
	// egress, which causes a protocol preamble mismatch.
	proxyTransport := ph.transport.Clone()
	proxyTransport.Proxy = http.ProxyURL(mustParseURL(ph.egressProxy))
	proxyTransport.TLSClientConfig = &tls.Config{NextProtos: []string{"http/1.1"}}
	proxyTransport.ForceAttemptHTTP2 = false
	proxyTransport.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)

	resp, err := proxyTransport.RoundTrip(outReq)
	if err != nil {
		ph.audit.Log(AuditEntry{
			Type:    "HTTP_PROXY_ERROR",
			EventID: eventID,
			Method:  r.Method,
			URL:     outURL,
			Host:    host,
			Error:   err.Error(),
		})
		http.Error(w, `{"error":"upstream error"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Strip dangerous headers from response
	for _, h := range dangerousHeaders {
		resp.Header.Del(h)
	}

	// Copy response headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	ph.audit.Log(AuditEntry{
		Type:       "HTTP_PROXY",
		EventID:    eventID,
		Method:     r.Method,
		URL:        outURL,
		Host:       host,
		Status:     resp.StatusCode,
		DurationMs: time.Since(start).Milliseconds(),
	})
}

// HandleConnect handles CONNECT tunneling for HTTPS traffic.
func (ph *ProxyHandler) HandleConnect(w http.ResponseWriter, r *http.Request) {
	host := r.Host

	if err := validateHost(host); err != nil {
		http.Error(w, `{"error":"invalid target host"}`, http.StatusBadRequest)
		return
	}

	// Domain gate check
	if !ph.domainGate.Allowed(host) {
		ph.audit.Log(AuditEntry{
			Type:    "CONNECT_BLOCKED",
			EventID: r.Header.Get("X-Agency-Event-Id"),
			Method:  "CONNECT",
			Host:    host,
			Status:  403,
		})
		http.Error(w, `{"error":"domain blocked by policy"}`, http.StatusForbidden)
		return
	}

	// Connect to egress proxy
	egressHost := strings.TrimPrefix(ph.egressProxy, "http://")
	egressConn, err := net.DialTimeout("tcp", egressHost, 10*time.Second)
	if err != nil {
		ph.audit.Log(AuditEntry{
			Type:    "CONNECT_ERROR",
			EventID: r.Header.Get("X-Agency-Event-Id"),
			Method:  "CONNECT",
			Host:    host,
			Error:   fmt.Sprintf("egress dial: %v", err),
		})
		http.Error(w, `{"error":"egress unavailable"}`, http.StatusBadGateway)
		return
	}

	// Send CONNECT to egress
	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", host, host)
	if _, err := egressConn.Write([]byte(connectReq)); err != nil {
		egressConn.Close()
		http.Error(w, `{"error":"egress connect failed"}`, http.StatusBadGateway)
		return
	}

	// Read response from egress (just need the 200)
	buf := make([]byte, 4096)
	n, err := egressConn.Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "200") {
		egressConn.Close()
		http.Error(w, `{"error":"egress tunnel failed"}`, http.StatusBadGateway)
		return
	}

	// Hijack the client connection
	hj, ok := w.(http.Hijacker)
	if !ok {
		egressConn.Close()
		http.Error(w, `{"error":"hijack not supported"}`, http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hj.Hijack()
	if err != nil {
		egressConn.Close()
		http.Error(w, `{"error":"hijack failed"}`, http.StatusInternalServerError)
		return
	}

	// Send 200 to client
	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	ph.audit.Log(AuditEntry{
		Type:    "CONNECT_TUNNEL",
		EventID: r.Header.Get("X-Agency-Event-Id"),
		Method:  "CONNECT",
		Host:    host,
		Status:  200,
	})

	// Relay data bidirectionally
	go func() {
		defer egressConn.Close()
		defer clientConn.Close()
		io.Copy(egressConn, clientConn)
	}()
	go func() {
		defer egressConn.Close()
		defer clientConn.Close()
		io.Copy(clientConn, egressConn)
	}()
}

func mustParseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	if err != nil {
		log.Fatalf("invalid proxy URL: %s", rawURL)
	}
	return u
}
