package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// CostSettings holds user-facing cost preferences.
type CostSettings struct {
	Strategy      string  `yaml:"strategy"`       // frugal | balanced | quality
	MonthlyBudget float64 `yaml:"monthly_budget"` // USD
}

// Settings — user preferences (everyone sees these).
type Settings struct {
	Language        string       `yaml:"language"`
	Timezone        string       `yaml:"timezone"`
	Notifications   bool         `yaml:"notifications"`
	MorningBriefing string       `yaml:"morning_briefing"`
	ActiveChannels  []string     `yaml:"active_channels"`
	Model           string       `yaml:"model"`
	PersonaName     string       `yaml:"persona_name"`
	Skills          []string     `yaml:"skills"`
	AgentsEnabled    bool         `yaml:"agents_enabled"`
	AgentMode        string       `yaml:"agent_mode"`
	Cost             CostSettings `yaml:"cost"`
	OfflineMode      bool         `yaml:"offline_mode"`
	TelemetryEnabled bool         `yaml:"telemetry_enabled"`
}

// ModelConfig holds model provider configuration.
type ModelConfig struct {
	Primary  string `yaml:"primary"`
	Fallback string `yaml:"fallback"`
	// BaseURL overrides the OpenAI-compatible endpoint URL for local/cloud
	// providers (Ollama, LM Studio, vLLM, Ollama Cloud, Groq, DeepSeek, etc.).
	// Ignored for native Anthropic / OpenAI / Gemini / xAI adapters.
	// Example: "https://ollama.com/v1/chat/completions" for Ollama Cloud.
	// Defaults to http://localhost:11434/v1/chat/completions.
	BaseURL  string `yaml:"base_url"`
}

// ChannelConfig holds per-channel configuration.
type ChannelConfig struct {
	Enabled          bool   `yaml:"enabled"`
	TypingIndicators bool   `yaml:"typing_indicators"`
	VoiceResponse    string `yaml:"voice_response"` // text | voice | auto | both
}

// AgentConfig holds agent orchestration settings.
type AgentConfig struct {
	MaxConcurrent   int              `yaml:"max_concurrent"`
	MaxDepth        int              `yaml:"max_depth"`
	DefaultSubModel string           `yaml:"default_subagent_model"`
	CustomRoles     []CustomRoleSpec `yaml:"custom_roles"`
	Routing         string           `yaml:"routing"` // classify | explicit | round-robin (for ModeCustom)
}

// CustomRoleSpec defines a user-configured custom agent role.
type CustomRoleSpec struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Model       string   `yaml:"model"`        // Model override (empty = use primary)
	Tools       []string `yaml:"tools"`         // Allowed tool names (empty = all)
	Prompt      string   `yaml:"system_prompt"` // Additional system instructions
}

// ShellConfig holds shell execution security settings.
type ShellConfig struct {
	Mode    string   `yaml:"mode"`    // allowlist | denylist
	Allowed []string `yaml:"allowed"`
	// UseDefaultAllowlist, when true, prepends the curated DefaultAllowlist
	// from each native-script executor (applescript, shortcuts, dbus) to the
	// user-provided Allowed list. Defaults to false so a fresh install with
	// an empty Allowed list still denies everything (secure-by-default).
	UseDefaultAllowlist bool `yaml:"use_default_allowlist"`
}

// SecurityConfig holds security-related settings.
type SecurityConfig struct {
	Shell ShellConfig `yaml:"shell"`
}

// VaultRequestConfig controls the credential broker (vault.request tool).
//
// AutoApprovedKeys are credential keys that the broker grants to agents
// without further interaction. DeniedKeys are never granted (deny wins
// over auto-approval). Keys not in either list default to denied with a
// guidance message — interactive user-confirmation is reserved for a
// future release.
type VaultRequestConfig struct {
	AutoApprovedKeys []string `yaml:"auto_approved_keys"`
	DeniedKeys       []string `yaml:"denied_keys"`
}

// VaultConfig holds vault-related settings beyond storage backend choice
// (which lives in the higher-level Settings tier).
type VaultConfig struct {
	Request VaultRequestConfig `yaml:"request"`
}

// CostConfig holds advanced cost settings.
type CostConfig struct {
	Alerts          []int  `yaml:"alerts"`
	AlertChannel    string `yaml:"alert_channel"`
	OnBudgetReached string `yaml:"on_budget_reached"` // warn | pause
}

