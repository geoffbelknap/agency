package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type MediationProxy struct {
	proxies    map[string]*httputil.ReverseProxy
	targets    map[string]string
	audit      *AuditLogger
	reqCount   int64     // atomic counter
	reqResetAt time.Time // reset every minute
	reqLimit   int64     // max requests per minute (0 = unlimited)
}

func NewMediationProxy(serviceURLs map[string]string, audit *AuditLogger) *MediationProxy {
	proxies := make(map[string]*httputil.ReverseProxy, len(serviceURLs))
	targets := make(map[string]string, len(serviceURLs))
	for name, rawURL := range serviceURLs {
		targets[name] = rawURL
		target, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		rp := httputil.NewSingleHostReverseProxy(target)
		rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, fmt.Sprintf(`{"error":"mediation proxy: %s"}`, err), http.StatusBadGateway)
		}
		proxies[name] = rp
	}
	return &MediationProxy{
		proxies:    proxies,
		targets:    targets,
		audit:      audit,
		reqResetAt: time.Now().Add(time.Minute),
		reqLimit:   600,
	}
}

// statusCapture wraps ResponseWriter to capture the status code.
type statusCapture struct {
	http.ResponseWriter
	status int
}

func (sc *statusCapture) WriteHeader(code int) {
	sc.status = code
	sc.ResponseWriter.WriteHeader(code)
}

func (mp *MediationProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	now := time.Now()
	if now.After(mp.reqResetAt) {
		atomic.StoreInt64(&mp.reqCount, 0)
		mp.reqResetAt = now.Add(time.Minute)
	}
	if mp.reqLimit > 0 && atomic.AddInt64(&mp.reqCount, 1) > mp.reqLimit {
		http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/mediation/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 {
		http.Error(w, `{"error":"missing service name"}`, http.StatusBadRequest)
		return
	}

	service := parts[0]
	remainder := "/"
	if len(parts) > 1 {
		remainder = "/" + parts[1]
	}

	targetURL, ok := mp.targets[service]
	if !ok {
		http.Error(w, fmt.Sprintf(`{"error":"unknown mediation service: %s"}`, service), http.StatusBadGateway)
		return
	}

	if isWebSocketUpgrade(r) {
		mp.audit.Log(AuditEntry{
			Type:    "MEDIATION_WS",
			EventID: r.Header.Get("X-Agency-Event-Id"),
			Method:  r.Method,
			URL:     remainder,
			Host:    service,
			Service: service,
		})
		mp.proxyWebSocket(w, r, targetURL, remainder)
		return
	}

	proxy, ok := mp.proxies[service]
	if !ok {
		http.Error(w, fmt.Sprintf(`{"error":"unknown mediation service: %s"}`, service), http.StatusBadGateway)
		return
	}
	r.URL.Path = remainder
	r.URL.RawPath = ""
	wrapped := &statusCapture{ResponseWriter: w, status: 200}
	proxy.ServeHTTP(wrapped, r)

	mp.audit.Log(AuditEntry{
		Type:       "MEDIATION_PROXY",
		EventID:    r.Header.Get("X-Agency-Event-Id"),
		Method:     r.Method,
		URL:        remainder,
		Host:       service,
		Service:    service,
		Status:     wrapped.status,
		DurationMs: time.Since(start).Milliseconds(),
	})
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func (mp *MediationProxy) proxyWebSocket(w http.ResponseWriter, r *http.Request, targetBase, path string) {
	target, err := url.Parse(targetBase)
	if err != nil {
		http.Error(w, `{"error":"invalid target"}`, http.StatusBadGateway)
		return
	}

	targetAddr := target.Host
	if !strings.Contains(targetAddr, ":") {
		targetAddr += ":80"
	}

	backendConn, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"backend connect: %s"}`, err), http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		backendConn.Close()
		http.Error(w, `{"error":"hijack not supported"}`, http.StatusInternalServerError)
		return
	}
	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		backendConn.Close()
		return
	}

	// Forward the original request to backend with rewritten path
	if r.URL.RawQuery != "" {
		fmt.Fprintf(backendConn, "%s %s?%s HTTP/1.1\r\n", r.Method, path, r.URL.RawQuery)
	} else {
		fmt.Fprintf(backendConn, "%s %s HTTP/1.1\r\n", r.Method, path)
	}
	fmt.Fprintf(backendConn, "Host: %s\r\n", target.Host)
	for key, values := range r.Header {
		for _, v := range values {
			fmt.Fprintf(backendConn, "%s: %s\r\n", key, v)
		}
	}
	fmt.Fprintf(backendConn, "\r\n")

	if clientBuf.Reader.Buffered() > 0 {
		buffered := make([]byte, clientBuf.Reader.Buffered())
		clientBuf.Read(buffered)
		backendConn.Write(buffered)
	}

	done := make(chan struct{}, 2)
	go func() { io.Copy(clientConn, backendConn); done <- struct{}{} }()
	go func() { io.Copy(backendConn, clientConn); done <- struct{}{} }()
	<-done

	clientConn.Close()
	backendConn.Close()
}
