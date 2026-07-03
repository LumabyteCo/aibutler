package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/LumabyteCo/aibutler/internal/skillsynth"
)

// SetProposals wires the approvals surface: self-authored skill proposals
// (and future proposal kinds) listed, inspected, and decided from the panel.
// Decisions route through the same synthesizer logic the CLI uses — one
// approval path, whatever the surface.
func (d *Dashboard) SetProposals(s *skillsynth.Synthesizer) {
	d.proposals = s
}

func (d *Dashboard) registerProposalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/dashboard/proposals", d.handleProposalsList)
	mux.HandleFunc("/api/dashboard/proposals/approve", d.handleProposalApprove)
	mux.HandleFunc("/api/dashboard/proposals/reject", d.handleProposalReject)
}

func (d *Dashboard) requireProposals(w http.ResponseWriter) bool {
	if d.proposals == nil {
		writeError(w, http.StatusServiceUnavailable, "proposals not configured")
		return false
	}
	return true
}

func (d *Dashboard) handleProposalsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !d.requireProposals(w) {
		return
	}
	pending, err := d.proposals.Pending(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"proposals": pending})
}

func (d *Dashboard) handleProposalApprove(w http.ResponseWriter, r *http.Request) {
	if !requireJSONPost(w, r) || !d.requireProposals(w) {
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == 0 {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	path, err := d.proposals.Approve(r.Context(), req.ID, "dashboard", 0)
	if err != nil {
		d.auditMemoryAction(r.Context(), "proposal.approve", "skill.propose", strconv.FormatInt(req.ID, 10), "error", err.Error())
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	d.auditMemoryAction(r.Context(), "proposal.approve", "skill.propose", strconv.FormatInt(req.ID, 10), "success", "")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"activated": path,
		"note":      "skills load at startup — restart the daemon to use it",
	})
}

func (d *Dashboard) handleProposalReject(w http.ResponseWriter, r *http.Request) {
	if !requireJSONPost(w, r) || !d.requireProposals(w) {
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == 0 {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := d.proposals.Reject(r.Context(), req.ID, "dashboard"); err != nil {
		d.auditMemoryAction(r.Context(), "proposal.reject", "skill.propose", strconv.FormatInt(req.ID, 10), "error", err.Error())
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	d.auditMemoryAction(r.Context(), "proposal.reject", "skill.propose", strconv.FormatInt(req.ID, 10), "success", "")
	writeJSON(w, http.StatusOK, map[string]interface{}{"rejected": true})
}