// PromptConfig holds prompt system paths.
type PromptConfig struct {
	PersonaFile string `yaml:"persona_file"`
	SkillsDir   string `yaml:"skills_dir"`
}

// WebConfig holds WebChat adapter settings.
type WebConfig struct {
	Port          int    `yaml:"port"`
	BindAddress   string `yaml:"bind_address"`
	MaxUploadSize int64  `yaml:"max_upload_size_mb"` // MB
}

// MCPServerConfig describes a single MCP server connection.
type MCPServerConfig struct {
	Name     string            `yaml:"name"`
	Command  string            `yaml:"command"`
	Args     []string          `yaml:"args"`
	Env      map[string]string `yaml:"env"`
	VaultEnv map[string]string `yaml:"vault_env"` // vault_key → env_var_name
}

// MCPConfig holds MCP client settings.
type MCPConfig struct {
	Servers []MCPServerConfig `yaml:"servers"`
}

// ScheduleConfig holds schedule system settings.
type ScheduleConfig struct {
	Enabled bool `yaml:"enabled"`
}

// IoTConfig holds IoT adapter settings.
type IoTConfig struct {
	Adapter string `yaml:"adapter"` // "stub" (default), "homeassistant"
}

// EmbeddingConfig holds embedding provider settings for vector search.
type EmbeddingConfig struct {
	Provider string `yaml:"provider"` // "openai", "ollama", "openai_compat", "" (auto-detect)
	Model    string `yaml:"model"`    // e.g., "text-embedding-3-small", "nomic-embed-text:v1.5"
	BaseURL  string `yaml:"base_url"` // for ollama/compat, e.g., "http://localhost:11434"
}

// VoiceConfig holds voice pipeline settings.
type VoiceConfig struct {
	STTProvider string `yaml:"stt_provider"` // "whisper" (default)
	TTSProvider string `yaml:"tts_provider"` // "stub" (default)
}

// TypingOptions holds typing indicator tuning.
type TypingOptions struct {
	IntervalMs int `yaml:"interval_ms"` // default: 3000
	TimeoutMs  int `yaml:"timeout_ms"`  // default: 120000
}

// MediaOptions holds media processing tuning.
type MediaOptions struct {
	MaxUploadSizeMB int `yaml:"max_upload_size_mb"` // default: 20
	MaxTextLines    int `yaml:"max_text_lines"`     // default: 500
}

// PluginConfig holds plugin system configuration.
type PluginConfig struct {
	AutoEnable bool   `yaml:"auto_enable"` // Auto-enable plugins on install
	PluginDir  string `yaml:"plugin_dir"`  // Override plugin directory
}

// A2AConfig holds agent-to-agent protocol settings.
type A2AConfig struct {
	Enabled     bool     `yaml:"enabled"`
	TokenHashes []string `yaml:"token_hashes"` // SHA-256 hashes of allowed bearer tokens
	Port        int      `yaml:"port"`          // default: 8081
	BindAddress string   `yaml:"bind_address"`  // default: "127.0.0.1" (localhost only for security)
}

// MCPServerExposure holds MCP server exposure settings.
type MCPServerExposure struct {
	AllowedCapabilities []string `yaml:"allowed_capabilities"` // default: ["memory.read", "data.read"]
}

// OAuthProviderConfig holds OAuth client credentials for one provider.
type OAuthProviderConfig struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

// OAuthConfig holds OAuth provider settings.
type OAuthConfig struct {
	Gmail          OAuthProviderConfig `yaml:"gmail"`
	GoogleCalendar OAuthProviderConfig `yaml:"google_calendar"`
	GitHub         OAuthProviderConfig `yaml:"github"`
	RedirectURI    string              `yaml:"redirect_uri"`
}

// BridgeConfig describes a single subprocess bridge.
type BridgeConfig struct {
	Command      string        `yaml:"command"`
	Args         []string      `yaml:"args"`
	Timeout      time.Duration `yaml:"timeout"`
	Capabilities []string      `yaml:"capabilities"`
}

// BridgesConfig holds all subprocess bridge definitions.
type BridgesConfig struct {
	Bridges map[string]BridgeConfig `yaml:"bridges"`
}

