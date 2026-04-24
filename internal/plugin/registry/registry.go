package registry

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/LumabyteCo/aibutler/internal/plugin"
	"github.com/LumabyteCo/aibutler/internal/plugin/defense"
	"github.com/LumabyteCo/aibutler/internal/plugin/host"
	"github.com/LumabyteCo/aibutler/internal/plugin/manifest"
)

// ToolRegistry is a narrow interface for registering/unregistering tools.
type ToolRegistry interface {
	Register(t ToolLike)
	UnregisterPrefix(prefix string)
}

// ToolLike matches tool.Tool without importing the tool package (avoids cycle).
type ToolLike interface {
	Name() string
	Description() string
	Schema() string
	Execute(ctx context.Context, input string) (string, error)
	Capability() string
}

// PluginInfo contains metadata about an installed plugin.
type PluginInfo struct {
	ID           int64
	Name         string
	Version      string
	Status       string // enabled, disabled, error
	Capabilities []string
	ManifestHash string
	WASMHash     string
}

// Registry manages plugin lifecycle: install, enable, disable, remove.
type Registry struct {
	mu         sync.Mutex
	db         *sql.DB
	runtime    plugin.Runtime
	toolReg    ToolRegistry
	toolCaller host.ToolCaller
	vault      host.VaultGetter
	auditor    host.AuditWriter
	logger     host.Logger
	pluginDir  string
	maxPlugins int // 0 means unlimited
}

// New creates a plugin registry. maxPlugins=0 means unlimited.
func New(db *sql.DB, runtime plugin.Runtime, toolReg ToolRegistry, toolCaller host.ToolCaller, vault host.VaultGetter, auditor host.AuditWriter, pluginDir string, maxPlugins int) *Registry {
	return &Registry{
		db:         db,
		runtime:    runtime,
		toolReg:    toolReg,
		toolCaller: toolCaller,
		vault:      vault,
		auditor:    auditor,
		pluginDir:  pluginDir,
		maxPlugins: maxPlugins,
	}
}

// SetLogger sets the logger used by plugins.
func (r *Registry) SetLogger(l host.Logger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logger = l
}

// Install parses a manifest, runs defense checks, and inserts plugin metadata into the database.
// Returns the plugin info and any defense warnings. Does NOT enable the plugin.
func (r *Registry) Install(ctx context.Context, manifestPath string) (*PluginInfo, []string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Enforce max plugins limit.
	if r.maxPlugins > 0 {
		var count int
		if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plugins`).Scan(&count); err == nil && count >= r.maxPlugins {
			return nil, nil, fmt.Errorf("max plugins limit reached (%d)", r.maxPlugins)
		}
	}

	// Parse and validate manifest.
	m, err := manifest.ParseFile(manifestPath)
	if err != nil {
		return nil, nil, fmt.Errorf("parse manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, nil, fmt.Errorf("validate manifest: %w", err)
	}

	// L1 sandbox check.
	if err := defense.ValidateSandbox(m); err != nil {
		return nil, nil, err
	}

	// L2 capability audit.
	auditResult := defense.AuditCapabilities(m.Capabilities)
	if !auditResult.Passed {
		return nil, nil, fmt.Errorf("defense audit failed: %v", auditResult.Critical)
	}

	// Resolve WASM path relative to manifest.
	dir := filepath.Dir(manifestPath)
	wasmPath := m.WASMPath
	if !filepath.IsAbs(wasmPath) {
		wasmPath = filepath.Join(dir, wasmPath)
	}

	// Read and hash WASM file.
	wasmData, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read wasm file: %w", err)
	}
	wasmHash := sha256Hex(wasmData)

	// Hash manifest.
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read manifest: %w", err)
	}
	manifestHash := sha256Hex(manifestData)

	// Marshal capabilities.
	capsJSON, _ := json.Marshal(m.Capabilities)

	// Insert or update in database.
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO plugins (name, version, manifest_hash, wasm_hash, status, capabilities)
		 VALUES (?, ?, ?, ?, 'disabled', ?)
		 ON CONFLICT(name) DO UPDATE SET
		   version = excluded.version,
		   manifest_hash = excluded.manifest_hash,
		   wasm_hash = excluded.wasm_hash,
		   capabilities = excluded.capabilities,
		   updated_at = datetime('now')`,
		m.Name, m.Version, manifestHash, wasmHash, string(capsJSON))
	if err != nil {
		return nil, nil, fmt.Errorf("insert plugin: %w", err)
	}

	id, _ := result.LastInsertId()
	// If ON CONFLICT updated, LastInsertId may be 0; fetch the actual ID.
	if id == 0 {
		if err := r.db.QueryRowContext(ctx, `SELECT id FROM plugins WHERE name = ?`, m.Name).Scan(&id); err != nil {
			return nil, nil, fmt.Errorf("fetch plugin id for %s: %w", m.Name, err)
		}
	}

	info := &PluginInfo{
		ID:           id,
		Name:         m.Name,
		Version:      m.Version,
		Status:       "disabled",
		Capabilities: m.Capabilities,
		ManifestHash: manifestHash,
		WASMHash:     wasmHash,
	}

	return info, auditResult.Warnings, nil
}

