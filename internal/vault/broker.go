package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/capability"
)

// RequestPolicy controls how a credential request is decided.
type RequestPolicy string

const (
	// PolicyAuto — credential is in the auto-approval list and is granted
	// without further interaction.
	PolicyAuto RequestPolicy = "auto"
	// PolicyDeny — credential is in the deny list, or default-deny applies.
	PolicyDeny RequestPolicy = "deny"
	// PolicyPrompt — credential requires interactive user confirmation.
	// Reserved for a future channel-routed prompt flow; not yet implemented.
	// Requests that fall into this policy currently return a denial with a
	// clear "add to vault.auto_approved_keys to permit" message.
	PolicyPrompt RequestPolicy = "prompt"
)

// BrokerRules describes how the broker decides each request.
type BrokerRules struct {
	// AutoApprovedKeys are credential keys that are granted without
	// confirmation. Comparison is case-insensitive exact-match.
	AutoApprovedKeys []string
	// DeniedKeys are keys that are NEVER granted, even if listed in
	// AutoApprovedKeys (deny wins).
	DeniedKeys []string
}

// RequestResult describes the broker's decision for a credential request.
type RequestResult struct {
	Granted bool          `json:"granted"`
	Policy  RequestPolicy `json:"policy"`
	Reason  string        `json:"reason"`
	// Value is populated only when Granted=true. Callers must treat the
	// bytes as sensitive — do not echo back to the agent's conversation,
	// log, or persist beyond what's strictly required for the action.
	Value []byte `json:"-"`
}

// Auditor is the narrow interface the broker uses for audit logging — same
// shape as capability.Auditor, declared here to avoid forcing every caller
// to import capability.
type Auditor interface {
	LogAccess(ctx context.Context, entry capability.AuditEntry) error
}

// Broker decides whether to issue credentials to agents on demand. It
// wraps a Vault for actual storage lookup and an Auditor for traceability.
//
// Every Request — granted or denied — produces an audit entry containing
// the credential key, agent ID, status, and reason. The agent-supplied
// purpose string is captured separately by the tool dispatcher's
// compliance log (which records call.Input verbatim) so reviewers can
// see why a credential was requested.
type Broker struct {
	vault   Vault
	rules   BrokerRules
	auditor Auditor
}

// NewBroker creates a Broker.
//
// The rules slices are not copied — callers should not mutate them after
// construction. Pass nil for auditor to skip audit logging (testing only).
func NewBroker(v Vault, rules BrokerRules, auditor Auditor) *Broker {
	return &Broker{
		vault:   v,
		rules:   rules,
		auditor: auditor,
	}
}

// Request decides whether to grant the named credential to the requesting
// agent. The purpose string is recorded for after-the-fact review.
//
// Decision order:
//
//  1. If key is in DeniedKeys → deny.
//  2. If key is in AutoApprovedKeys → grant (look up value; if not found
//     in the vault, deny with "credential not stored").
//  3. Otherwise → deny with "needs explicit user confirmation" guidance
//     (PolicyPrompt — interactive flow not yet shipped).
//
// agentID is recorded in the audit entry so credential issuance is
// traceable to a specific agent execution.
func (b *Broker) Request(ctx context.Context, key, purpose, agentID string) RequestResult {
	res := RequestResult{}

	if strings.TrimSpace(key) == "" {
		res.Policy = PolicyDeny
		res.Reason = "credential key is required"
		b.audit(ctx, agentID, key, "denied", res.Reason)
		return res
	}

	// Deny list always wins.
	if matchKey(b.rules.DeniedKeys, key) {
		res.Policy = PolicyDeny
		res.Reason = fmt.Sprintf("key %q is on the deny list", key)
		b.audit(ctx, agentID, key, "denied", res.Reason)
		return res
	}

	// Auto-approval.
	if matchKey(b.rules.AutoApprovedKeys, key) {
		cred, err := b.vault.Get(ctx, key)
		if err != nil {
			res.Policy = PolicyDeny
			res.Reason = fmt.Sprintf("credential %q not stored in vault: %v", key, err)
			b.audit(ctx, agentID, key, "denied", res.Reason)
			return res
		}
		res.Granted = true
		res.Policy = PolicyAuto
		res.Reason = fmt.Sprintf("key %q is auto-approved", key)
		res.Value = cred.Value
		b.audit(ctx, agentID, key, "granted", "auto-approved")
		return res
	}

	// Default-deny — neither approved nor explicitly denied.
	res.Policy = PolicyPrompt
	res.Reason = fmt.Sprintf(
		"key %q requires explicit user approval. Interactive confirmation is not yet implemented — "+
			"add %q to Configurations.Vault.AutoApprovedKeys in your config to permit this request.",
		key, key,
	)
	b.audit(ctx, agentID, key, "denied", "needs user approval (interactive prompt not implemented)")
	return res
}