// SandboxConfig holds shell sandbox settings.
type SandboxConfig struct {
	Mode       string   `yaml:"mode"`        // off | workspace-only | allow-list
	AllowPaths []string `yaml:"allow_paths"`
}

// RemoteBackupConfig holds remote backup destination settings.
type RemoteBackupConfig struct {
	Provider   string `yaml:"provider"`    // s3 | http
	Endpoint   string `yaml:"endpoint"`    // S3 endpoint or HTTP PUT URL
	Bucket     string `yaml:"bucket"`
	AccessKey  string `yaml:"access_key"`
	SecretKey  string `yaml:"secret_key"`
	Region     string `yaml:"region"`
	EncryptKey string `yaml:"encrypt_key"` // optional age encryption key
}

// BackupConfig holds all backup settings.
type BackupConfig struct {
	Remote RemoteBackupConfig `yaml:"remote"`
}

// SwarmConfig holds swarm orchestrator settings.
type SwarmConfig struct {
	Enabled           bool    `yaml:"enabled"`
	MaxDepth          int     `yaml:"max_depth"`
	BudgetUSD         float64 `yaml:"budget_usd"`
	WorkspaceTTLHours int     `yaml:"workspace_ttl_hours"`
}

// RegistryConfig holds dynamic agent registry settings.
type RegistryConfig struct {
	HealthTTLMinutes int  `yaml:"health_ttl_minutes"`
	SelfRegister     bool `yaml:"self_register"`
}

// HookEntry describes a single hook command with optional tool filters.
type HookEntry struct {
	Command string   `yaml:"command"` // shell command to execute
	Tools   []string `yaml:"tools"`   // tool name patterns (glob-like)
}

// HooksConfig holds pre/post tool-use hook definitions.
type HooksConfig struct {
	PreToolUse  []HookEntry `yaml:"pre_tool_use"`
	PostToolUse []HookEntry `yaml:"post_tool_use"`
}

// PermissionRuleConfig holds a single permission rule.
type PermissionRuleConfig struct {
	Pattern string `yaml:"pattern"` // e.g., "bash(git:*)"
	Action  string `yaml:"action"`  // "allow" or "deny"
}

// PermissionsConfig holds permission mode and rules.
type PermissionsConfig struct {
	Mode  string                 `yaml:"mode"` // read-only, workspace-write, full-access
	Rules []PermissionRuleConfig `yaml:"rules"`
}

// Configurations — system wiring (power users).
type Configurations struct {
	Models    ModelConfig                `yaml:"models"`
	Channels  map[string]*ChannelConfig  `yaml:"channels"`
	Agents    AgentConfig                `yaml:"agents"`
	Security  SecurityConfig             `yaml:"security"`
	Vault     VaultConfig                `yaml:"vault"`
	Cost      CostConfig                 `yaml:"cost"`
	Prompts   PromptConfig               `yaml:"prompts"`
	Web       WebConfig                  `yaml:"web"`
	MCP       MCPConfig                  `yaml:"mcp"`
	Schedule  ScheduleConfig             `yaml:"schedule"`
	IoT       IoTConfig                  `yaml:"iot"`
	Voice     VoiceConfig                `yaml:"voice"`
	Embedding EmbeddingConfig            `yaml:"embedding"`
	Plugins   PluginConfig               `yaml:"plugins"`
	A2A       A2AConfig                  `yaml:"a2a"`
	MCPServer MCPServerExposure           `yaml:"mcp_server"`
	OAuth       OAuthConfig       `yaml:"oauth"`
	Swarm       SwarmConfig       `yaml:"swarm"`
	Registry    RegistryConfig    `yaml:"registry"`
	Hooks       HooksConfig       `yaml:"hooks"`
	Permissions PermissionsConfig `yaml:"permissions"`
	Bridges     BridgesConfig     `yaml:"bridges"`
	Sandbox     SandboxConfig     `yaml:"sandbox"`
	Backup      BackupConfig      `yaml:"backup"`
	File        FileConfig        `yaml:"file"`
}

// FileConfig configures the file-manipulation tools. AllowPaths extends the
// built-in defaults (data dir + $HOME) with additional directories the agent
// may read/write. Paths are resolved after symlink expansion.
type FileConfig struct {
	AllowPaths []string `yaml:"allow_paths"`
}

