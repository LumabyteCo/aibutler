package mcp

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// JSON-RPC 2.0 message types per MCP specification.

// Protocol versions this client understands, newest first. The client offers
// PreferredProtocolVersion at initialize; per the MCP lifecycle the server may
// answer with any version it supports, and the client must either accept it or
// disconnect. Anything in this list is accepted.
const (
	ProtocolVersion20250618 = "2025-06-18" // adds elicitation, structured output
	ProtocolVersion20250326 = "2025-03-26"
	ProtocolVersion20241105 = "2024-11-05"

	// PreferredProtocolVersion is what we offer first. Elicitation and
	// structured tool output are only available from 2025-03-26 / 2025-06-18,
	// so offering the newest unlocks them where the server supports it.
	PreferredProtocolVersion = ProtocolVersion20250618
)

// SupportedProtocolVersions lists every version this client can speak.
var SupportedProtocolVersions = []string{
	ProtocolVersion20250618,
	ProtocolVersion20250326,
	ProtocolVersion20241105,
}

// IsSupportedProtocolVersion reports whether the client can speak version v.
func IsSupportedProtocolVersion(v string) bool {
	for _, s := range SupportedProtocolVersions {
		if s == v {
			return true
		}
	}
	return false
}

// JSONRPCRequest is a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCNotification is a JSON-RPC 2.0 notification: a message with a method
// but deliberately no id, so the peer must not answer it. The spec requires the
// id to be absent (not null/zero), which is why this is a distinct type from
// JSONRPCRequest rather than a request with an omitempty id.
type JSONRPCNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError is a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *JSONRPCError) Error() string { return e.Message }

// Standard JSON-RPC error codes used when answering server-initiated requests.
const (
	ErrCodeMethodNotFound = -32601
	ErrCodeInternalError  = -32603
)

// rpcEnvelope is the permissive shape used by the transport read loop to
// classify an inbound line before dispatching it. A single stdout stream
// carries three kinds of message and they are told apart by which fields are
// present:
//
//	method + id  → a server→client request (e.g. elicitation/create) we must answer
//	method, no id → a notification (e.g. notifications/progress) we must not answer
//	id, no method → a response to one of our requests, routed by matching id
//
// ID is deliberately json.RawMessage rather than a numeric type. JSON-RPC 2.0
// permits string ids and the SERVER chooses the id for server→client requests,
// so a typed field would fail to decode the whole message — dropping the line
// and hanging the server's request — the moment a peer used a string id.
type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *JSONRPCError   `json:"error"`
}

// hasID reports whether the message carries a JSON-RPC id. Absent or null both
// mean "no id" (i.e. a notification).
func (e *rpcEnvelope) hasID() bool {
	id := bytes.TrimSpace(e.ID)
	return len(id) > 0 && !bytes.Equal(id, []byte("null"))
}

// intID returns the id as an int, and whether it was present and numeric.
//
// Only responses to OUR requests need this, and those are always numeric
// because the client only ever sends int ids. A server-chosen string id
// correctly reports false here; such a message is never one of our pending
// responses, and requests echo the raw id instead.
func (e *rpcEnvelope) intID() (int, bool) {
	if !e.hasID() {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(e.ID, &n); err != nil {
		return 0, false
	}
	return n, true
}

// rawResponse is a JSON-RPC response whose id is echoed verbatim. Replies to
// server→client requests must carry back the exact id bytes received — a
// string id must not come back as a number.
type rawResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// MCP-specific types.

// InitializeParams is sent during the MCP initialize handshake.
type InitializeParams struct {
	ProtocolVersion string              `json:"protocolVersion"`
	ClientInfo      ClientInfo          `json:"clientInfo"`
	Capabilities    *ClientCapabilities `json:"capabilities,omitempty"`
}

// ClientInfo identifies the MCP client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClientCapabilities declares what this client can do. Servers gate features on
// these: a server may only send elicitation/create if Elicitation is non-nil,
// so anything left nil is a feature we opt out of.
type ClientCapabilities struct {
	Elicitation  *EmptyCapability           `json:"elicitation,omitempty"`
	Roots        *RootsCapability           `json:"roots,omitempty"`
	Sampling     *EmptyCapability           `json:"sampling,omitempty"`
	Experimental map[string]json.RawMessage `json:"experimental,omitempty"`
}

// EmptyCapability is a capability with no options: it serializes to {}.
type EmptyCapability struct{}

// RootsCapability declares filesystem-roots support.
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ServerCapabilities is what the server declares it can do in the initialize result.
type ServerCapabilities struct {
	Tools       *ListChangedCapability `json:"tools,omitempty"`
	Resources   *ResourcesCapability   `json:"resources,omitempty"`
	Prompts     *ListChangedCapability `json:"prompts,omitempty"`
	Completions *EmptyCapability       `json:"completions,omitempty"`
	Logging     *EmptyCapability       `json:"logging,omitempty"`
}

// ListChangedCapability is a capability that can emit list_changed notifications.
type ListChangedCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability declares resource support.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// InitializeResult is the server's response to initialize.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	Instructions    string             `json:"instructions,omitempty"`
}

