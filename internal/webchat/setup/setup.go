package setup

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/LumabyteCo/aibutler/internal/config"
)

// Step describes a single setup step.
type Step struct {
	Name      string `json:"name"`
	Completed bool   `json:"completed"`
}

// Wizard provides a first-run setup flow via HTTP API.
type Wizard struct {
	mu         sync.Mutex
	configPath string
	config     *config.Config
	completed  bool
	steps      map[string]bool
}

// New creates a setup wizard.
func New(configPath string, cfg *config.Config) *Wizard {
	return &Wizard{
		configPath: configPath,
		config:     cfg,
		steps:      make(map[string]bool),
	}
}

// IsComplete returns whether the setup has been completed.
func (w *Wizard) IsComplete() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.completed
}

// Handler returns an http.Handler with all setup API routes.
func (w *Wizard) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/setup/status", w.handleStatus)
	mux.HandleFunc("/api/setup/model", w.handleModel)
	mux.HandleFunc("/api/setup/channels", w.handleChannels)
	mux.HandleFunc("/api/setup/complete", w.handleComplete)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (w *Wizard) handleStatus(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	steps := []Step{
		{Name: "model", Completed: w.steps["model"]},
		{Name: "channels", Completed: w.steps["channels"]},
	}

	writeJSON(rw, http.StatusOK, map[string]interface{}{
		"completed": w.completed,
		"steps":     steps,
	})
}

func (w *Wizard) handleModel(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if w.IsComplete() {
		writeError(rw, http.StatusGone, "setup already completed")
		return
	}

	// Limit request body to 1MB to prevent OOM from oversized payloads.
	r.Body = http.MaxBytesReader(rw, r.Body, 1024*1024)
	var body struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		APIKey   string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(rw, http.StatusBadRequest, "invalid JSON")
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if body.Model != "" {
		w.config.Configurations.Models.Primary = body.Model
		w.config.Settings.Model = body.Model
	}
	w.steps["model"] = true

	writeJSON(rw, http.StatusOK, map[string]string{"status": "configured"})
}

func (w *Wizard) handleChannels(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if w.IsComplete() {
		writeError(rw, http.StatusGone, "setup already completed")
		return
	}

	// Limit request body to 1MB to prevent OOM from oversized payloads.
	r.Body = http.MaxBytesReader(rw, r.Body, 1024*1024)
	var body struct {
		ActiveChannels []string `json:"active_channels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(rw, http.StatusBadRequest, "invalid JSON")
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if len(body.ActiveChannels) > 0 {
		w.config.Settings.ActiveChannels = body.ActiveChannels
	}
	w.steps["channels"] = true

	writeJSON(rw, http.StatusOK, map[string]string{"status": "configured"})
}

func (w *Wizard) handleComplete(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if w.IsComplete() {
		writeError(rw, http.StatusGone, "setup already completed")
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Write config to disk.
	if w.configPath != "" {
		if err := w.writeConfig(); err != nil {
			writeError(rw, http.StatusInternalServerError, fmt.Sprintf("write config: %v", err))
			return
		}
	}

	w.completed = true
	writeJSON(rw, http.StatusOK, map[string]string{"status": "complete"})
}

// writeConfig writes the current config to the config path as YAML.
// Caller must hold w.mu.
func (w *Wizard) writeConfig() error {
	data, err := yaml.Marshal(w.config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(w.configPath, data, 0600)
}