// ModelOptions holds model tuning parameters.
type ModelOptions struct {
	MaxTokens      int           `yaml:"max_tokens"`
	Temperature    float64       `yaml:"temperature"`
	RequestTimeout time.Duration `yaml:"request_timeout"`
	RetryCount     int           `yaml:"retry_count"`
}

// DatabaseOptions holds database tuning parameters.
type DatabaseOptions struct {
	BusyTimeout int `yaml:"busy_timeout"`
}

// AgentOptions holds agent tuning parameters.
type AgentOptions struct {
	MaxToolCalls       int           `yaml:"max_tool_calls"`
	SubagentTimeout    time.Duration `yaml:"subagent_timeout"`
	BackgroundTimeout  time.Duration `yaml:"background_timeout"`
	BackgroundMax      int           `yaml:"background_max"`
	PerSubagentBudget  float64       `yaml:"per_subagent_budget"`  // Max USD per subagent (0 = unlimited)
	AutonomyLevel      string        `yaml:"autonomy_level"`       // l1 | l2 (default: l1)
	L2AutoActions      []string      `yaml:"l2_auto_actions"`      // Tools auto-approved at L2
	L2AskActions       []string      `yaml:"l2_ask_actions"`       // Tools requiring confirmation at L2
	ParallelToolLimit  int           `yaml:"parallel_tool_limit"`  // Max concurrent tool calls in multi mode (default: 5)
	PerUserAgentLimit  int           `yaml:"per_user_agent_limit"` // Max concurrent agents per user (default: 3)
	L3TimeBound        time.Duration `yaml:"l3_time_bound"`        // L3 autonomy time limit (default: 30m, max: 24h)
	L3SafetyActions    []string      `yaml:"l3_safety_actions"`    // Actions that always require confirmation even at L3
}

// PromptOptions holds prompt tuning parameters.
type PromptOptions struct {
	MaxTier1Tokens        int     `yaml:"max_tier1_tokens"`
	MaxSkillsPerTurn      int     `yaml:"max_skills_per_turn"`
	SkillTriggerThreshold float64 `yaml:"skill_trigger_threshold"`
	TruncationStrategy    string  `yaml:"truncation_strategy"`    // balanced | essential_only
	MaxInstructionTokens  int     `yaml:"max_instruction_tokens"` // default: 200
}

// CostOptions holds cost tracking tuning.
type CostOptions struct {
	CacheTTL           time.Duration `yaml:"cache_ttl"`
	ExpensiveThreshold int           `yaml:"expensive_threshold"` // tokens
}

// SessionOptions holds session lifecycle tuning parameters.
type SessionOptions struct {
	CleanupInterval time.Duration `yaml:"cleanup_interval"` // default: 1h
	MaxAge          time.Duration `yaml:"max_age"`          // default: 168h (7 days)
}

// ScheduleOptions holds schedule tuning parameters.
type ScheduleOptions struct {
	TickInterval  time.Duration `yaml:"tick_interval"`  // default: 60s
	MaxConcurrent int          `yaml:"max_concurrent"` // default: 3
}

// VoiceOptions holds voice pipeline tuning parameters.
type VoiceOptions struct {
	MaxAudioSizeMB int           `yaml:"max_audio_size_mb"` // default: 25
	STTTimeout     time.Duration `yaml:"stt_timeout"`       // default: 30s
}

// PluginOptions holds plugin system tuning.
type PluginOptions struct {
	MaxPlugins  int           `yaml:"max_plugins"`  // default: 20
	ExecTimeout time.Duration `yaml:"exec_timeout"` // default: 30s
	MaxMemoryMB int           `yaml:"max_memory_mb"` // default: 64
}

// Options — technical tuning (developers).
// TransactionOptions controls transactional action limits.
type TransactionOptions struct {
	PerTransactionLimit float64 `yaml:"per_transaction_limit"` // max USD per single transaction (0 = no spend)
	DailyLimit          float64 `yaml:"daily_limit"`           // max USD daily total (0 = no spend)
}

type Options struct {
	Models      ModelOptions       `yaml:"models"`
	Database    DatabaseOptions    `yaml:"database"`
	Agents      AgentOptions       `yaml:"agents"`
	Prompts     PromptOptions      `yaml:"prompts"`
	Cost        CostOptions        `yaml:"cost"`
	Typing      TypingOptions      `yaml:"typing"`
	Media       MediaOptions       `yaml:"media"`
	Sessions    SessionOptions     `yaml:"sessions"`
	Schedule    ScheduleOptions    `yaml:"schedule"`
	Voice       VoiceOptions       `yaml:"voice"`
	Plugins     PluginOptions      `yaml:"plugins"`
	Transaction TransactionOptions `yaml:"transaction"`
}