// ServerInfo identifies the MCP server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolInfo describes a tool exposed by an MCP server.
type ToolInfo struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	Annotations  json.RawMessage `json:"annotations,omitempty"`
}

// ToolListResult is the response to tools/list.
type ToolListResult struct {
	Tools []ToolInfo `json:"tools"`
}

// RequestMeta carries per-request MCP metadata. A progressToken opts the call
// into notifications/progress: servers only stream progress when one is sent.
type RequestMeta struct {
	ProgressToken interface{} `json:"progressToken,omitempty"`
}

// ToolCallParams is the request payload for tools/call.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Meta      *RequestMeta    `json:"_meta,omitempty"`
}

// ToolCallResult is the response from tools/call.
type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
	// StructuredContent is the typed payload servers return alongside the
	// human-readable content blocks (MCP structured output). Servers that
	// declare an outputSchema may return this with little or no text, so it is
	// the fallback that keeps such results from surfacing as an empty string.
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

// ContentBlock is a piece of content in a tool call result.
type ContentBlock struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	Data     string            `json:"data,omitempty"`     // base64 for image/audio
	MimeType string            `json:"mimeType,omitempty"` // for image/audio/resource_link
	URI      string            `json:"uri,omitempty"`      // for resource_link
	Name     string            `json:"name,omitempty"`     // for resource_link
	Resource *EmbeddedResource `json:"resource,omitempty"` // for type "resource"
}

// EmbeddedResource is a resource inlined into a content block or returned by
// resources/read.
type EmbeddedResource struct {
	URI      string `json:"uri,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"` // base64
}

// TextContent returns the concatenated text of all text content blocks.
//
// It is deliberately strict: only type=="text" blocks with non-empty text are
// included. Use AgentText for the richer rendering that also covers embedded
// resources, binary placeholders and structured content.
func (r *ToolCallResult) TextContent() string {
	var parts []string
	for _, c := range r.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += "\n" + p
	}
	return result
}

// AgentText renders a tool result into the string handed to the model.
//
// Unlike TextContent it never silently discards a payload: embedded resource
// text is included, binary blocks become a typed placeholder so the model is
// told something arrived, and a result carrying only structuredContent falls
// back to that JSON instead of returning "".
func (r *ToolCallResult) AgentText() string {
	var parts []string
	for _, c := range r.Content {
		switch c.Type {
		case "text":
			if c.Text != "" {
				parts = append(parts, c.Text)
			}
		case "resource":
			if c.Resource == nil {
				continue
			}
			switch {
			case c.Resource.Text != "":
				parts = append(parts, c.Resource.Text)
			case c.Resource.Blob != "":
				parts = append(parts, "["+describeBinary("resource", c.Resource.MimeType, len(c.Resource.Blob))+" "+c.Resource.URI+"]")
			}
		case "resource_link":
			label := c.Name
			if label == "" {
				label = c.URI
			}
			parts = append(parts, "[resource_link "+label+" "+c.URI+"]")
		case "image", "audio":
			parts = append(parts, "["+describeBinary(c.Type, c.MimeType, len(c.Data))+"]")
		}
	}

	if len(parts) == 0 {
		// No renderable content blocks. A structured-output server may legally
		// return structuredContent with an empty content array; surface it
		// rather than handing the model an empty string.
		if len(r.StructuredContent) > 0 {
			return string(r.StructuredContent)
		}
		return ""
	}

	out := parts[0]
	for _, p := range parts[1:] {
		out += "\n" + p
	}
	return out
}

