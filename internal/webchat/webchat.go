package webchat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/media"
	"nhooyr.io/websocket"
)

// Config holds WebChat adapter settings.
type Config struct {
	Port           int
	BindAddress    string
	MaxUploadSize  int64 // bytes
	MaxConnections int   // max concurrent WebSocket connections (0 = 100 default)
	Pipeline       *media.Pipeline
	ExtraHandlers  map[string]http.Handler // Additional route prefixes to mount (e.g., "/api/dashboard/" -> handler)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Port:          8080,
		BindAddress:   "localhost",
		MaxUploadSize: 20 * 1024 * 1024, // 20 MB
	}
}

// wsFrame is the JSON structure exchanged over the WebSocket.
type wsFrame struct {
	Type   string `json:"type"` // "message", "typing", "file"
	Text   string `json:"text,omitempty"`
	FileID string `json:"file_id,omitempty"`
}

type wsConn struct {
	conn      *websocket.Conn
	accountID string
}

// Adapter implements channel.Channel for the built-in web chat.
type Adapter struct {
	cfg     Config
	server  *http.Server
	handler channel.MessageHandler

	mu            sync.RWMutex
	conns         map[string]*wsConn // accountID -> conn
	extraHandlers map[string]http.Handler
}

// New creates a WebChat adapter.
func New(cfg Config) *Adapter {
	return &Adapter{
		cfg:   cfg,
		conns: make(map[string]*wsConn),
	}
}

func (a *Adapter) Name() string { return "webchat" }

// MountHandler registers an additional HTTP handler at the given path prefix.
// Must be called before Start().
func (a *Adapter) MountHandler(prefix string, h http.Handler) {
	if a.extraHandlers == nil {
		a.extraHandlers = make(map[string]http.Handler)
	}
	a.extraHandlers[prefix] = h
}

// Start begins serving HTTP and WebSocket connections.
func (a *Adapter) Start(_ context.Context, handler channel.MessageHandler) error {
	a.handler = handler

	mux := http.NewServeMux()

	// Static files from embedded FS.
	staticHandler := http.FileServer(http.FS(staticFS))
	mux.Handle("/static/", staticHandler)
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/chat", a.handleIndex) // /chat route alias
	mux.HandleFunc("/ws", a.handleWebSocket)
	mux.HandleFunc("/upload", a.handleUpload)

	// Mount any extra handlers (e.g., dashboard, setup wizard, PWA).
	// PWA manifest + service worker are mounted by cli/app.go via
	// MountHandler so they're available in both local and internet modes.
	for prefix, h := range a.cfg.ExtraHandlers {
		mux.Handle(prefix, h)
	}
	for prefix, h := range a.extraHandlers {
		mux.Handle(prefix, h)
	}

	addr := fmt.Sprintf("%s:%d", a.cfg.BindAddress, a.cfg.Port)
	a.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("webchat: listening on %s", addr)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("webchat: server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server.
func (a *Adapter) Stop(ctx context.Context) error {
	if a.server == nil {
		return nil
	}

	// Close all WebSocket connections.
	a.mu.Lock()
	for id, wc := range a.conns {
		wc.conn.Close(websocket.StatusGoingAway, "server shutdown")
		delete(a.conns, id)
	}
	a.mu.Unlock()

	return a.server.Shutdown(ctx)
}

// Send sends a message to a connected WebSocket client.
func (a *Adapter) Send(ctx context.Context, accountID string, msg channel.OutgoingMessage) error {
	a.mu.RLock()
	wc, ok := a.conns[accountID]
	a.mu.RUnlock()
	if !ok {
		return fmt.Errorf("webchat: client %s not connected", accountID)
	}

	frame := wsFrame{Type: "message", Text: msg.Text}
	data, _ := json.Marshal(frame)
	return wc.conn.Write(ctx, websocket.MessageText, data)
}

// SendTyping sends a typing indicator to the client.
func (a *Adapter) SendTyping(ctx context.Context, accountID string) error {
	a.mu.RLock()
	wc, ok := a.conns[accountID]
	a.mu.RUnlock()
	if !ok {
		return nil // Silently ignore if not connected.
	}

	frame := wsFrame{Type: "typing"}
	data, _ := json.Marshal(frame)
	return wc.conn.Write(ctx, websocket.MessageText, data)
}

func (a *Adapter) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/chat" {
		http.NotFound(w, r)
		return
	}
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (a *Adapter) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Enforce connection limit to prevent resource exhaustion.
	maxConns := a.cfg.MaxConnections
	if maxConns <= 0 {
		maxConns = 100 // default limit
	}
	a.mu.RLock()
	currentConns := len(a.conns)
	a.mu.RUnlock()
	if currentConns >= maxConns {
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// InsecureSkipVerify allows any WebSocket origin. Safe for localhost binding
		// (default). For internet-facing deployments, the CORS middleware in
		// internet.go handles origin validation before reaching this handler.
		InsecureSkipVerify: a.cfg.BindAddress == "localhost" || a.cfg.BindAddress == "127.0.0.1" || a.cfg.BindAddress == "::1",
	})
	if err != nil {
		log.Printf("webchat: ws accept: %v", err)
		return
	}

	// Generate account ID from remote address for simplicity.
	accountID := r.RemoteAddr

	a.mu.Lock()
	a.conns[accountID] = &wsConn{conn: conn, accountID: accountID}
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		delete(a.conns, accountID)
		a.mu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "")
	}()

	ctx := r.Context()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return // Client disconnected.
		}

		var frame wsFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}

		if frame.Type == "message" && a.handler != nil {
			env := channel.Envelope{
				ID:        fmt.Sprintf("wc-%d", time.Now().UnixNano()),
				Channel:   "webchat",
				AccountID: accountID,
				Type:      channel.TypeText,
				Text:      frame.Text,
				Timestamp: time.Now(),
			}
			if err := a.handler(ctx, env); err != nil {
				log.Printf("webchat: handler error: %v", err)
			}
		}
	}
}