// Config is the top-level configuration struct with three enrichment layers.
type Config struct {
	Settings       Settings       `yaml:"settings"`
	Configurations Configurations `yaml:"configurations"`
	Options        Options        `yaml:"options"`
}

// Default returns a Config with all default values.
func Default() *Config {
	homeDir, _ := os.UserHomeDir()
	butlerDir := filepath.Join(homeDir, ".aibutler")

	return &Config{
		Settings: Settings{
			Language:        "en",
			Timezone:        "UTC",
			Notifications:   true,
			MorningBriefing: "8:00 AM",
			ActiveChannels:  []string{"webchat"},
			Model:           "claude-sonnet-4-6",
			PersonaName:     "Butler",
			Skills:          []string{"coding", "research"},
			AgentsEnabled:   true,
			AgentMode:       "auto",
			Cost: CostSettings{
				Strategy:      "balanced",
				MonthlyBudget: 10.00,
			},
		},
		Configurations: Configurations{
			Models: ModelConfig{
				Primary: "claude-sonnet-4-6",
			},
			Agents: AgentConfig{
				MaxConcurrent:   5,
				MaxDepth:        3,
				DefaultSubModel: "haiku",
			},
			Security: SecurityConfig{
				Shell: ShellConfig{
					Mode: "allowlist",
					// Sensible read-only default set. Users can replace via
					// configurations.security.shell.allowed in config.yaml.
					// Destructive commands (rm, mv, cp, chmod, sudo, curl,
					// wget, git, ssh, docker, kill) are intentionally NOT
					// in the default — the shell sandbox also blocks $VAR
					// expansion and shell metacharacters, so this list is
					// safe as a starting point.
					Allowed: []string{
						"ls", "cat", "head", "tail", "wc",
						"grep", "find", "echo", "pwd", "date",
						"uname", "whoami", "hostname", "which",
						"basename", "dirname", "file", "stat",
						"sort", "uniq", "cut", "tr", "env",
						"printenv", "true", "false",
					},
				},
			},
			Cost: CostConfig{
				Alerts:          []int{50, 75, 90, 100},
				AlertChannel:    "same",
				OnBudgetReached: "warn",
			},
			Prompts: PromptConfig{
				PersonaFile: filepath.Join(butlerDir, "prompts", "persona.yaml"),
				SkillsDir:   filepath.Join(butlerDir, "prompts", "skills"),
			},
			Web: WebConfig{
				Port:          3377,
				BindAddress:   "localhost",
				MaxUploadSize: 20,
			},
			Schedule: ScheduleConfig{
				Enabled: true,
			},
			IoT: IoTConfig{
				Adapter: "stub",
			},
			Voice: VoiceConfig{
				STTProvider: "whisper",
				TTSProvider: "stub",
			},
			Plugins: PluginConfig{
				AutoEnable: false,
				PluginDir:  "", // Resolved at bootstrap to $data_dir/plugins
			},
			A2A: A2AConfig{
				Port: 8081,
			},
			MCPServer: MCPServerExposure{
				AllowedCapabilities: []string{"memory.read", "data.read"},
			},
			Swarm: SwarmConfig{
				MaxDepth:          4,
				WorkspaceTTLHours: 24,
			},
			Registry: RegistryConfig{
				HealthTTLMinutes: 5,
				SelfRegister:     true,
			},
		},
		Options: Options{
			Models: ModelOptions{
				MaxTokens:      8192,
				Temperature:    0.7,
				RequestTimeout: 120 * time.Second,
				RetryCount:     3,
			},
			Database: DatabaseOptions{
				BusyTimeout: 5000,
			},
			Agents: AgentOptions{
				MaxToolCalls:      50,
				SubagentTimeout:   5 * time.Minute,
				BackgroundTimeout: 4 * time.Hour,
				BackgroundMax:     3,
				AutonomyLevel:     "l1",
				ParallelToolLimit: 5,
			},
			Prompts: PromptOptions{
				MaxTier1Tokens:        700,
				MaxSkillsPerTurn:      3,
				SkillTriggerThreshold: 0.5,
				TruncationStrategy:    "balanced",
				MaxInstructionTokens:  200,
			},
			Cost: CostOptions{
				CacheTTL:           5 * time.Minute,
				ExpensiveThreshold: 5000,
			},
			Typing: TypingOptions{
				IntervalMs: 3000,
				TimeoutMs:  120000,
			},
			Media: MediaOptions{
				MaxUploadSizeMB: 20,
				MaxTextLines:    500,
			},
			Sessions: SessionOptions{
				CleanupInterval: 1 * time.Hour,
				MaxAge:          7 * 24 * time.Hour,
			},
			Schedule: ScheduleOptions{
				TickInterval:  60 * time.Second,
				MaxConcurrent: 3,
			},
			Voice: VoiceOptions{
				MaxAudioSizeMB: 25,
				STTTimeout:     30 * time.Second,
			},
			Plugins: PluginOptions{
				MaxPlugins:  20,
				ExecTimeout: 30 * time.Second,
				MaxMemoryMB: 64,
			},
		},
	}
}

