package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// RegisterEnhancedRoutes adds the extended dashboard endpoints to the given mux.
// This is called from Handler() to extend the existing dashboard.
func (d *Dashboard) RegisterEnhancedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/dashboard/audit", d.handleAudit)
	mux.HandleFunc("/api/dashboard/capabilities", d.handleCapabilities)
	mux.HandleFunc("/api/dashboard/iot", d.handleIoT)
	mux.HandleFunc("/api/dashboard/plugins/full", d.handlePluginsFull)
	mux.HandleFunc("/api/dashboard/transactions", d.handleTransactions)
	mux.HandleFunc("/api/dashboard/config", d.handleConfig)
	mux.HandleFunc("/api/dashboard/config/schema", d.handleConfigSchema)
}

func (d *Dashboard) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	search := r.URL.Query().Get("search")
	action := r.URL.Query().Get("action")

	type auditEntry struct {
		ID        int    `json:"id"`
		Action    string `json:"action"`
		Resource  string `json:"resource"`
		AccountID string `json:"account_id"`
		Result    string `json:"result"`
		CreatedAt string `json:"created_at"`
	}

	var entries []auditEntry

	query := `SELECT id, action, COALESCE(resource,''), COALESCE(account_id,''), COALESCE(result,''), created_at
	          FROM resource_access_log`
	var conditions []string
	var args []interface{}

	if search != "" {
		conditions = append(conditions, "(action LIKE ? OR resource LIKE ?)")
		args = append(args, "%"+search+"%", "%"+search+"%")
	}
	if action != "" {
		conditions = append(conditions, "action = ?")
		args = append(args, action)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"entries": []auditEntry{}})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var e auditEntry
		if err := rows.Scan(&e.ID, &e.Action, &e.Resource, &e.AccountID, &e.Result, &e.CreatedAt); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []auditEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"entries": entries})
}

func (d *Dashboard) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	type capEntry struct {
		Capability string `json:"capability"`
		GrantCount int    `json:"grant_count"`
		LastUsed   string `json:"last_used"`
	}

	var caps []capEntry

	rows, err := d.db.QueryContext(ctx,
		`SELECT action, COUNT(*) as cnt, MAX(created_at) as last_used
		 FROM resource_access_log
		 GROUP BY action
		 ORDER BY cnt DESC LIMIT 50`)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"capabilities": []capEntry{}})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var c capEntry
		if err := rows.Scan(&c.Capability, &c.GrantCount, &c.LastUsed); err != nil {
			continue
		}
		caps = append(caps, c)
	}
	if caps == nil {
		caps = []capEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"capabilities": caps})
}

func (d *Dashboard) handleIoT(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	type iotDevice struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Type     string `json:"type"`
		State    string `json:"state"`
		LastSeen string `json:"last_seen"`
	}

	var devices []iotDevice

	rows, err := d.db.QueryContext(ctx,
		`SELECT id, COALESCE(name,''), COALESCE(type,''), COALESCE(state,''), COALESCE(last_seen,'')
		 FROM iot_devices ORDER BY name LIMIT 100`)
	if err != nil {
		// Table may not exist; return empty array.
		writeJSON(w, http.StatusOK, map[string]interface{}{"devices": []iotDevice{}})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var d iotDevice
		if err := rows.Scan(&d.ID, &d.Name, &d.Type, &d.State, &d.LastSeen); err != nil {
			continue
		}
		devices = append(devices, d)
	}
	if devices == nil {
		devices = []iotDevice{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"devices": devices})
}

