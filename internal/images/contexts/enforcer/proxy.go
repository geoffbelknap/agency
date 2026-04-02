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

// allowedForwardHeaders are the only request headers forwarded to upstream
// targets. This allowlist approach prevents leaking internal, hop-by-hop,
// or privacy-sensitive headers (ASK Tenet 7: least privilege).
// Use http.CanonicalHeaderKey format so map lookups match r.Header keys.
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

	// Domain gate check
	if !ph.domainGate.Allowed(host) {
		ph.audit.Log(AuditEntry{
			Type:   "DOMAIN_BLOCKED",
			Method: r.Method,
			URL:    r.URL.String(),
			Host:   host,
			Status: 403,
		})
		http.Error(w, `{"error":"domain blocked by policy"}`, http.StatusForbidden)
		return
	}

	// Service credential swap — always consume the header to prevent leaking
	// to egress, which would inject credentials without grant validation.
	if svcName := r.Header.Get("X-Agency-Service"); svcName != "" {
		r.Header.Del("X-Agency-Service")
		if ph.services != nil {
			cred := ph.services.Lookup(svcName)
			if cred != nil {
				r.Header.Set(cred.Header, cred.Value)
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
			} else {
				ph.audit.Log(AuditEntry{
					Type:   "SERVICE_DENIED",
					Method: r.Method,
					URL:    r.URL.String(),
					Host:   host,
					Status: 403,
				})
				http.Error(w, `{"error":"service not granted"}`, http.StatusForbidden)
				return
			}
		} else {
			ph.audit.Log(AuditEntry{
				Type:   "SERVICE_DENIED",
				Method: r.Method,
				URL:    r.URL.String(),
				Host:   host,
				Status: 403,
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

	// Copy only allowlisted headers to upstream (ASK Tenet 7: least privilege).
	// Blocklist approaches leak new/unknown headers; allowlist ensures only
	// known-safe headers are forwarded through the egress proxy.
	for _, allowed := range allowedForwardHeaders {
		if vv, ok := r.Header[allowed]; ok {
			for _, v := range vv {
				outReq.Header.Add(allowed, v)
			}
		}
	}

	// Inject agent identity for egress credential routing (ASK Tenet 4).
	// Set directly on outReq (not in allowlist) because this is an
	// infrastructure header injected by the enforcer, not agent-originated.
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
			Type:   "HTTP_PROXY_ERROR",
			Method: r.Method,
			URL:    outURL,
			Host:   host,
			Error:  err.Error(),
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
			Type:   "CONNECT_BLOCKED",
			Method: "CONNECT",
			Host:   host,
			Status: 403,
		})
		http.Error(w, `{"error":"domain blocked by policy"}`, http.StatusForbidden)
		return
	}

	// Connect to egress proxy
	egressHost := strings.TrimPrefix(ph.egressProxy, "http://")
	egressConn, err := net.DialTimeout("tcp", egressHost, 10*time.Second)
	if err != nil {
		ph.audit.Log(AuditEntry{
			Type:   "CONNECT_ERROR",
			Method: "CONNECT",
			Host:   host,
			Error:  fmt.Sprintf("egress dial: %v", err),
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
		Type:   "CONNECT_TUNNEL",
		Method: "CONNECT",
		Host:   host,
		Status: 200,
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