func (a *Adapter) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, a.cfg.MaxUploadSize)
	if err := r.ParseMultipartForm(a.cfg.MaxUploadSize); err != nil {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}

	fileID := fmt.Sprintf("upload-%d", time.Now().UnixNano())

	// Detect MIME type for the response and envelope.
	mimeType := media.DetectMIME(data, header.Filename)

	// Process through media pipeline if available.
	resp := map[string]interface{}{
		"file_id":  fileID,
		"filename": header.Filename,
		"size":     len(data),
	}
	if a.cfg.Pipeline != nil {
		result, err := a.cfg.Pipeline.Process(r.Context(), data, header.Filename)
		if err != nil {
			resp["processing_error"] = err.Error()
		} else {
			resp["type"] = result.Type
			resp["mime"] = result.MimeType
			if result.Language != "" {
				resp["language"] = result.Language
			}
		}
	}

	// Dispatch file as an Envelope so the router/agent can process it.
	if a.handler != nil {
		accountID := r.URL.Query().Get("account_id")
		if accountID == "" {
			accountID = r.RemoteAddr
		}
		env := channel.Envelope{
			ID:        fmt.Sprintf("wc-upload-%d", time.Now().UnixNano()),
			Channel:   "webchat",
			AccountID: accountID,
			Type:      channel.TypeFile,
			Text:      r.FormValue("message"),
			Attachments: []channel.Attachment{{
				Type:     channel.TypeFile,
				MimeType: mimeType,
				Data:     data,
				Filename: header.Filename,
				Size:     int64(len(data)),
			}},
			Timestamp: time.Now(),
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("PANIC recovered in webchat-upload: %v", r)
				}
			}()
			handlerCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := a.handler(handlerCtx, env); err != nil {
				log.Printf("webchat: upload handler error: %v", err)
			}
		}()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Verify interface compliance.
var _ channel.Channel = (*Adapter)(nil)