func (d *Dashboard) handlePluginsFull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	type pluginInfo struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		Version      string `json:"version"`
		Author       string `json:"author"`
		Enabled      bool   `json:"enabled"`
		InstalledAt  string `json:"installed_at"`
		Capabilities string `json:"capabilities"`
	}

	var plugins []pluginInfo

	rows, err := d.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(version,''), COALESCE(author,''),
		        enabled, COALESCE(installed_at,''), COALESCE(capabilities,'')
		 FROM plugins ORDER BY name LIMIT 100`)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"plugins": []pluginInfo{}})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p pluginInfo
		var enabled int
		if err := rows.Scan(&p.ID, &p.Name, &p.Version, &p.Author, &enabled, &p.InstalledAt, &p.Capabilities); err != nil {
			continue
		}
		p.Enabled = enabled != 0
		plugins = append(plugins, p)
	}
	if plugins == nil {
		plugins = []pluginInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"plugins": plugins})
}

func (d *Dashboard) handleTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	type txEntry struct {
		ID          int     `json:"id"`
		Type        string  `json:"type"`
		Description string  `json:"description"`
		AmountUSD   float64 `json:"amount_usd"`
		Status      string  `json:"status"`
		CreatedAt   string  `json:"created_at"`
	}

	var entries []txEntry

	rows, err := d.db.QueryContext(ctx,
		`SELECT id, COALESCE(type,''), COALESCE(description,''),
		        COALESCE(amount_usd,0), COALESCE(status,''), COALESCE(created_at,'')
		 FROM transaction_audit
		 ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"transactions": []txEntry{}, "total_usd": 0})
		return
	}
	defer rows.Close()

	var totalUSD float64
	for rows.Next() {
		var e txEntry
		if err := rows.Scan(&e.ID, &e.Type, &e.Description, &e.AmountUSD, &e.Status, &e.CreatedAt); err != nil {
			continue
		}
		totalUSD += e.AmountUSD
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []txEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"transactions": entries, "total_usd": totalUSD})
}

func (d *Dashboard) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.handleConfigGet(w, r)
	case http.MethodPut:
		d.handleConfigPut(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (d *Dashboard) handleConfigGet(w http.ResponseWriter, _ *http.Request) {
	// Return a stub indicating config API is available.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "available",
		"note":   "Use PUT to update configuration parameters",
	})
}

func (d *Dashboard) handleConfigPut(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 1MB to prevent memory exhaustion from oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Store config updates in the database for persistence.
	ctx := r.Context()
	for key, value := range body {
		valJSON, _ := json.Marshal(value)
		_, err := d.db.ExecContext(ctx,
			`INSERT INTO config_overrides (key, value, updated_at) VALUES (?, ?, datetime('now'))
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			key, string(valJSON))
		if err != nil {
			// Table may not exist; that's okay.
			writeError(w, http.StatusInternalServerError, "failed to save config")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (d *Dashboard) handleConfigSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Return the Three Enriches schema description.
	schema := map[string]interface{}{
		"layers": []map[string]interface{}{
			{
				"name":        "settings",
				"description": "User preferences (everyone sees these)",
				"fields": []map[string]string{
					{"name": "language", "type": "string", "description": "Interface language"},
					{"name": "timezone", "type": "string", "description": "User timezone"},
					{"name": "notifications", "type": "boolean", "description": "Enable notifications"},
					{"name": "model", "type": "string", "description": "Primary AI model"},
					{"name": "persona_name", "type": "string", "description": "Butler persona name"},
					{"name": "cost.strategy", "type": "enum:frugal,balanced,quality", "description": "Cost strategy"},
					{"name": "cost.monthly_budget", "type": "number", "description": "Monthly budget in USD"},
				},
			},
			{
				"name":        "configurations",
				"description": "System wiring (power users)",
				"fields": []map[string]string{
					{"name": "models.primary", "type": "string", "description": "Primary model identifier"},
					{"name": "models.fallback", "type": "string", "description": "Fallback model identifier"},
					{"name": "agents.max_concurrent", "type": "integer", "description": "Max concurrent agents"},
					{"name": "agents.max_depth", "type": "integer", "description": "Max delegation depth"},
					{"name": "web.port", "type": "integer", "description": "Web server port"},
					{"name": "web.bind_address", "type": "string", "description": "Web server bind address"},
				},
			},
			{
				"name":        "options",
				"description": "Technical tuning (developers)",
				"fields": []map[string]string{
					{"name": "models.max_tokens", "type": "integer", "description": "Max tokens per request"},
					{"name": "models.temperature", "type": "number", "description": "Model temperature (0-2)"},
					{"name": "agents.max_tool_calls", "type": "integer", "description": "Max tool calls per turn"},
					{"name": "prompts.max_tier1_tokens", "type": "integer", "description": "Max tier 1 prompt tokens"},
				},
			},
		},
	}

	writeJSON(w, http.StatusOK, schema)
}