// describeBinary builds a short placeholder like `image image/png, 1234 b64 bytes`.
func describeBinary(kind, mime string, n int) string {
	if mime == "" {
		mime = "application/octet-stream"
	}
	return kind + " " + mime + ", " + strconv.Itoa(n) + " b64 bytes"
}

// --- Elicitation (MCP 2025-03-26+) ---

// ElicitRequestParams is the payload of a server→client elicitation/create
// request: a prompt plus a flat JSON Schema of primitive fields to fill in.
type ElicitRequestParams struct {
	Message         string          `json:"message"`
	RequestedSchema json.RawMessage `json:"requestedSchema"`
}

// Elicitation actions per the MCP spec.
const (
	ElicitActionAccept  = "accept"
	ElicitActionDecline = "decline"
	ElicitActionCancel  = "cancel"
)

// ElicitResult is the client's answer to elicitation/create.
type ElicitResult struct {
	Action  string                 `json:"action"`
	Content map[string]interface{} `json:"content,omitempty"`
}

// ElicitationHandler answers a server's elicitation/create request.
type ElicitationHandler func(serverName string, params ElicitRequestParams) ElicitResult

// elicitSchema is the flat primitive-field schema elicitation forms use.
type elicitSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]elicitField `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

type elicitField struct {
	Type    string      `json:"type"`
	Title   string      `json:"title,omitempty"`
	Enum    []string    `json:"enum,omitempty"`
	Default interface{} `json:"default,omitempty"`
}

// DeclineElicitation is the safe default handler: it answers every elicitation
// with "decline". Spec-compliant and side-effect free — the server gets a real
// answer and continues instead of hanging, but the client never puts words in
// the user's mouth.
func DeclineElicitation(_ string, _ ElicitRequestParams) ElicitResult {
	return ElicitResult{Action: ElicitActionDecline}
}

// AcceptElicitationDefaults answers by accepting the form pre-filled with the
// schema's own default values (falling back to the first enum option). MCP
// elicitation forms put the suggested answer in `default` precisely so a client
// can accept it in one step; fields with neither a default nor options are left
// out, which well-formed schemas allow because such fields are not required.
//
// If the schema is unparseable or yields no answers at all, it declines rather
// than sending an empty accept.
func AcceptElicitationDefaults(_ string, params ElicitRequestParams) ElicitResult {
	var schema elicitSchema
	if err := json.Unmarshal(params.RequestedSchema, &schema); err != nil {
		return ElicitResult{Action: ElicitActionDecline}
	}

	content := make(map[string]interface{}, len(schema.Properties))
	for key, field := range schema.Properties {
		switch {
		case field.Default != nil:
			content[key] = field.Default
		case len(field.Enum) > 0:
			content[key] = field.Enum[0]
		}
	}
	if len(content) == 0 {
		return ElicitResult{Action: ElicitActionDecline}
	}
	// Never accept a form we cannot fill completely: a required field with no
	// default and no options has no defensible answer, and an accept missing it
	// is worse than a decline the server already knows how to handle.
	for _, key := range schema.Required {
		if _, answered := content[key]; !answered {
			return ElicitResult{Action: ElicitActionDecline}
		}
	}
	return ElicitResult{Action: ElicitActionAccept, Content: content}
}

// --- Progress (MCP 2025-03-26+) ---

// ProgressParams is the payload of a notifications/progress notification.
type ProgressParams struct {
	ProgressToken interface{} `json:"progressToken"`
	Progress      float64     `json:"progress"`
	Total         float64     `json:"total,omitempty"`
	Message       string      `json:"message,omitempty"`
}

// ProgressHandler receives progress updates for an in-flight call.
type ProgressHandler func(serverName string, p ProgressParams)

// --- Resources ---

// ResourceInfo describes a resource exposed by a server.
type ResourceInfo struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceListResult is the response to resources/list.
type ResourceListResult struct {
	Resources []ResourceInfo `json:"resources"`
}

// ResourceReadParams is the request payload for resources/read.
type ResourceReadParams struct {
	URI string `json:"uri"`
}

// ResourceReadResult is the response to resources/read.
type ResourceReadResult struct {
	Contents []EmbeddedResource `json:"contents"`
}
