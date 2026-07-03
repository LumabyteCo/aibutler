package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/memory"
)

// AccessLogger records dashboard-initiated memory mutations into the same
// audit trail tool calls use, so one query still answers "what changed and
// who asked for it" regardless of the surface it came from.
type AccessLogger interface {
	LogAccess(ctx context.Context, entry capability.AuditEntry) error
}

// SetFactStore wires the Memories panel's fact endpoints. Nil (the default)
// leaves the endpoints responding 503 — the read-only panel keeps working.
func (d *Dashboard) SetFactStore(store *memory.Store, auditor AccessLogger) {
	d.facts = store
	d.auditor = auditor
}

// registerFactRoutes adds the Memories panel's fact-quality endpoints.
func (d *Dashboard) registerFactRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/dashboard/memory/facts", d.handleFactsList)
	mux.HandleFunc("/api/dashboard/memory/facts/correct", d.handleFactCorrect)
	mux.HandleFunc("/api/dashboard/memory/facts/forget", d.handleFactForget)
	mux.HandleFunc("/api/dashboard/memory/facts/pin", d.handleFactPin)
	mux.HandleFunc("/api/dashboard/memory/facts/importance", d.handleFactImportance)
	mux.HandleFunc("/api/dashboard/memory/conflicts", d.handleConflictsList)
	mux.HandleFunc("/api/dashboard/memory/conflicts/review", d.handleConflictReview)
}

// auditMemoryAction best-effort records a panel-initiated mutation.
func (d *Dashboard) auditMemoryAction(ctx context.Context, action, target, status, errMsg string) {
	if d.auditor == nil {
		return
	}
	_ = d.auditor.LogAccess(ctx, capability.AuditEntry{
		AgentType:      "dashboard",
		ResourceType:   "memory",
		Service:        "memories_panel",
		Action:         action,
		Target:         target,
		CapabilityUsed: "memory.write",
		Status:         status,
		Error:          errMsg,
	})
}

func (d *Dashboard) requireFacts(w http.ResponseWriter) bool {
	if d.facts == nil {
		writeError(w, http.StatusServiceUnavailable, "memory store not configured")
		return false
	}
	return true
}

func (d *Dashboard) handleFactsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !d.requireFacts(w) {
		return
	}
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	facts, err := d.facts.GetKeyFacts(r.Context(), r.URL.Query().Get("category"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"facts": facts})
}

func (d *Dashboard) handleConflictsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !d.requireFacts(w) {
		return
	}
	onlyUnreviewed := r.URL.Query().Get("unreviewed") == "1"
	conflicts, err := d.facts.GetConflicts(r.Context(), onlyUnreviewed, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"conflicts": conflicts})
}

func (d *Dashboard) handleFactCorrect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !d.requireFacts(w) {
		return
	}
	var req struct {
		ID   int64  `json:"id"`
		Fact string `json:"fact"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == 0 || req.Fact == "" {
		writeError(w, http.StatusBadRequest, "id and fact are required")
		return
	}
	newID, err := d.facts.CorrectFact(r.Context(), req.ID, req.Fact)
	if err != nil {
		d.auditMemoryAction(r.Context(), "fact.correct", strconv.FormatInt(req.ID, 10), "error", err.Error())
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.auditMemoryAction(r.Context(), "fact.correct", strconv.FormatInt(req.ID, 10), "success", "")
	writeJSON(w, http.StatusOK, map[string]interface{}{"new_id": newID})
}

func (d *Dashboard) handleFactForget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !d.requireFacts(w) {
		return
	}
	var req struct {
		FactID    int64 `json:"fact_id"`
		ThoughtID int64 `json:"thought_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.FactID == 0) == (req.ThoughtID == 0) {
		writeError(w, http.StatusBadRequest, "exactly one of fact_id or thought_id is required")
		return
	}
	if req.FactID != 0 {
		if err := d.facts.ForgetFact(r.Context(), req.FactID); err != nil {
			d.auditMemoryAction(r.Context(), "fact.forget", strconv.FormatInt(req.FactID, 10), "error", err.Error())
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		d.auditMemoryAction(r.Context(), "fact.forget", strconv.FormatInt(req.FactID, 10), "success", "")
		writeJSON(w, http.StatusOK, map[string]interface{}{"deleted": "fact"})
		return
	}
	res, err := d.facts.ForgetThought(r.Context(), req.ThoughtID)
	if err != nil {
		d.auditMemoryAction(r.Context(), "thought.forget", strconv.FormatInt(req.ThoughtID, 10), "error", err.Error())
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.auditMemoryAction(r.Context(), "thought.forget", strconv.FormatInt(req.ThoughtID, 10), "success", "")
	writeJSON(w, http.StatusOK, map[string]interface{}{"deleted": "thought", "cascade": res})
}

func (d *Dashboard) handleFactPin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !d.requireFacts(w) {
		return
	}
	var req struct {
		ID     int64 `json:"id"`
		Pinned bool  `json:"pinned"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == 0 {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := d.facts.PinFact(r.Context(), req.ID, req.Pinned); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.auditMemoryAction(r.Context(), "fact.pin", strconv.FormatInt(req.ID, 10), "success", "")
	writeJSON(w, http.StatusOK, map[string]interface{}{"pinned": req.Pinned})
}

func (d *Dashboard) handleFactImportance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !d.requireFacts(w) {
		return
	}
	var req struct {
		ID         int64 `json:"id"`
		Importance int   `json:"importance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == 0 {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := d.facts.SetFactImportance(r.Context(), req.ID, req.Importance); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	d.auditMemoryAction(r.Context(), "fact.importance", strconv.FormatInt(req.ID, 10), "success", "")
	writeJSON(w, http.StatusOK, map[string]interface{}{"importance": req.Importance})
}

func (d *Dashboard) handleConflictReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !d.requireFacts(w) {
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == 0 {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := d.facts.MarkConflictReviewed(r.Context(), req.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"reviewed": true})
}