// Load reads a YAML config file and merges it with defaults.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", filepath.Base(path), err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", filepath.Base(path), err)
	}

	cfg.Resolve()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadOrDefault loads from the default config path, or returns defaults if the file doesn't exist.
func LoadOrDefault() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		cfg := Default()
		cfg.Resolve()
		return cfg, nil
	}

	path := filepath.Join(homeDir, ".aibutler", "config.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := Default()
		cfg.Resolve()
		return cfg, nil
	}

	return Load(path)
}

// Validate checks the configuration for invalid values.
func (c *Config) Validate() error {
	// Cost strategy must be one of the known values.
	switch c.Settings.Cost.Strategy {
	case "frugal", "balanced", "quality":
		// ok
	default:
		return fmt.Errorf("config: invalid cost strategy %q (must be frugal, balanced, or quality)", c.Settings.Cost.Strategy)
	}

	if c.Settings.Cost.MonthlyBudget < 0 {
		return fmt.Errorf("config: monthly_budget cannot be negative")
	}

	// Agent mode must be valid.
	switch c.Settings.AgentMode {
	case "auto", "single", "multi", "custom", "swarm":
		// ok
	default:
		return fmt.Errorf("config: invalid agent_mode %q (must be auto, single, multi, custom, or swarm)", c.Settings.AgentMode)
	}

	// Agent depth must be within safe bounds (1-3 hops).
	if c.Configurations.Agents.MaxDepth < 1 || c.Configurations.Agents.MaxDepth > 3 {
		return fmt.Errorf("config: max_depth must be between 1 and 3, got %d", c.Configurations.Agents.MaxDepth)
	}

	// Custom roles: max 10.
	if len(c.Configurations.Agents.CustomRoles) > 10 {
		return fmt.Errorf("config: max 10 custom_roles allowed, got %d", len(c.Configurations.Agents.CustomRoles))
	}
	// Routing must be valid if custom mode is used.
	if c.Settings.AgentMode == "custom" {
		switch c.Configurations.Agents.Routing {
		case "classify", "explicit", "round-robin", "":
			// ok (empty defaults to classify)
		default:
			return fmt.Errorf("config: invalid routing %q (must be classify, explicit, or round-robin)", c.Configurations.Agents.Routing)
		}
	}

	// Autonomy level must be valid.
	switch c.Options.Agents.AutonomyLevel {
	case "l1", "l2", "l3", "":
		// ok (empty defaults to l1)
	default:
		return fmt.Errorf("config: invalid autonomy_level %q (must be l1, l2, or l3)", c.Options.Agents.AutonomyLevel)
	}

	// L3 time bound validation.
	if c.Options.Agents.AutonomyLevel == "l3" {
		if c.Options.Agents.L3TimeBound > 24*time.Hour {
			return fmt.Errorf("config: l3_time_bound cannot exceed 24 hours")
		}
	}

	// Options constraints.
	if c.Options.Prompts.MaxTier1Tokens <= 0 {
		return fmt.Errorf("config: max_tier1_tokens must be positive")
	}
	if c.Options.Prompts.MaxSkillsPerTurn <= 0 {
		return fmt.Errorf("config: max_skills_per_turn must be positive")
	}
	if c.Options.Agents.MaxToolCalls <= 0 {
		return fmt.Errorf("config: max_tool_calls must be positive")
	}

	// Shell mode must be a known value.
	if c.Configurations.Security.Shell.Mode != "" && c.Configurations.Security.Shell.Mode != "allowlist" && c.Configurations.Security.Shell.Mode != "denylist" {
		return fmt.Errorf("config: invalid shell mode %q (must be allowlist or denylist)", c.Configurations.Security.Shell.Mode)
	}

	// Web port must be valid.
	if c.Configurations.Web.Port < 0 || c.Configurations.Web.Port > 65535 {
		return fmt.Errorf("config: web port must be between 0 and 65535, got %d", c.Configurations.Web.Port)
	}

	// MaxTokens must be positive if set.
	if c.Options.Models.MaxTokens <= 0 {
		return fmt.Errorf("config: max_tokens must be positive")
	}

	// Temperature must be in range.
	if c.Options.Models.Temperature < 0 || c.Options.Models.Temperature > 2 {
		return fmt.Errorf("config: temperature must be between 0 and 2, got %f", c.Options.Models.Temperature)
	}

	// RetryCount must be non-negative.
	if c.Options.Models.RetryCount < 0 {
		return fmt.Errorf("config: retry_count cannot be negative")
	}

	// RequestTimeout must be positive.
	if c.Options.Models.RequestTimeout <= 0 {
		return fmt.Errorf("config: request_timeout must be positive")
	}

	// A2A port must be valid if set.
	if c.Configurations.A2A.Port < 0 || c.Configurations.A2A.Port > 65535 {
		return fmt.Errorf("config: a2a port must be between 0 and 65535, got %d", c.Configurations.A2A.Port)
	}

	// Spending limits must be non-negative.
	if c.Options.Transaction.PerTransactionLimit < 0 {
		return fmt.Errorf("config: per_transaction_limit cannot be negative")
	}
	if c.Options.Transaction.DailyLimit < 0 {
		return fmt.Errorf("config: daily_limit cannot be negative")
	}

	// Plugin options: MaxPlugins must be non-negative.
	if c.Options.Plugins.MaxPlugins < 0 {
		return fmt.Errorf("config: max_plugins cannot be negative")
	}
	if c.Options.Plugins.ExecTimeout < 0 {
		return fmt.Errorf("config: plugin exec_timeout cannot be negative")
	}
	if c.Options.Plugins.MaxMemoryMB < 0 {
		return fmt.Errorf("config: plugin max_memory_mb cannot be negative")
	}

	return nil
}