// matchKey returns true if key matches any entry in the list (case-insensitive).
func matchKey(list []string, key string) bool {
	for _, entry := range list {
		if strings.EqualFold(entry, key) {
			return true
		}
	}
	return false
}

func (b *Broker) audit(ctx context.Context, agentID, key, status, reason string) {
	if b.auditor == nil {
		return
	}
	_ = b.auditor.LogAccess(ctx, capability.AuditEntry{
		AgentID:        agentID,
		Action:         "vault.request",
		CapabilityUsed: "tool.vault.request",
		CredentialKey:  key,
		Status:         status,
		Error: func() string {
			if status == "denied" {
				return reason
			}
			return ""
		}(),
	})
}

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// brokerToolResponse is the JSON shape returned to the agent. Note that
// the credential value IS included here — the agent needs it to act —
// but the tool description warns the agent to treat it as sensitive.
type brokerToolResponse struct {
	Granted bool          `json:"granted"`
	Policy  RequestPolicy `json:"policy"`
	Reason  string        `json:"reason"`
	Value   string        `json:"value,omitempty"`
}

// RegisterRequestTool registers vault.request.
func RegisterRequestTool(registry toolRegistry, broker *Broker, agentIDFn func(ctx context.Context) string) {
	registry.Register(
		"vault.request",
		"Request a stored credential by key with a stated purpose. Returns granted=true and the value when "+
			"the key is on the auto-approval list, or granted=false with guidance otherwise. The returned value "+
			"is sensitive — do not echo it back in normal output, do not include it in messages to the user, and "+
			"use it only for the immediate action you described in 'purpose'. Every request is audited.",
		`{"type":"object","properties":{`+
			`"key":{"type":"string","description":"Credential key as stored in the vault (e.g. github_token, openai_api_key)"},`+
			`"purpose":{"type":"string","description":"One-line justification for needing this credential — recorded in the audit log"}`+
			`},"required":["key","purpose"]}`,
		"tool.vault.request",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Key     string `json:"key"`
				Purpose string `json:"purpose"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("vault.request: invalid input: %w", err)
			}
			if strings.TrimSpace(args.Key) == "" {
				return "", fmt.Errorf("vault.request: key is required")
			}
			if strings.TrimSpace(args.Purpose) == "" {
				return "", fmt.Errorf("vault.request: purpose is required (used for audit trail)")
			}

			agentID := ""
			if agentIDFn != nil {
				agentID = agentIDFn(ctx)
			}
			result := broker.Request(ctx, args.Key, args.Purpose, agentID)

			resp := brokerToolResponse{
				Granted: result.Granted,
				Policy:  result.Policy,
				Reason:  result.Reason,
			}
			if result.Granted {
				resp.Value = string(result.Value)
			}
			out, err := json.Marshal(resp)
			if err != nil {
				return "", fmt.Errorf("vault.request: marshal response: %w", err)
			}
			return string(out), nil
		},
	)
}