// Enable loads a plugin's WASM module and registers its tools.
func (r *Registry) Enable(ctx context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.enableLocked(ctx, name)
}

func (r *Registry) enableLocked(ctx context.Context, name string) error {
	// Fetch plugin info from DB.
	info, manifestPath, err := r.fetchPlugin(ctx, name)
	if err != nil {
		return err
	}

	// Parse manifest to get tool definitions.
	m, err := manifest.ParseFile(manifestPath)
	if err != nil {
		return fmt.Errorf("parse manifest for %s: %w", name, err)
	}

	// Re-verify WASM hash to detect tampering since install (TOCTOU defense).
	wasmPath := m.WASMPath
	if !filepath.IsAbs(wasmPath) {
		wasmPath = filepath.Join(filepath.Dir(manifestPath), wasmPath)
	}
	wasmData, err := os.ReadFile(wasmPath)
	if err != nil {
		_ = r.setStatus(ctx, name, "error")
		return fmt.Errorf("read wasm for hash verification %s: %w", name, err)
	}
	if currentHash := sha256Hex(wasmData); currentHash != info.WASMHash {
		_ = r.setStatus(ctx, name, "error")
		return fmt.Errorf("wasm hash mismatch for %s: expected %s, got %s (possible tampering)", name, info.WASMHash[:12], currentHash[:12])
	}

	// Build host deps.
	deps := host.Deps{
		ToolCaller: r.toolCaller,
		Vault:      r.vault,
		Auditor:    r.auditor,
		Logger:     r.logger,
		Caps:       info.Capabilities,
		PluginID:   info.ID,
		PluginName: name,
	}

	// Load WASM.
	dir := filepath.Dir(manifestPath)
	if err := r.runtime.Load(ctx, dir, m, deps); err != nil {
		_ = r.setStatus(ctx, name, "error")
		return fmt.Errorf("load plugin %s: %w", name, err)
	}

	// Register tools.
	for _, toolDef := range m.Tools {
		pt := plugin.NewPluginTool(r.runtime, name, toolDef)
		r.toolReg.Register(pt)
	}

	// Update status.
	return r.setStatus(ctx, name, "enabled")
}

// Disable unloads a plugin and unregisters its tools.
func (r *Registry) Disable(ctx context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.disableLocked(ctx, name)
}

func (r *Registry) disableLocked(ctx context.Context, name string) error {
	if r.runtime.IsLoaded(name) {
		if err := r.runtime.Unload(ctx, name); err != nil {
			return fmt.Errorf("unload plugin %s: %w", name, err)
		}
	}

	r.toolReg.UnregisterPrefix("plugin." + name + ".")

	return r.setStatus(ctx, name, "disabled")
}

