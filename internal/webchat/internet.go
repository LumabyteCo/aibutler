package webchat

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
)

// InternetConfig controls internet-facing (TLS) mode for the web adapter.
type InternetConfig struct {
	Enabled     bool     // When true, bind to 0.0.0.0:443 with TLS
	TLSCertFile string   // Path to cert.pem (required for internet mode)
	TLSKeyFile  string   // Path to key.pem (required for internet mode)
	Domain      string   // Domain name (informational, logged on startup)
	IPAllowlist []string // Optional IP/CIDR restrictions (empty = allow all)
}

// StartInternet starts the webchat adapter in internet-facing mode with TLS.
// It requires TLSCertFile and TLSKeyFile; autocert is not used to avoid
// pulling in additional dependencies.
func (a *Adapter) StartInternet(ctx context.Context, handler channel.MessageHandler, cfg InternetConfig) error {
	if !cfg.Enabled {
		return fmt.Errorf("webchat: internet mode not enabled")
	}
	if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
		return fmt.Errorf("webchat: internet mode requires TLS cert and key files")
	}

	a.handler = handler

	mux := http.NewServeMux()

	staticHandler := http.FileServer(http.FS(staticFS))
	mux.Handle("/static/", staticHandler)
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/chat", a.handleIndex)
	mux.HandleFunc("/ws", a.handleWebSocket)
	mux.HandleFunc("/upload", a.handleUpload)

	// Mount extra handlers (PWA is wired via cli/app.go MountHandler).
	for prefix, h := range a.cfg.ExtraHandlers {
		mux.Handle(prefix, h)
	}
	for prefix, h := range a.extraHandlers {
		mux.Handle(prefix, h)
	}

	var finalHandler http.Handler = mux

	// Apply IP allowlist middleware if configured.
	if len(cfg.IPAllowlist) > 0 {
		allowlistMW, err := newIPAllowlistMiddleware(cfg.IPAllowlist)
		if err != nil {
			return fmt.Errorf("webchat: invalid IP allowlist: %w", err)
		}
		finalHandler = allowlistMW(mux)
	}

	// Apply reverse proxy header middleware.
	finalHandler = reverseProxyMiddleware(finalHandler)

	tlsCert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return fmt.Errorf("webchat: load TLS cert: %w", err)
	}

	a.server = &http.Server{
		Addr:    "0.0.0.0:443",
		Handler: finalHandler,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			MinVersion:   tls.VersionTLS12,
		},
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		domain := cfg.Domain
		if domain == "" {
			domain = "0.0.0.0"
		}
		log.Printf("webchat: internet mode listening on https://%s:443", domain)
		if err := a.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Printf("webchat: internet server error: %v", err)
		}
	}()

	return nil
}

// newIPAllowlistMiddleware creates middleware that restricts access to the given CIDRs.
func newIPAllowlistMiddleware(cidrs []string) (func(http.Handler) http.Handler, error) {
	var nets []*net.IPNet
	for _, cidr := range cidrs {
		// If no slash, treat as single IP.
		if !strings.Contains(cidr, "/") {
			if strings.Contains(cidr, ":") {
				cidr = cidr + "/128"
			} else {
				cidr = cidr + "/32"
			}
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("parse CIDR %q: %w", cidr, err)
		}
		nets = append(nets, ipNet)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := extractClientIP(r)
			ip := net.ParseIP(clientIP)
			if ip == nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			for _, ipNet := range nets {
				if ipNet.Contains(ip) {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}, nil
}

// extractClientIP gets the client IP, checking X-Forwarded-For first.
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (set by reverse proxies).
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}

	// Fall back to RemoteAddr.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// reverseProxyMiddleware reads standard reverse proxy headers and sets
// appropriate CORS headers for cross-origin requests.
func reverseProxyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers for cross-origin requests.
		// Security: Do not reflect arbitrary origins with credentials.
		// Only allow same-origin requests; omit CORS headers for cross-origin.
		origin := r.Header.Get("Origin")
		if origin != "" {
			// Allow same-host origins only (scheme may differ in dev).
			host := r.Host
			if host != "" && (origin == "https://"+host || origin == "http://"+host) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}

		// Handle preflight requests.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