// Resolve applies resolution rules: settings override configurations where applicable.
func (c *Config) Resolve() {
	// Settings.Model overrides Configurations.Models.Primary
	if c.Settings.Model != "" {
		c.Configurations.Models.Primary = c.Settings.Model
	}
}

// SlidingWindowSize returns the number of messages for the sliding window
// based on the cost strategy.
func (c *Config) SlidingWindowSize() int {
	switch c.Settings.Cost.Strategy {
	case "frugal":
		return 30
	case "quality":
		return 200
	default: // balanced
		return 100
	}
}

// SkillsDir returns the resolved skills directory path.
func (c *Config) SkillsDir() string {
	if c.Configurations.Prompts.SkillsDir != "" {
		return c.Configurations.Prompts.SkillsDir
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".aibutler", "prompts", "skills")
}

// StartWatcher polls the config file at the given interval and calls onChange
// when non-destructive changes are detected (log level, channel settings, hooks).
// It runs until the context is cancelled and returns a stop function.
func (c *Config) StartWatcher(ctx context.Context, path string, interval time.Duration, onChange func(*Config)) func() {
	if interval <= 0 {
		interval = 60 * time.Second
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		var lastMod time.Time

		// Get initial mod time.
		if info, err := os.Stat(path); err == nil {
			lastMod = info.ModTime()
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				info, err := os.Stat(path)
				if err != nil {
					continue
				}
				if !info.ModTime().After(lastMod) {
					continue
				}
				lastMod = info.ModTime()

				newCfg, err := Load(path)
				if err != nil {
					log.Printf("config: hot-reload parse error: %v", err)
					continue
				}

				// Apply only non-destructive fields.
				c.Configurations.Hooks = newCfg.Configurations.Hooks
				c.Configurations.Permissions = newCfg.Configurations.Permissions

				if onChange != nil {
					onChange(newCfg)
				}
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}
