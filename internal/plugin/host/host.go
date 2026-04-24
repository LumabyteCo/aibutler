package host

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolCaller invokes a tool by name with JSON input.
type ToolCaller interface {
	CallTool(ctx context.Context, name, input string) (string, error)
}

// VaultGetter retrieves a credential value by key.
type VaultGetter interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

// ConfigGetter reads plugin configuration values.
type ConfigGetter interface {
	Get(key string) (string, bool)
}

// Logger writes plugin log messages.
type Logger interface {
	Log(pluginName string, level string, message string)
}

// AuditWriter records plugin actions to the audit log.
type AuditWriter interface {
	WriteAudit(ctx context.Context, pluginID int64, action, capability, status string) error
}

// Deps holds dependencies for host functions.
type Deps struct {
	ToolCaller ToolCaller
	Vault      VaultGetter
	Config     ConfigGetter
	Logger     Logger
	Auditor    AuditWriter
	Caps       []string // Approved capabilities from manifest
	PluginID   int64
	PluginName string
}

// hasCap returns true if the capability list includes the given cap.
func (d *Deps) hasCap(required string) bool {
	for _, c := range d.Caps {
		if c == required {
			return true
		}
		// Support prefix matching: "credential.read" matches "credential.read:key".
		if strings.HasPrefix(required, c+":") {
			return true
		}
	}
	return false
}

// ---- Host function request/response types ----

// ToolCallRequest is the input for aibutler_tool_call.
type ToolCallRequest struct {
	Tool string `json:"tool"`
	Args string `json:"args"`
}

// ToolCallResponse is the output for aibutler_tool_call.
type ToolCallResponse struct {
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// LogRequest is the input for aibutler_log.
type LogRequest struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// ConfigGetRequest is the input for aibutler_config_get.
type ConfigGetRequest struct {
	Key string `json:"key"`
}

// ConfigGetResponse is the output for aibutler_config_get.
type ConfigGetResponse struct {
	Value string `json:"value,omitempty"`
	Found bool   `json:"found"`
}

// CredentialGetRequest is the input for aibutler_credential_get.
type CredentialGetRequest struct {
	Key string `json:"key"`
}

// CredentialGetResponse is the output for aibutler_credential_get.
type CredentialGetResponse struct {
	Value string `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

// ExecuteToolCall handles the aibutler_tool_call host function.
func ExecuteToolCall(ctx context.Context, deps *Deps, input []byte) ([]byte, error) {
	var req ToolCallRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalResponse(ToolCallResponse{Error: "invalid input: " + err.Error()})
	}

	if !deps.hasCap("tool.call") {
		if deps.Auditor != nil {
			_ = deps.Auditor.WriteAudit(ctx, deps.PluginID, "tool_call", "tool.call", "denied")
		}
		return marshalResponse(ToolCallResponse{Error: "capability denied: tool.call not granted"})
	}

	if deps.Auditor != nil {
		_ = deps.Auditor.WriteAudit(ctx, deps.PluginID, "tool_call", "tool.call", "success")
	}

	if deps.ToolCaller == nil {
		return marshalResponse(ToolCallResponse{Error: "no tool executor configured"})
	}

	result, err := deps.ToolCaller.CallTool(ctx, req.Tool, req.Args)
	if err != nil {
		return marshalResponse(ToolCallResponse{Error: fmt.Sprintf("tool error: %v", err)})
	}
	return marshalResponse(ToolCallResponse{Result: result})
}

// ExecuteLog handles the aibutler_log host function.
// Logging is always allowed (no capability gate).
func ExecuteLog(ctx context.Context, deps *Deps, input []byte) ([]byte, error) {
	var req LogRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return []byte(`{"error":"invalid input"}`), nil
	}

	if deps.Logger != nil {
		deps.Logger.Log(deps.PluginName, req.Level, req.Message)
	}
	return []byte(`{"ok":true}`), nil
}

// ExecuteConfigGet handles the aibutler_config_get host function.
// Config reading is always allowed (no capability gate).
func ExecuteConfigGet(ctx context.Context, deps *Deps, input []byte) ([]byte, error) {
	var req ConfigGetRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalResponse(ConfigGetResponse{Found: false})
	}

	if deps.Config == nil {
		return marshalResponse(ConfigGetResponse{Found: false})
	}

	val, ok := deps.Config.Get(req.Key)
	return marshalResponse(ConfigGetResponse{Value: val, Found: ok})
}

// ExecuteCredentialGet handles the aibutler_credential_get host function.
func ExecuteCredentialGet(ctx context.Context, deps *Deps, input []byte) ([]byte, error) {
	var req CredentialGetRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalResponse(CredentialGetResponse{Error: "invalid input"})
	}

	if req.Key == "" {
		return marshalResponse(CredentialGetResponse{Error: "credential key must not be empty"})
	}

	// Check for specific key capability: credential.read:<key>.
	requiredCap := "credential.read:" + req.Key
	if !deps.hasCap(requiredCap) && !deps.hasCap("credential.read") {
		if deps.Auditor != nil {
			_ = deps.Auditor.WriteAudit(ctx, deps.PluginID, "credential_get", requiredCap, "denied")
		}
		return marshalResponse(CredentialGetResponse{Error: fmt.Sprintf("capability denied: %s not granted", requiredCap)})
	}

	if deps.Auditor != nil {
		_ = deps.Auditor.WriteAudit(ctx, deps.PluginID, "credential_get", requiredCap, "success")
	}

	if deps.Vault == nil {
		return marshalResponse(CredentialGetResponse{Error: "no vault configured"})
	}

	val, err := deps.Vault.Get(ctx, req.Key)
	if err != nil {
		return marshalResponse(CredentialGetResponse{Error: fmt.Sprintf("vault error: %v", err)})
	}
	return marshalResponse(CredentialGetResponse{Value: string(val)})
}

func marshalResponse(v interface{}) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"error":"marshal error"}`), nil
	}
	return data, nil
}
