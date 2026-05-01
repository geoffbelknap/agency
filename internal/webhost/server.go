package webhost

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Options struct {
	DistDir    string
	AgencyHome string
	Host       string
	Port       string
}

type configFile struct {
	Token       string `yaml:"token"`
	GatewayAddr string `yaml:"gateway_addr,omitempty"`
}

func Serve(opts Options) error {
	handler, err := Handler(opts)
	if err != nil {
		return err
	}
	host := strings.TrimSpace(opts.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	port := strings.TrimSpace(opts.Port)
	if port == "" {
		port = "8280"
	}
	server := &http.Server{
		Addr:              netAddr(host, port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server.ListenAndServe()
}

func Handler(opts Options) (http.Handler, error) {
	distDir := strings.TrimSpace(opts.DistDir)
	if distDir == "" {
		return nil, fmt.Errorf("web dist directory is required")
	}
	if info, err := os.Stat(filepath.Join(distDir, "index.html")); err != nil || info.IsDir() {
		if err == nil {
			err = fmt.Errorf("index.html is a directory")
		}
		return nil, fmt.Errorf("web dist is not built at %s: %w", distDir, err)
	}

	state := &runtimeConfig{agencyHome: opts.AgencyHome}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("/__agency/config", func(w http.ResponseWriter, _ *http.Request) {
		token, _ := state.read()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token, "gateway": ""})
	})
	mux.Handle("/api/v1/", state.proxy(false))
	mux.Handle("/ws", state.proxy(true))
	mux.Handle("/", staticHandler(distDir))
	return mux, nil
}

type runtimeConfig struct {
	agencyHome string
}

func (c *runtimeConfig) read() (string, string) {
	agencyHome := strings.TrimSpace(c.agencyHome)
	if agencyHome == "" {
		agencyHome = os.Getenv("AGENCY_HOME")
	}
	if agencyHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			agencyHome = filepath.Join(home, ".agency")
		}
	}
	cfg := configFile{GatewayAddr: "127.0.0.1:8200"}
	data, err := os.ReadFile(filepath.Join(agencyHome, "config.yaml"))
	if err == nil {
		_ = yaml.Unmarshal(data, &cfg)
	}
	addr := strings.TrimSpace(cfg.GatewayAddr)
	if addr == "" {
		addr = "127.0.0.1:8200"
	}
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	addr = strings.TrimPrefix(addr, "0.0.0.0")
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	if strings.HasPrefix(addr, "[::]") {
		addr = "127.0.0.1" + strings.TrimPrefix(addr, "[::]")
	}
	return strings.TrimSpace(cfg.Token), addr
}

func (c *runtimeConfig) proxy(websocket bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, addr := c.read()
		target, err := url.Parse("http://" + addr)
		if err != nil {
			http.Error(w, "invalid gateway address", http.StatusBadGateway)
			return
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			req.Host = target.Host
			if websocket && token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
		}
		proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, "gateway proxy failed: "+err.Error(), http.StatusBadGateway)
		}
		proxy.ServeHTTP(w, r)
	})
}

func staticHandler(distDir string) http.Handler {
	files := http.FileServer(http.Dir(distDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		if clean == "/" {
			serveIndex(w, r, distDir)
			return
		}
		if filepath.Ext(clean) == "" {
			serveIndex(w, r, distDir)
			return
		}
		files.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, distDir string) {
	http.ServeFile(w, r, filepath.Join(distDir, "index.html"))
}

func netAddr(host, port string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return host + ":" + port
}