// Remove disables and deletes a plugin from the database.
func (r *Registry) Remove(ctx context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Disable first (safe if already disabled).
	if r.runtime.IsLoaded(name) {
		if err := r.disableLocked(ctx, name); err != nil {
			return fmt.Errorf("disable before remove %s: %w", name, err)
		}
	}

	result, err := r.db.ExecContext(ctx, `DELETE FROM plugins WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete plugin %s: %w", name, err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("plugin %q not found", name)
	}
	return nil
}

// List returns all installed plugins.
func (r *Registry) List(ctx context.Context) ([]PluginInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, version, status, capabilities, manifest_hash, wasm_hash FROM plugins ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plugins []PluginInfo
	for rows.Next() {
		var p PluginInfo
		var capsJSON string
		if err := rows.Scan(&p.ID, &p.Name, &p.Version, &p.Status, &capsJSON, &p.ManifestHash, &p.WASMHash); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(capsJSON), &p.Capabilities); err != nil {
			return nil, fmt.Errorf("unmarshal capabilities for %s: %w", p.Name, err)
		}
		plugins = append(plugins, p)
	}
	return plugins, rows.Err()
}

// Get returns info for a single plugin.
func (r *Registry) Get(ctx context.Context, name string) (*PluginInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var p PluginInfo
	var capsJSON string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, version, status, capabilities, manifest_hash, wasm_hash FROM plugins WHERE name = ?`, name).
		Scan(&p.ID, &p.Name, &p.Version, &p.Status, &capsJSON, &p.ManifestHash, &p.WASMHash)
	if err != nil {
		return nil, fmt.Errorf("plugin %q not found", name)
	}
	if err := json.Unmarshal([]byte(capsJSON), &p.Capabilities); err != nil {
		return nil, fmt.Errorf("unmarshal capabilities for %s: %w", name, err)
	}
	return &p, nil
}

// EnableAll loads all plugins with status='enabled'. Errors are logged, not fatal.
func (r *Registry) EnableAll(ctx context.Context) []error {
	r.mu.Lock()
	defer r.mu.Unlock()

	rows, err := r.db.QueryContext(ctx, `SELECT name FROM plugins WHERE status = 'enabled'`)
	if err != nil {
		return []error{err}
	}
	defer rows.Close()

	var errs []error
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			errs = append(errs, err)
			continue
		}
		names = append(names, name)
	}

	for _, name := range names {
		if err := r.enableLocked(ctx, name); err != nil {
			errs = append(errs, fmt.Errorf("enable %s: %w", name, err))
			_ = r.setStatus(ctx, name, "error")
		}
	}
	return errs
}

// DisableAll unloads all loaded plugins.
func (r *Registry) DisableAll(ctx context.Context) []error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for _, name := range r.runtime.Loaded() {
		if err := r.disableLocked(ctx, name); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// fetchPlugin reads plugin info from the DB and resolves manifest path.
func (r *Registry) fetchPlugin(ctx context.Context, name string) (*PluginInfo, string, error) {
	var p PluginInfo
	var capsJSON string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, version, status, capabilities, manifest_hash, wasm_hash FROM plugins WHERE name = ?`, name).
		Scan(&p.ID, &p.Name, &p.Version, &p.Status, &capsJSON, &p.ManifestHash, &p.WASMHash)
	if err != nil {
		return nil, "", fmt.Errorf("plugin %q not found", name)
	}
	if err := json.Unmarshal([]byte(capsJSON), &p.Capabilities); err != nil {
		return nil, "", fmt.Errorf("unmarshal capabilities for %s: %w", name, err)
	}

	manifestPath := filepath.Join(r.pluginDir, name, "plugin.toml")
	return &p, manifestPath, nil
}

func (r *Registry) setStatus(ctx context.Context, name, status string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE plugins SET status = ?, updated_at = datetime('now') WHERE name = ?`, status, name)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("plugin %q not found", name)
	}
	return nil
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
