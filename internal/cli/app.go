package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"log"
	"net/http"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/audit"
	"github.com/LumabyteCo/aibutler/internal/backup"
	"github.com/LumabyteCo/aibutler/internal/browser"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/channel"
	wapkg "github.com/LumabyteCo/aibutler/internal/channel/whatsapp"
	clippkg "github.com/LumabyteCo/aibutler/internal/clipboard"
	costpkg "github.com/LumabyteCo/aibutler/internal/cost"
	permpkg "github.com/LumabyteCo/aibutler/internal/permissions"
	waitpkg "github.com/LumabyteCo/aibutler/internal/wait"
	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/contact"
	"github.com/LumabyteCo/aibutler/internal/db"
	"github.com/LumabyteCo/aibutler/internal/discord"
	"github.com/LumabyteCo/aibutler/internal/file"
	"github.com/LumabyteCo/aibutler/internal/finance"
	"github.com/LumabyteCo/aibutler/internal/health"
	"github.com/LumabyteCo/aibutler/internal/instruction"
	"github.com/LumabyteCo/aibutler/internal/iot"
	"github.com/LumabyteCo/aibutler/internal/media/archive"
	mediacontact "github.com/LumabyteCo/aibutler/internal/media/contact"
	"github.com/LumabyteCo/aibutler/internal/media/spreadsheet"
	"github.com/LumabyteCo/aibutler/internal/mcp"
	"github.com/LumabyteCo/aibutler/internal/media"
	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/internal/memory/graph"
	"github.com/LumabyteCo/aibutler/internal/memory/hybrid"
	"github.com/LumabyteCo/aibutler/internal/memory/vector"
	"github.com/LumabyteCo/aibutler/internal/offline"
	mcpserver "github.com/LumabyteCo/aibutler/internal/mcp/server"
	pluginpkg "github.com/LumabyteCo/aibutler/internal/plugin"
	"github.com/LumabyteCo/aibutler/internal/plugin/registry"
	"github.com/LumabyteCo/aibutler/internal/protocol/a2a"
	a2aregistry "github.com/LumabyteCo/aibutler/internal/protocol/a2a/registry"
	swarmws "github.com/LumabyteCo/aibutler/internal/memory/swarm"
	oauth "github.com/LumabyteCo/aibutler/internal/proxy/oauth"
	emailpkg "github.com/LumabyteCo/aibutler/internal/email"
	calpkg "github.com/LumabyteCo/aibutler/internal/calendar"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/proxy"
	"github.com/LumabyteCo/aibutler/internal/schedule"
	"github.com/LumabyteCo/aibutler/internal/services"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/internal/shell"
	aspkg "github.com/LumabyteCo/aibutler/internal/shell/applescript"
	dbuspkg "github.com/LumabyteCo/aibutler/internal/shell/dbus"
	dispatchpkg "github.com/LumabyteCo/aibutler/internal/shell/dispatch"
	pspkg "github.com/LumabyteCo/aibutler/internal/shell/powershell"
	scutpkg "github.com/LumabyteCo/aibutler/internal/shell/shortcuts"
	"github.com/LumabyteCo/aibutler/internal/slack"
	"github.com/LumabyteCo/aibutler/internal/taskctx"
	"github.com/LumabyteCo/aibutler/internal/telegram"
	"github.com/LumabyteCo/aibutler/internal/telemetry"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/internal/vault"
	"github.com/LumabyteCo/aibutler/internal/voice"
	"github.com/LumabyteCo/aibutler/internal/voice/deepgram"
	"github.com/LumabyteCo/aibutler/internal/voice/elevenlabs"
	"github.com/LumabyteCo/aibutler/internal/voice/piper"
	"github.com/LumabyteCo/aibutler/internal/webchat"
	"github.com/LumabyteCo/aibutler/internal/webchat/dashboard"
	"github.com/LumabyteCo/aibutler/internal/webchat/lan"
	"github.com/LumabyteCo/aibutler/internal/webchat/setup"
	"github.com/LumabyteCo/aibutler/internal/updater"
	"github.com/LumabyteCo/aibutler/internal/coding"
	"github.com/LumabyteCo/aibutler/internal/plugin/scanner"
	"github.com/LumabyteCo/aibutler/internal/plugin/sandbox"
	incrementalpkg "github.com/LumabyteCo/aibutler/internal/backup/incremental"
	"github.com/LumabyteCo/aibutler/internal/hook"
	gitpkg "github.com/LumabyteCo/aibutler/internal/git"
	linepkg "github.com/LumabyteCo/aibutler/internal/channel/line"
	ircpkg "github.com/LumabyteCo/aibutler/internal/channel/irc"
	teamspkg "github.com/LumabyteCo/aibutler/internal/channel/teams"
	gchatpkg "github.com/LumabyteCo/aibutler/internal/channel/gchat"
	webhookpkg "github.com/LumabyteCo/aibutler/internal/channel/webhook"
	nostrpkg "github.com/LumabyteCo/aibutler/internal/channel/nostr"
	videopkg "github.com/LumabyteCo/aibutler/internal/media/video"
	txpkg "github.com/LumabyteCo/aibutler/internal/transaction"
	designpkg "github.com/LumabyteCo/aibutler/internal/ai/design"
	threedpkg "github.com/LumabyteCo/aibutler/internal/ai/threed"
	workflowpkg "github.com/LumabyteCo/aibutler/internal/ai/workflow"
	batchpkg "github.com/LumabyteCo/aibutler/internal/ai/batch"
	authpkg "github.com/LumabyteCo/aibutler/internal/webchat/auth"
	pwapkg "github.com/LumabyteCo/aibutler/internal/webchat/pwa"
	rescanpkg "github.com/LumabyteCo/aibutler/internal/plugin/rescan"
	webauthnpkg "github.com/LumabyteCo/aibutler/internal/auth/webauthn"
	secmonpkg "github.com/LumabyteCo/aibutler/internal/security/monitor"
	cachepkg "github.com/LumabyteCo/aibutler/internal/cache"
	rbacpkg "github.com/LumabyteCo/aibutler/internal/rbac"
	oidcpkg "github.com/LumabyteCo/aibutler/internal/auth/oidc"
	compliancepkg "github.com/LumabyteCo/aibutler/internal/compliance"
	mcpserverv2 "github.com/LumabyteCo/aibutler/internal/mcp/server_v2"
	subprocpkg "github.com/LumabyteCo/aibutler/internal/bridge/subprocess"
	qrpkg "github.com/LumabyteCo/aibutler/internal/webchat/qr"
	marketplacepkg "github.com/LumabyteCo/aibutler/internal/plugin/marketplace"
	"github.com/LumabyteCo/aibutler/internal/model"
	shellsandbox "github.com/LumabyteCo/aibutler/internal/shell/sandbox"
	pluginstore "github.com/LumabyteCo/aibutler/internal/plugin/store"
	remotebackup "github.com/LumabyteCo/aibutler/internal/backup/remote"
)

const Version = "0.1.0"

// App is the central application struct that wires all internal packages together.
type App struct {
	Config         *config.Config
	DB             *db.DB
	Vault          vault.Vault
	Engine         *capability.Engine
	Channels       *channel.Registry
	Tools          *tool.Registry
	Sessions       *session.Manager
	Composer       *prompt.Composer
	Tracker        *prompt.Tracker
	Dispatcher     *tool.Dispatcher
	Scheduler      *schedule.Scheduler
	MCPClient      *mcp.Client
	Telemetry      *telemetry.Collector
	Offline        *offline.Guard
	MediaPipeline  *media.Pipeline
	Voice          *voice.Pipeline
	Lifecycle      *vault.TokenLifecycleManager
	HybridSearcher *hybrid.Searcher
	VectorStore    *vector.Store
	MemStore       *memory.Store
	EntityStore    *entity.Store
	PluginRuntime  pluginpkg.Runtime
	PluginRegistry *registry.Registry
	MCPServer      *mcpserver.Server
	A2AHandler     *a2a.Handler
	AgentRegistry  *a2aregistry.Registry
	SwarmWorkspace *swarmws.Workspace
	OAuthStore     *oauth.Store
	EmailClient    *emailpkg.Client
	CalClient      *calpkg.Client

	// Channels, Media, Voice + Agent Mesh
	WhatsApp       *wapkg.Client
	BrowserClient  *browser.Client
	ElevenLabs     *elevenlabs.Client
	Deepgram       *deepgram.Client
	Piper          *piper.Executor

	// Platform, UX & Swarm Dashboard
	Dashboard      *dashboard.Dashboard
	LANDiscovery   *lan.Discovery
	SetupWizard    *setup.Wizard
	Updater        *updater.Updater
	CodingRunner   *coding.Runner

	// Hardening, Polish + Swarm Safety
	PluginScanner     *scanner.Scanner
	PluginSandbox     *sandbox.Sandbox
	IncrementalBackup *incrementalpkg.Manager

	// Hook Engine, Git, Permissions
	HookEngine        *hook.Engine
	GitClient         *gitpkg.Client
	Prompter          *capability.InteractivePrompter

	// Channels, Browser Interactive, Video, Voice Enhancements
	LINEClient        *linepkg.Client
	IRCClient         *ircpkg.Client
	InteractiveBrowser *browser.InteractiveClient
	VideoProcessor    *videopkg.Processor
	TUIVoice          *voice.TUIMode
	WakeWord          *voice.WakeWordDetector

	// Channels — Teams, Google Chat, Webhook & Nostr
	TeamsClient    *teamspkg.Client
	GChatClient    *gchatpkg.Client
	WebhookAdapter *webhookpkg.Adapter
	NostrClient    *nostrpkg.Client

	// Transactional Actions & AI Services
	TransactionEngine *txpkg.Engine

	// Advanced Security (WebAuthn, Monitoring)
	WebAuthnServer   *webauthnpkg.Server
	SecurityMonitor  *secmonpkg.Monitor

	// Multi-Agent (stored for swarm mode access)
	AgentRouter      interface{} // *router.Router when swarm mode active
	MessageBus       interface{} // *bus.Bus when swarm mode active

	// Orphan fixes (Pass 10): previously built but never wired
	ShellSandbox     *shellsandbox.Sandbox
	PluginKVStore    *pluginstore.Store
	RemoteBackup     *remotebackup.Client

	// Web App (Internet Mode, PWA, Dashboard)
	Authenticator    *authpkg.Authenticator
	PluginRescanner  *rescanpkg.Rescanner

	// Wired services
	ResponseCache    *cachepkg.Cache
	RBAC             *rbacpkg.Engine
	OIDCClient       *oidcpkg.Client
	ComplianceLogger *compliancepkg.Logger
	MCPServerV2      *mcpserverv2.Server
	// MCPAgentAdapter is exported so cmd_run can inject the agent factory
	// after it's constructed (the factory doesn't exist at app.New time).
	MCPAgentAdapter *mcpV2AgentAdapter
	SubprocessBridges map[string]*subprocpkg.Adapter
	Marketplace      *marketplacepkg.Registry
	BatchExecutor    *model.BatchExecutor

	configPath     string
	dataDir        string
	webChatAdapter *webchat.Adapter // reference for mounting dashboard/setup handlers
}

// DefaultDataDir returns ~/.aibutler.
func DefaultDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aibutler")
}

// ConfigPath resolves config file path: $AIBUTLER_CONFIG > dataDir/config.yaml.
func ConfigPath(dataDir string) string {
	if p := os.Getenv("AIBUTLER_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(dataDir, "config.yaml")
}

// Bootstrap creates and initializes all app components.
// Pass dbPath="" for default (dataDir/aibutler.db), or ":memory:" for tests.
func Bootstrap(dataDir, dbPath string) (*App, error) {
	app := &App{dataDir: dataDir}

	// Ensure data directory exists.
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	// 1. Load config.
	app.configPath = ConfigPath(dataDir)
	cfg, err := config.LoadOrDefault()
	if err != nil {
		cfg = config.Default()
	}
	cfg.Resolve()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}
	app.Config = cfg

	// 1b. Create offline guard and guarded HTTP client early so all HTTP calls respect offline mode.
	app.Offline = offline.NewGuard(cfg.Settings.OfflineMode)
	guardedClient := offline.NewGuardedClient(app.Offline, 15*time.Second)

	// 1c. Install audit-redacting log writer FIRST to protect all subsequent log output.
	log.SetOutput(audit.NewRedactingWriter(log.Writer()))

	// 2. Open DB + apply schema.
	if dbPath == "" {
		dbPath = filepath.Join(dataDir, "aibutler.db")
	}
	database, err := db.Open(db.Config{Path: dbPath, BusyTimeout: cfg.Options.Database.BusyTimeout})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	ctx := context.Background()
	if err := database.ApplySchema(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	app.DB = database

	// 2b. Run integrity check on startup.
	if err := backup.IntegrityCheck(database.Conn()); err != nil {
		log.Printf("WARNING: database integrity check failed: %v", err)
	}

	// 3. Initialize vault.
	v, err := vault.New(vault.Config{
		VaultDir: filepath.Join(dataDir, "vault"),
	})
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("init vault: %w", err)
	}
	app.Vault = v

	// 4. Capability engine with SQLite audit logging.
	auditor := audit.NewSQLiteAuditor(database.Conn())
	app.Engine = capability.NewEngine(auditor)

	// 5. Tool registry.
	app.Tools = tool.NewRegistry()

	// 5b. Register data tools.
	tool.RegisterDataTools(app.Tools, database.Conn())

	// 5c. Register file tools (path-bounded file operations).
	// Defaults include the data dir and the user's home. Additional paths
	// can be added via configurations.file.allow_paths in config.yaml.
	filePaths := []string{dataDir}
	if home, err := os.UserHomeDir(); err == nil {
		filePaths = append(filePaths, home)
	}
	filePaths = append(filePaths, cfg.Configurations.File.AllowPaths...)
	file.RegisterFileTools(app.Tools, filePaths)

	// 5c2. Register task context tools (multi-step task state machine).
	tcStore := taskctx.NewStore(database.Conn())
	taskctx.RegisterTaskContextTools(app.Tools, tcStore)

	// 5d. Register instruction tools (learned instructions: save, list, update, remove).
	instrStore := instruction.NewStore(database.Conn())
	instrDetector := instruction.NewDetector(instrStore)
	instruction.RegisterInstructionTools(app.Tools, instrStore)

	// 5d2. Register memory tools (living memory: capture, thoughts, facts).
	// MemStore is the single shared instance used by both the capture tool
	// (here) and postRunProcessor (in cmd_run.go). Keeping one instance means
	// SetIndexer in resolveHandler wires vector indexing into every save path.
	app.MemStore = memory.NewStore(database.Conn())
	memory.RegisterMemoryTools(app.Tools, app.MemStore, instrDetector)

	// 5d3. Register advanced memory tools (FTS search, entities, graph, hybrid search, vector).
	ftsStore := fts.NewStore(database.Conn())
	app.EntityStore = entity.NewStore(database.Conn())
	graphStore := graph.NewStore(database.Conn())
	vectorStore := vector.NewStore(database.Conn())
	hybridSearcher := hybrid.NewSearcher(ftsStore, app.EntityStore)
	app.HybridSearcher = hybridSearcher
	app.VectorStore = vectorStore
	// Vector search wired in resolveHandler() when an embedding provider is available.
	memory.RegisterP2MemoryTools(app.Tools, memory.P2Deps{
		FTS:    ftsStore,
		Entity: app.EntityStore,
		Graph:  graphStore,
		Hybrid: hybridSearcher,
		Vector: vectorStore,
	})

	// 5e. Register schedule tools (create, list, delete scheduled tasks).
	schedStore := schedule.NewStore(database.Conn())
	schedule.RegisterScheduleTools(app.Tools, schedStore, nil) // LLM wired later in resolveHandler

	// 5f. Register shell tools (sandboxed POSIX shell execution).
	shellExec := shell.NewExecutor(cfg.Configurations.Security.Shell, nil)
	shell.RegisterShellTools(app.Tools, shellExec)

	// 5g. Register proxy tools (web.fetch, web.search).
	svcRegistry, _ := vault.NewServiceRegistry("")
	credResolver := proxy.NewCredentialResolver(svcRegistry, v)
	httpExec := proxy.NewHTTPExecutor(30 * time.Second)
	tokenRefresher := proxy.NewTokenRefresher(v, &http.Client{Timeout: 15 * time.Second})
	p := proxy.NewProxy(app.Engine, credResolver, httpExec, tokenRefresher, nil)
	proxy.RegisterProxyTools(app.Tools, p)

	// 5g2. Token lifecycle manager (refreshes OAuth tokens before expiry).
	app.Lifecycle = vault.NewTokenLifecycleManager(v, svcRegistry,
		vault.WithRefreshFunc(tokenRefresher.Refresh))

	// 5h. Register finance tools (market.price, market.watchlist, market.alerts).
	watchlistStore := finance.NewWatchlistStore(database.Conn())
	alphaKeyCred, _ := v.Get(ctx, "alphavantage_api_key")
	if len(alphaKeyCred.Value) > 0 {
		finProvider := finance.NewAlphaVantageProvider(string(alphaKeyCred.Value), guardedClient)
		finance.RegisterFinanceTools(app.Tools, finProvider, watchlistStore)
	} else {
		// Register stub tools that explain how to enable finance features.
		app.Tools.Register(&tool.FuncTool{
			ToolName: "market.price", ToolDesc: "Get stock/crypto prices (requires API key)",
			ToolSchema: `{"type":"object","properties":{"symbol":{"type":"string"}},"required":["symbol"]}`,
			ToolCap: "data.finance.read",
			Exec: func(_ context.Context, _ string) (string, error) {
				return "", fmt.Errorf("finance tools not configured. Run: aibutler vault set alphavantage_api_key YOUR_KEY")
			},
		})
	}

	// 5j. Register everyday services (weather, news, maps, sports, transit, flight, tracking, recipe) — uses offline-guarded client.
	weatherKey, _ := v.Get(ctx, "openweathermap_api_key")
	newsKey, _ := v.Get(ctx, "newsapi_key")
	sportsKey, _ := v.Get(ctx, "sports_api_key")
	transitKey, _ := v.Get(ctx, "transit_api_key")
	flightKey, _ := v.Get(ctx, "flight_api_key")
	trackingKey, _ := v.Get(ctx, "tracking_api_key")
	recipeKey, _ := v.Get(ctx, "recipe_api_key")
	services.RegisterEverydayServices(app.Tools, string(weatherKey.Value), string(newsKey.Value), string(sportsKey.Value), string(transitKey.Value), string(flightKey.Value), string(trackingKey.Value), string(recipeKey.Value), guardedClient)

	// 5i. Register MCP tools (connect to configured MCP servers).
	if len(cfg.Configurations.MCP.Servers) > 0 {
		mcpClient := mcp.NewClient()
		connected := 0
		for _, srv := range cfg.Configurations.MCP.Servers {
			// Build env map: start with static env, then overlay vault-resolved secrets.
			env := make(map[string]string)
			for k, val := range srv.Env {
				env[k] = val
			}
			for vaultKey, envVar := range srv.VaultEnv {
				if cred, err := v.Get(ctx, vaultKey); err == nil && len(cred.Value) > 0 {
					env[envVar] = string(cred.Value)
				} else {
					log.Printf("mcp: vault_env: %s not found for %s (env %s)", vaultKey, srv.Name, envVar)
				}
			}

			mcpCfg := mcp.ServerConfig{
				Name:    srv.Name,
				Command: srv.Command,
				Args:    srv.Args,
				Env:     env,
			}
			if err := mcpClient.Connect(ctx, mcpCfg); err != nil {
				log.Printf("mcp: failed to connect to %s: %v", srv.Name, err)
				continue
			}
			tools, _ := mcpClient.Tools(srv.Name)
			log.Printf("mcp: connected to %s (%d tools)", srv.Name, len(tools))
			connected++
		}
		if connected > 0 {
			app.MCPClient = mcpClient
			mcp.RegisterMCPTools(app.Tools, mcpClient)
			log.Printf("mcp: %d/%d servers connected", connected, len(cfg.Configurations.MCP.Servers))
		} else {
			log.Println("mcp: no servers connected successfully")
		}
	}

	// 6. Media pipeline (created before channels so webchat can use it).
	app.MediaPipeline = media.NewDefaultPipeline(int64(cfg.Options.Media.MaxUploadSizeMB))

	// 6a. Channel registry — register active channels.
	app.Channels = channel.NewRegistry()
	for _, chName := range cfg.Settings.ActiveChannels {
		switch chName {
		case "webchat":
			webCfg := webchat.Config{
				Port:          cfg.Configurations.Web.Port,
				BindAddress:   cfg.Configurations.Web.BindAddress,
				MaxUploadSize: cfg.Configurations.Web.MaxUploadSize * 1024 * 1024,
				Pipeline:      app.MediaPipeline,
			}
			wa := webchat.New(webCfg)
			app.webChatAdapter = wa
			app.Channels.Register(wa)
		case "slack":
			slackBotToken, _ := v.Get(ctx, "slack_bot_token")
			slackAppToken, _ := v.Get(ctx, "slack_app_token")
			if len(slackBotToken.Value) > 0 && len(slackAppToken.Value) > 0 {
				api := slack.NewAPIClient(string(slackBotToken.Value), string(slackAppToken.Value))
				app.Channels.Register(slack.New(api))
				log.Println("channel: slack adapter registered")
			} else {
				log.Println("channel: slack requires slack_bot_token and slack_app_token in vault")
			}
		case "discord":
			discordToken, _ := v.Get(ctx, "discord_bot_token")
			if len(discordToken.Value) > 0 {
				api := discord.NewAPIClient(string(discordToken.Value))
				app.Channels.Register(discord.NewWithToken(api, string(discordToken.Value)))
				log.Println("channel: discord adapter registered")
			} else {
				log.Println("channel: discord requires discord_bot_token in vault")
			}
		case "telegram":
			tgToken, _ := v.Get(ctx, "telegram_bot_token")
			if len(tgToken.Value) > 0 {
				tgAPI := telegram.NewAPIClient(string(tgToken.Value))
				app.Channels.Register(telegram.New(tgAPI))
				log.Println("channel: telegram adapter registered")
			} else {
				log.Println("channel: telegram requires telegram_bot_token in vault")
			}
		}
	}

	// 6a. Register IoT tools (stub adapter for v0.1).
	// The stub comes pre-populated with a small set of demo devices so the
	// IoT tool surface is usable out-of-the-box — useful for trying the
	// natural-language flow ("turn off the living room lights", "what's
	// the temperature upstairs?") before the Home Assistant adapter ships
	// in v0.2. TODO(v0.2): replace with real adapter configured via
	// configurations.iot.adapter.
	//
	// Important: devices must be registered at BOTH the adapter (for
	// discover/read_sensor) AND the controller (for tier-aware execution
	// of ReadSensor/ExecuteCommand). The controller keeps its own device
	// registry that's used for tier lookups before delegating to the
	// adapter. See internal/iot/iot.go:Controller.devices.
	iotAdapter := iot.NewStubAdapter()
	iotCtrl := iot.NewController(iotAdapter, app.Engine, iot.NewPINVerifier(v))

	demoDevices := []iot.Device{
		{ID: "light-living-room", Name: "Living Room Light", DeviceType: "light", Adapter: "stub", Tier: iot.TierComfort, Enabled: true},
		{ID: "light-kitchen", Name: "Kitchen Light", DeviceType: "light", Adapter: "stub", Tier: iot.TierComfort, Enabled: true},
		{ID: "thermostat-main", Name: "Main Thermostat", DeviceType: "thermostat", Adapter: "stub", Tier: iot.TierComfort, Enabled: true},
		{ID: "sensor-living-room", Name: "Living Room Motion + Climate Sensor", DeviceType: "sensor", Adapter: "stub", Tier: iot.TierSensor, Enabled: true},
		{ID: "lock-front-door", Name: "Front Door Lock", DeviceType: "lock", Adapter: "stub", Tier: iot.TierSafety, Enabled: true},
	}
	for _, d := range demoDevices {
		iotAdapter.AddDevice(d)
		iotCtrl.RegisterDevice(d)
	}

	// Seed sample sensor readings so iot.sensor.read returns realistic data.
	iotAdapter.AddReading("sensor-living-room",
		iot.SensorReading{DeviceID: "sensor-living-room", Metric: "temperature", Value: 21.3, Unit: "°C"},
		iot.SensorReading{DeviceID: "sensor-living-room", Metric: "humidity", Value: 48.5, Unit: "%"},
		iot.SensorReading{DeviceID: "sensor-living-room", Metric: "motion", Value: 0, Unit: "bool"},
	)
	iotAdapter.AddReading("thermostat-main",
		iot.SensorReading{DeviceID: "thermostat-main", Metric: "current_temperature", Value: 21.8, Unit: "°C"},
		iot.SensorReading{DeviceID: "thermostat-main", Metric: "target_temperature", Value: 21.0, Unit: "°C"},
	)

	iot.RegisterIoTTools(app.Tools, iotCtrl)

	// 6b. Register channel tools (channel.send, channel.read, channel.relay).
	contactResolver := contact.NewResolver(database.Conn())
	channel.RegisterChannelToolsWithDeps(app.Tools, app.Channels, database.Conn(), contactResolver)

	// 6c. Plugin system (WASM plugins via Extism).
	pluginDir := cfg.Configurations.Plugins.PluginDir
	if pluginDir == "" {
		pluginDir = filepath.Join(dataDir, "plugins")
	}
	_ = os.MkdirAll(pluginDir, 0700)
	pluginRuntime := pluginpkg.NewExtismRuntime()
	pluginAuditor := pluginpkg.NewSQLiteAuditWriter(database.Conn())
	// toolRegAdapter wraps tool.Registry to satisfy registry.ToolRegistry interface.
	pluginReg := registry.New(database.Conn(), pluginRuntime, &toolRegAdapter{reg: app.Tools}, &toolCallerAdapter{reg: app.Tools}, &vaultAdapter{v: v}, pluginAuditor, pluginDir, cfg.Options.Plugins.MaxPlugins)
	app.PluginRuntime = pluginRuntime
	app.PluginRegistry = pluginReg

	// 6c2. Plugin security: scanner and sandbox.
	app.PluginScanner = scanner.New()
	app.PluginSandbox = sandbox.New(sandbox.DefaultPolicy())

	// 6c3. Incremental backup manager.
	backupDir := filepath.Join(dataDir, "backups")
	_ = os.MkdirAll(backupDir, 0700)
	app.IncrementalBackup = incrementalpkg.New(dbPath, backupDir)

	// 6c4. Shell sandbox (Linux unshare, macOS sandbox-exec).
	sandboxMode := shellsandbox.ModeWorkspaceOnly
	if cfg.Configurations.Sandbox.Mode != "" {
		sandboxMode = shellsandbox.Mode(cfg.Configurations.Sandbox.Mode)
	}
	app.ShellSandbox = shellsandbox.New(sandboxMode, dataDir, cfg.Configurations.Sandbox.AllowPaths)

	// 6c5. Plugin KV store (for plugin-scoped persistent storage).
	app.PluginKVStore = pluginstore.New(database.Conn(), 0) // global store (pluginID=0)

	// 6c6. Remote backup client (S3/HTTP, only if configured).
	if cfg.Configurations.Backup.Remote.Endpoint != "" {
		app.RemoteBackup = remotebackup.NewClient(remotebackup.Config{
			Provider:   remotebackup.Provider(cfg.Configurations.Backup.Remote.Provider),
			Endpoint:   cfg.Configurations.Backup.Remote.Endpoint,
			Bucket:     cfg.Configurations.Backup.Remote.Bucket,
			AccessKey:  cfg.Configurations.Backup.Remote.AccessKey,
			SecretKey:  cfg.Configurations.Backup.Remote.SecretKey,
			Region:     cfg.Configurations.Backup.Remote.Region,
			EncryptKey: cfg.Configurations.Backup.Remote.EncryptKey,
		})
	}

	// Load all previously-enabled plugins. Errors logged, not fatal.
	if errs := pluginReg.EnableAll(ctx); len(errs) > 0 {
		for _, e := range errs {
			log.Printf("plugin: %v", e)
		}
	}

	// 6d. MCP Server (not started until `mcp serve` command).
	mcpLister := mcpserver.NewRegistryLister(&toolProviderAdapter{reg: app.Tools}, cfg.Configurations.MCPServer.AllowedCapabilities)
	app.MCPServer = mcpserver.New(mcp.ServerInfo{Name: "aibutler", Version: Version}, mcpLister)

	// 6e. A2A Handler (activated when A2A is enabled).
	// NOTE: The handler is created but no HTTP server is started here.
	// A2A port listening is deferred until HTTP router consolidation,
	// which will unify WebChat, A2A, and future OAuth callback servers into a
	// single multiplexed listener. Until then, A2A is testable via direct
	// handler invocation and httptest, but not reachable over the network.
	if cfg.Configurations.A2A.Enabled {
		a2aCard := a2a.AgentCard{
			Name:         cfg.Settings.PersonaName,
			Description:  "AI Butler personal assistant",
			Capabilities: cfg.Configurations.MCPServer.AllowedCapabilities,
			Version:      Version,
		}
		app.A2AHandler = a2a.NewHandler(database.Conn(), &a2aRunnerStub{}, a2aCard, cfg.Configurations.A2A.TokenHashes)
	}

	// 6f. Agent registry (A2A v2 peer discovery).
	app.AgentRegistry = a2aregistry.New(database.Conn())

	// 6g. Swarm workspace.
	app.SwarmWorkspace = swarmws.NewWorkspace(database.Conn())

	// 6h. OAuth token store.
	app.OAuthStore = oauth.NewStore(database.Conn())

	// 6i. Email and calendar tools (require OAuth authorization by user).
	app.EmailClient = emailpkg.NewClient(app.OAuthStore, nil)
	emailpkg.RegisterEmailTools(app.Tools, app.EmailClient)
	app.CalClient = calpkg.NewClient(app.OAuthStore, nil)
	calpkg.RegisterCalendarTools(app.Tools, app.CalClient)

	// 6j. Register channel/media/voice tools using the funcToolRegistry adapter.
	ftReg := &funcToolRegistry{reg: app.Tools}

	// Browser tools (always available).
	app.BrowserClient = browser.NewClient()
	browser.RegisterBrowserTools(ftReg, app.BrowserClient)

	// WhatsApp tools (only when access token is configured).
	waTokenCred, _ := v.Get(ctx, "whatsapp_access_token")
	waPhoneIDCred, _ := v.Get(ctx, "whatsapp_phone_number_id")
	if len(waTokenCred.Value) > 0 && len(waPhoneIDCred.Value) > 0 {
		app.WhatsApp = wapkg.NewClient(string(waPhoneIDCred.Value), string(waTokenCred.Value))
		wapkg.RegisterWhatsAppTools(ftReg, app.WhatsApp)
		log.Println("whatsapp: tools registered")
	}

	// PowerShell tool (shell allowlist from config).
	psAllowlist := cfg.Configurations.Security.Shell.Allowed
	psExec := pspkg.NewExecutor(psAllowlist)
	pspkg.RegisterPowerShellTool(ftReg, psExec)

	// Native OS scripting — AppleScript / Shortcuts (macOS) and D-Bus (Linux).
	//
	// All four executors read the user's `Configurations.Security.Shell.Allowed`
	// list. Each executor only matches entries that fit its grammar (PowerShell
	// first-words like "Get-Date", AppleScript first-words like "tell",
	// Shortcuts names, D-Bus service:path:interface:method patterns). Empty
	// allowlist denies everything per the secure-by-default posture.
	//
	// When `Configurations.Security.Shell.UseDefaultAllowlist=true`, each
	// executor's curated DefaultAllowlist is prepended to the user list. This
	// gives a sensible starter for users who don't want to assemble an
	// allowlist from scratch, while leaving fresh installs deny-everything by
	// default. PowerShell intentionally has no DefaultAllowlist — its surface
	// shipped in v0.1 and the default policy there is "user explicitly lists
	// every cmdlet."
	//
	// Registration is unconditional across OSes — the executors return clear
	// errors at call time on mismatched OSes, so cross-platform agents see
	// useful failures rather than silently missing tools.
	useDefaults := cfg.Configurations.Security.Shell.UseDefaultAllowlist
	asAllowlist := psAllowlist
	scutAllowlist := psAllowlist
	dbusAllowlist := psAllowlist
	if useDefaults {
		asAllowlist = mergeAllowlists(aspkg.DefaultAllowlist(), psAllowlist)
		scutAllowlist = mergeAllowlists(scutpkg.DefaultAllowlist(), psAllowlist)
		dbusAllowlist = mergeAllowlists(dbuspkg.DefaultAllowlist(), psAllowlist)
	}
	asExec := aspkg.NewExecutor(asAllowlist)
	aspkg.RegisterAppleScriptTool(ftReg, asExec)
	scutRunner := scutpkg.NewRunner(scutAllowlist)
	scutpkg.RegisterShortcutsTool(ftReg, scutRunner)
	dbusClient := dbuspkg.NewClient(dbusAllowlist)
	dbuspkg.RegisterDBusTool(ftReg, dbusClient)

	// Cross-OS dispatcher (shell.script). The agent supplies a per-OS
	// payload; the dispatcher forwards the entry matching the running GOOS
	// to the relevant executor. Each executor still applies its own
	// allowlist — the dispatcher is purely a router.
	osDispatch := dispatchpkg.New()
	osDispatch.SetHandler("darwin", func(ctx context.Context, input string) (string, error) {
		var args struct {
			Script   string `json:"script"`
			Language string `json:"language"`
		}
		if err := json.Unmarshal([]byte(input), &args); err != nil {
			return "", fmt.Errorf("shell.script[darwin]: invalid input: %w", err)
		}
		if args.Script == "" {
			return "", fmt.Errorf("shell.script[darwin]: script is required")
		}
		return asExec.Execute(ctx, args.Script, args.Language)
	})
	osDispatch.SetHandler("windows", func(ctx context.Context, input string) (string, error) {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(input), &args); err != nil {
			return "", fmt.Errorf("shell.script[windows]: invalid input: %w", err)
		}
		if args.Command == "" {
			return "", fmt.Errorf("shell.script[windows]: command is required")
		}
		return psExec.Execute(ctx, args.Command)
	})
	osDispatch.SetHandler("linux", func(ctx context.Context, input string) (string, error) {
		var args struct {
			Bus        string        `json:"bus"`
			Service    string        `json:"service"`
			ObjectPath string        `json:"object_path"`
			Interface  string        `json:"interface"`
			Method     string        `json:"method"`
			Args       []interface{} `json:"args"`
		}
		if err := json.Unmarshal([]byte(input), &args); err != nil {
			return "", fmt.Errorf("shell.script[linux]: invalid input: %w", err)
		}
		return dbusClient.Call(ctx, dbuspkg.BusKind(args.Bus), args.Service, args.ObjectPath, args.Interface, args.Method, args.Args)
	})
	dispatchpkg.RegisterDispatchTool(ftReg, osDispatch)

	// Clipboard tools — cross-platform OS clipboard read/write via native
	// command-line backends (pbcopy/pbpaste on macOS, wl-clipboard or xclip
	// on Linux, clip + Get-Clipboard on Windows). No CGO required.
	//
	// Read and write have separate capabilities (tool.clipboard.read,
	// tool.clipboard.write) so callers can grant write-only or read-only.
	// Neither is in the default capability set — explicit grant required.
	clippkg.RegisterTools(ftReg, clippkg.NewClient())

	// cost.forecast — pre-action USD/token estimate for a planned model
	// call. Pairs with the existing cost.status tool (which reports
	// historical usage) to give the supervisor agent a "this mission will
	// cost roughly $X — approve?" gate before kicking off expensive work.
	// Capability is empty (advisory only — always available).
	costpkg.RegisterForecastTool(ftReg, costpkg.NewForecaster())

	// wait.until — block until a condition is true (or timeout). Five
	// condition types: file_exists, process_running, port_open, http_ready,
	// duration. Removes UI/IO race conditions by giving agents a primitive
	// to gate on real-world readiness instead of guessing with sleep.
	// Capability: tool.wait.until. Probes are read-only (Stat / TCP connect /
	// HTTP request / process listing); no mutation.
	waitpkg.RegisterTool(ftReg, waitpkg.NewWaiter())

	// permissions.check — OS-level permission diagnostic. On macOS probes
	// Automation (System Events, Finder) and Screen Recording, returning
	// a structured report with deep-links to the relevant System Settings
	// panel for any denied entries. Other OSes get a "not applicable"
	// report. No capability — read-only diagnostic.
	permpkg.RegisterTool(ftReg)

	// ElevenLabs TTS (only when API key is configured).
	elKeyCred, _ := v.Get(ctx, "elevenlabs_api_key")
	elVoiceCred, _ := v.Get(ctx, "elevenlabs_voice_id")
	if len(elKeyCred.Value) > 0 {
		voiceID := string(elVoiceCred.Value)
		if voiceID == "" {
			voiceID = "21m00Tcm4TlvDq8ikWAM" // Default Rachel voice.
		}
		app.ElevenLabs = elevenlabs.NewClient(string(elKeyCred.Value), voiceID)
		elevenlabs.RegisterElevenLabsTools(ftReg, app.ElevenLabs)
		log.Println("elevenlabs: TTS tools registered")
	}

	// Deepgram STT (only when API key is configured).
	dgKeyCred, _ := v.Get(ctx, "deepgram_api_key")
	if len(dgKeyCred.Value) > 0 {
		app.Deepgram = deepgram.NewClient(string(dgKeyCred.Value))
		deepgram.RegisterDeepgramTools(ftReg, app.Deepgram)
		log.Println("deepgram: STT tools registered")
	}

	// Piper local TTS (only when binary path set via vault or environment).
	piperBinary := os.Getenv("AIBUTLER_PIPER_BINARY")
	piperModel := os.Getenv("AIBUTLER_PIPER_MODEL")
	if piperBinary == "" {
		if piperKeyCred, err := v.Get(ctx, "piper_binary_path"); err == nil {
			piperBinary = string(piperKeyCred.Value)
		}
	}
	if piperBinary != "" {
		app.Piper = piper.NewExecutor(piperBinary, piperModel)
		if app.Piper.Available() {
			piper.RegisterPiperTool(ftReg, app.Piper)
			log.Println("piper: local TTS tools registered")
		}
	}

	// Spreadsheet and archive media tools (always available).
	spreadsheet.RegisterSpreadsheetTools(ftReg)
	archive.RegisterArchiveTools(ftReg)
	mediacontact.RegisterContactTools(ftReg)

	// LINE channel (only when credentials configured).
	lineTokenCred, _ := v.Get(ctx, "line_access_token")
	lineSecretCred, _ := v.Get(ctx, "line_channel_secret")
	if len(lineTokenCred.Value) > 0 && len(lineSecretCred.Value) > 0 {
		app.LINEClient = linepkg.NewClient(string(lineSecretCred.Value), string(lineTokenCred.Value))
		linepkg.RegisterLINETools(ftReg, app.LINEClient)
		log.Println("line: tools registered")
	}

	// IRC channel (only when server configured).
	ircServer := os.Getenv("AIBUTLER_IRC_SERVER")
	ircNick := os.Getenv("AIBUTLER_IRC_NICK")
	if ircServer != "" && ircNick != "" {
		app.IRCClient = ircpkg.NewClient(ircServer, ircNick)
		ircpkg.RegisterIRCTools(ftReg, app.IRCClient)
		log.Println("irc: tools registered")
	}

	// Microsoft Teams channel (only when credentials configured).
	teamsAppIDCred, _ := v.Get(ctx, "teams_app_id")
	teamsAppPwCred, _ := v.Get(ctx, "teams_app_password")
	if len(teamsAppIDCred.Value) > 0 && len(teamsAppPwCred.Value) > 0 {
		app.TeamsClient = teamspkg.NewClient(string(teamsAppIDCred.Value), string(teamsAppPwCred.Value))
		teamspkg.RegisterTeamsTools(ftReg, app.TeamsClient)
		log.Println("teams: tools registered")
	}

	// Google Chat channel (only when service account key configured).
	gchatKeyCred, _ := v.Get(ctx, "gchat_service_account_key")
	if len(gchatKeyCred.Value) > 0 {
		app.GChatClient = gchatpkg.NewClient(gchatKeyCred.Value)
		gchatpkg.RegisterGChatTools(ftReg, app.GChatClient)
		log.Println("gchat: tools registered")
	}

	// Custom Webhook channel (from config).
	if whCfg, ok := cfg.Configurations.Channels["webhook"]; ok && whCfg.Enabled {
		whSecretCred, _ := v.Get(ctx, "webhook_auth_secret")
		app.WebhookAdapter = webhookpkg.New(webhookpkg.Config{
			OutboundURL: os.Getenv("AIBUTLER_WEBHOOK_OUTBOUND_URL"),
			AuthType:    os.Getenv("AIBUTLER_WEBHOOK_AUTH_TYPE"),
			AuthSecret:  string(whSecretCred.Value),
		})
		webhookpkg.RegisterWebhookTools(ftReg, app.WebhookAdapter)
		log.Println("webhook: tools registered")
	}

	// Nostr channel (only when private key + relay URL configured).
	nostrKeyCred, _ := v.Get(ctx, "nostr_private_key")
	nostrRelayURL := os.Getenv("AIBUTLER_NOSTR_RELAY_URL")
	if nostrRelayURL == "" {
		if relayCred, err := v.Get(ctx, "nostr_relay_url"); err == nil && len(relayCred.Value) > 0 {
			nostrRelayURL = string(relayCred.Value)
		}
	}
	if len(nostrKeyCred.Value) > 0 && nostrRelayURL != "" {
		app.NostrClient = nostrpkg.NewClient(nostrRelayURL, string(nostrKeyCred.Value))
		nostrpkg.RegisterNostrTools(ftReg, app.NostrClient)
		log.Println("nostr: tools registered")
	}

	// Interactive browser tools (always available).
	app.InteractiveBrowser = browser.NewInteractiveClient()
	browser.RegisterInteractiveBrowserTools(ftReg, app.InteractiveBrowser)

	// Transaction engine (spending limits default to zero — no spend without config).
	txLimits := txpkg.SpendingLimits{
		PerTransaction: cfg.Options.Transaction.PerTransactionLimit,
		DailyTotal:     cfg.Options.Transaction.DailyLimit,
	}
	app.TransactionEngine = txpkg.New(database.Conn(), txLimits)
	txpkg.RegisterTransactionTools(ftReg, app.TransactionEngine)

	// AI design tools (Canva, Figma — vault key gated).
	canvaKeyCred, _ := v.Get(ctx, "canva_api_key")
	figmaKeyCred, _ := v.Get(ctx, "figma_api_key")
	canvaProv := designpkg.NewCanva(string(canvaKeyCred.Value))
	figmaProv := designpkg.NewFigma(string(figmaKeyCred.Value))
	designpkg.RegisterDesignTools(ftReg, canvaProv, figmaProv)

	// AI 3D generation tools (Meshy, Tripo, Luma — vault key gated).
	meshyKeyCred, _ := v.Get(ctx, "meshy_api_key")
	tripoKeyCred, _ := v.Get(ctx, "tripo_api_key")
	lumaKeyCred, _ := v.Get(ctx, "luma_api_key")
	meshyProv := threedpkg.NewMeshy(string(meshyKeyCred.Value))
	tripoProv := threedpkg.NewTripo(string(tripoKeyCred.Value))
	lumaProv := threedpkg.NewLuma(string(lumaKeyCred.Value))
	threedpkg.RegisterThreeDTools(ftReg, meshyProv, tripoProv, lumaProv)

	// AI workflow and batch tools.
	toolCaller := &toolCallerAdapter{reg: app.Tools}
	workflowpkg.RegisterWorkflowTools(ftReg, toolCaller)
	batchpkg.RegisterBatchTools(ftReg, toolCaller)

	// Video processing tools (always available; ffmpeg checked at runtime).
	app.VideoProcessor = videopkg.NewProcessor()
	videopkg.RegisterVideoTools(ftReg, app.VideoProcessor)

	// Voice enhancements (TUI mode + wake word detector).
	app.TUIVoice = voice.NewTUIMode(app.Voice)
	app.WakeWord = voice.NewWakeWordDetector("hey butler")

	// 6k. Create tool dispatcher (after ALL tools registered) with audit logging.
	app.Dispatcher = tool.NewDispatcher(app.Tools, app.Engine, auditor)

	// 6l. Hook engine.
	if len(cfg.Configurations.Hooks.PreToolUse) > 0 || len(cfg.Configurations.Hooks.PostToolUse) > 0 {
		preHooks := make([]hook.HookConfig, len(cfg.Configurations.Hooks.PreToolUse))
		for i, h := range cfg.Configurations.Hooks.PreToolUse {
			preHooks[i] = hook.HookConfig{Command: h.Command, Tools: h.Tools}
		}
		postHooks := make([]hook.HookConfig, len(cfg.Configurations.Hooks.PostToolUse))
		for i, h := range cfg.Configurations.Hooks.PostToolUse {
			postHooks[i] = hook.HookConfig{Command: h.Command, Tools: h.Tools}
		}
		app.HookEngine = hook.New(preHooks, postHooks)
		app.Dispatcher.SetHookEngine(hook.NewHookRunnerAdapter(app.HookEngine))
	}

	// 6m. Git tools.
	cwd, _ := os.Getwd()
	app.GitClient = gitpkg.NewClient(cwd)
	gitpkg.RegisterGitTools(ftReg, app.GitClient)

	// 6n. Permission prompter.
	if cfg.Configurations.Permissions.Mode != "" || len(cfg.Configurations.Permissions.Rules) > 0 {
		var rules []capability.PermissionRule
		for _, r := range cfg.Configurations.Permissions.Rules {
			rules = append(rules, capability.PermissionRule{ToolPattern: r.Pattern, Action: r.Action})
		}
		app.Prompter = capability.NewInteractivePrompter(os.Stdin, os.Stdout, rules)
		app.Prompter.SetMode(capability.ParsePermissionMode(cfg.Configurations.Permissions.Mode))
	}

	// 7. Session manager.
	app.Sessions = session.NewManager(database.Conn(), cfg)

	// 8. Prompt composer + cost tracker.
	app.Tracker = prompt.NewTracker(database.Conn(), cfg)
	app.Composer = prompt.NewComposer(cfg, app.Sessions, app.Tracker, database.Conn())

	// 8a. Wire instruction provider: CompositeProvider (file + DB).
	dbInstrAdapter := &instructionAdapter{store: instrStore}
	compositeInstr := instruction.NewCompositeProvider(&compositeInstrDBAdapter{inner: dbInstrAdapter}, cwd)
	app.Composer.SetInstructionStore(&compositeInstrPromptAdapter{composite: compositeInstr})

	app.Composer.SetMemoryStore(app.MemStore)
	app.Composer.SetEntityStore(app.EntityStore)

	// 8b. Wire git context into prompt composer.
	app.Composer.SetGitContext(func(ctx context.Context) string {
		return app.GitClient.GitContext(ctx)
	})

	// 9. Scheduler (if enabled).
	if cfg.Configurations.Schedule.Enabled {
		store := schedule.NewStore(database.Conn())
		app.Scheduler = schedule.NewScheduler(store, nil, cfg.Options.Schedule.TickInterval)
	}

	// 10. Telemetry collector (no-op when disabled).
	app.Telemetry = telemetry.NewCollector(cfg.Settings.TelemetryEnabled)
	app.Dispatcher.SetTelemetry(app.Telemetry)
	app.Sessions.SetRecorder(app.Telemetry)

	// 11. Wire offline guard into proxy (guard created early in step 1b).
	httpExec.SetOfflineGuard(app.Offline)

	// 12. Health data encryptor (uses vault-stored key).
	healthKeyCred, _ := v.Get(ctx, "health_encryption_key")
	if len(healthKeyCred.Value) == 32 {
		enc, err := health.NewEncryptor(healthKeyCred.Value)
		if err == nil {
			tool.WireHealthEncryptor(app.Tools, enc)
			log.Println("health: encryption enabled")
		}
	}

	// 13. Voice pipeline (STT + TTS + normalizer).
	var sttProvider voice.STTProvider
	var ttsProvider voice.TTSProvider
	sttProvider = &voice.StubSTTProvider{}
	ttsProvider = &voice.StubTTSProvider{}

	if cfg.Configurations.Voice.STTProvider == "whisper" {
		if cred, err := v.Get(ctx, "openai_api_key"); err == nil && len(cred.Value) > 0 {
			sttProvider = voice.NewWhisperProvider(string(cred.Value), guardedClient)
			log.Println("voice: Whisper STT provider configured")
		}
	}
	if cfg.Configurations.Voice.TTSProvider == "edge" {
		ttsProvider = voice.NewEdgeTTSProvider(guardedClient, "")
		log.Println("voice: Edge TTS provider configured")
	}

	voiceMode := "text"
	if ch, ok := cfg.Configurations.Channels["webchat"]; ok && ch != nil && ch.VoiceResponse != "" {
		voiceMode = ch.VoiceResponse
	}
	app.Voice = voice.NewPipeline(sttProvider, ttsProvider, voice.NewNormalizer(), voiceMode)

	// 13b. Register voice tools (voice.transcribe, voice.speak) — available to agents.
	voice.RegisterVoiceTools(app.Tools, app.Voice)

	// 14. Platform, UX & Swarm Dashboard.

	// 14a. Dashboard (agent card, registry browser, swarm dashboard).
	dashReg := &dashboardRegistryAdapter{reg: app.AgentRegistry}
	dashSwarm := &dashboardSwarmAdapter{db: database.Conn()}
	app.Dashboard = dashboard.New(database.Conn(), dashReg, dashSwarm)

	// 14b. LAN discovery.
	app.LANDiscovery = lan.New(cfg.Configurations.Web.Port, cfg.Settings.PersonaName)

	// 14c. Setup wizard.
	app.SetupWizard = setup.New(app.configPath, cfg)

	// 14d. Auto-updater.
	app.Updater = updater.New(Version, "", 24*time.Hour)

	// 14e. Coding tools.
	app.CodingRunner = coding.NewRunner(dataDir)
	coding.RegisterCodingTools(ftReg, app.CodingRunner)

	// W2: RBAC engine (created before dashboard mount so middleware can wrap it).
	app.RBAC = rbacpkg.New(database.Conn())

	// 14f. Mount dashboard and setup wizard on webchat HTTP server.
	if app.webChatAdapter != nil {
		// E1: Wrap dashboard handler with RBAC enforcement middleware.
		dashHandler := app.Dashboard.Handler()
		if app.RBAC != nil {
			dashHandler = app.RBAC.Middleware(dashHandler)
		} else if app.Authenticator != nil {
			// Single-user mode: require password auth for dashboard when
			// the webchat binds to a non-localhost address (LAN/internet facing).
			bind := cfg.Configurations.Web.BindAddress
			if bind != "localhost" && bind != "127.0.0.1" && bind != "::1" {
				dashHandler = app.Authenticator.Middleware(dashHandler)
			}
		}
		app.webChatAdapter.MountHandler("/api/dashboard/", dashHandler)
		app.webChatAdapter.MountHandler("/api/setup/", app.SetupWizard.Handler())

		// Mount PWA handlers.
		// iOS Safari fetches /apple-touch-icon.png at the site root — redirect
		// it to the static FS where the actual PNG lives.
		app.webChatAdapter.MountHandler("/manifest.json", pwapkg.ManifestHandler())
		app.webChatAdapter.MountHandler("/sw.js", pwapkg.ServiceWorkerHandler())
		app.webChatAdapter.MountHandler("/apple-touch-icon.png", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/static/apple-touch-icon.png", http.StatusMovedPermanently)
		}))

		// Mount health check endpoint for Docker/K8s probes.
		app.webChatAdapter.MountHandler("/healthz", webchat.HealthHandler(app.DB.Conn()))
	}

	// 15. Web App enhancements.

	// 15a. WebAuthn/FIDO2 server (passwordless authentication).
	webauthnCfg := webauthnpkg.Config{
		RPID:     cfg.Configurations.Web.BindAddress,
		RPOrigin: fmt.Sprintf("https://%s:%d", cfg.Configurations.Web.BindAddress, cfg.Configurations.Web.Port),
		RPName:   cfg.Settings.PersonaName,
	}
	app.WebAuthnServer = webauthnpkg.New(webauthnCfg, database.Conn())

	// E8: Mount WebAuthn HTTP routes on webchat adapter.
	if app.webChatAdapter != nil && app.WebAuthnServer != nil {
		app.webChatAdapter.MountHandler("/auth/webauthn/register/begin", app.WebAuthnServer.BeginRegistrationHandler())
		app.webChatAdapter.MountHandler("/auth/webauthn/register/finish", app.WebAuthnServer.FinishRegistrationHandler())
		app.webChatAdapter.MountHandler("/auth/webauthn/login/begin", app.WebAuthnServer.BeginAuthenticationHandler())
		app.webChatAdapter.MountHandler("/auth/webauthn/login/finish", app.WebAuthnServer.FinishAuthenticationHandler())
	}

	// 15b. Security event monitor.
	app.SecurityMonitor = secmonpkg.New(database.Conn(), secmonpkg.DefaultThresholds())

	// 15c. Mount security dashboard endpoint on webchat.
	if app.webChatAdapter != nil {
		app.webChatAdapter.MountHandler("/api/dashboard/security/events", app.SecurityMonitor.Handler())
	}

	// 15d. Plugin rescanner (defense-in-depth continuous protection).
	app.PluginRescanner = rescanpkg.New(app.PluginScanner, &pluginRescanAdapter{reg: app.PluginRegistry}, 1*time.Hour)

	// W1: Response cache.
	app.ResponseCache = cachepkg.New(database.Conn(), cachepkg.Config{
		Enabled:    true,
		DefaultTTL: 5 * time.Minute,
		MaxEntries: 1000,
	})

	// W2: RBAC engine (moved earlier — created in Bootstrap before 14f mount).

	// W3: OIDC client (only if DiscoveryURL is configured via OAuth config).
	if cfg.Configurations.OAuth.RedirectURI != "" && cfg.Configurations.OAuth.Gmail.ClientID != "" {
		oidcClient, err := oidcpkg.New(oidcpkg.Config{
			DiscoveryURL: "https://accounts.google.com/.well-known/openid-configuration",
			ClientID:     cfg.Configurations.OAuth.Gmail.ClientID,
			ClientSecret: cfg.Configurations.OAuth.Gmail.ClientSecret,
			RedirectURL:  cfg.Configurations.OAuth.RedirectURI,
		})
		if err == nil {
			app.OIDCClient = oidcClient
		} else {
			log.Printf("oidc: init error: %v", err)
		}
	}

	// E2: Mount OIDC login/callback HTTP handlers on webchat.
	if app.OIDCClient != nil && app.webChatAdapter != nil {
		app.webChatAdapter.MountHandler("/auth/oidc/login", app.OIDCClient.LoginHandler())
		app.webChatAdapter.MountHandler("/auth/oidc/callback", app.OIDCClient.CallbackHandler())
	}

	// W4: Compliance logger.
	app.ComplianceLogger = compliancepkg.New(database.Conn())

	// E3: Wire compliance logger into tool dispatcher for audit logging.
	app.Dispatcher.SetComplianceLogger(app.ComplianceLogger)

	// E4: Wire response cache into tool dispatcher.
	app.Dispatcher.SetCache(app.ResponseCache)

	// W5: MCP Server v2.
	mcpV2Mem := &mcpV2MemoryAdapter{searcher: hybridSearcher}
	mcpV2Sched := &mcpV2ScheduleAdapter{store: schedStore}
	mcpV2Chan := &mcpV2ChannelAdapter{reg: app.Channels}
	mcpV2Agents := &mcpV2AgentAdapter{reg: app.AgentRegistry}
	app.MCPServerV2 = mcpserverv2.New(mcpV2Mem, mcpV2Sched, mcpV2Chan, mcpV2Agents)
	// Stash the agent adapter so cmd_run.go can inject the factory runner
	// once the factory is built. Without this wiring
	// `butler.agent.delegate` MCP calls would always fail with
	// "agent runner not yet configured".
	app.MCPAgentAdapter = mcpV2Agents

	// E6: Mount MCP v2 HTTP handler on webchat adapter for HTTP transport.
	if app.webChatAdapter != nil && app.MCPServerV2 != nil {
		app.webChatAdapter.MountHandler("/mcp/v2", app.MCPServerV2.HandleHTTP())
	}

	// W6: Subprocess bridges.
	if len(cfg.Configurations.Bridges.Bridges) > 0 {
		app.SubprocessBridges = make(map[string]*subprocpkg.Adapter)
		for name, bcfg := range cfg.Configurations.Bridges.Bridges {
			adapter := subprocpkg.New(subprocpkg.Config{
				Command:      bcfg.Command,
				Args:         bcfg.Args,
				Timeout:      bcfg.Timeout,
				Capabilities: bcfg.Capabilities,
			})
			app.SubprocessBridges[name] = adapter
			log.Printf("bridge: %s registered", name)
		}
	}

	// W7: QR code handler on webchat.
	if app.webChatAdapter != nil {
		app.webChatAdapter.MountHandler("/qr", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			webCfg := cfg.Configurations.Web
			qrURL := fmt.Sprintf("http://%s:%d", webCfg.BindAddress, webCfg.Port)
			data, err := qrpkg.GenerateQR(qrURL)
			if err != nil {
				log.Printf("qr: generate failed: %v", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Write(data)
		}))
	}

	// W8: Plugin marketplace.
	app.Marketplace = marketplacepkg.New(database.Conn())

	// E5: Register marketplace tools in the tool registry.
	marketplacepkg.RegisterMarketplaceTools(ftReg, app.Marketplace)

	// U3: Batch executor.
	app.BatchExecutor = model.NewBatchExecutor(model.DefaultBatchConfig())

	return app, nil
}

// Shutdown cleans up all resources.
func (a *App) Shutdown() {
	if a.PluginRescanner != nil {
		a.PluginRescanner.Stop()
	}
	if a.PluginRegistry != nil {
		a.PluginRegistry.DisableAll(context.Background())
	}
	if a.Lifecycle != nil {
		a.Lifecycle.Stop()
	}
	if a.BatchExecutor != nil {
		a.BatchExecutor.Stop()
	}
	if a.MCPClient != nil {
		for _, name := range a.MCPClient.Servers() {
			a.MCPClient.Disconnect(name)
		}
	}
	if a.Scheduler != nil {
		a.Scheduler.Stop()
	}
	if a.DB != nil {
		a.DB.Close()
	}
}

// SaveConfig writes the current config to disk as YAML.
func (a *App) SaveConfig() error {
	data, err := yaml.Marshal(a.Config)
	if err != nil {
		return err
	}
	return os.WriteFile(a.configPath, data, 0600)
}

// instructionAdapter bridges instruction.Store to prompt.InstructionProvider
// without creating a circular import.
type instructionAdapter struct {
	store *instruction.Store
}

func (a *instructionAdapter) ActiveForPrompt(ctx context.Context, channel, sessionID string) ([]prompt.InstructionEntry, error) {
	instructions, err := a.store.ActiveForPrompt(ctx, channel, sessionID)
	if err != nil {
		return nil, err
	}
	entries := make([]prompt.InstructionEntry, len(instructions))
	for i, inst := range instructions {
		entries[i] = prompt.InstructionEntry{
			Content:  inst.Content,
			Category: inst.Category,
			Priority: inst.Priority,
		}
	}
	return entries, nil
}

func (a *instructionAdapter) Count(ctx context.Context) (int, error) {
	return a.store.Count(ctx)
}

// toolRegAdapter wraps tool.Registry to satisfy the registry.ToolRegistry interface.
type toolRegAdapter struct {
	reg *tool.Registry
}

func (a *toolRegAdapter) Register(t registry.ToolLike) {
	a.reg.Register(&tool.FuncTool{
		ToolName:   t.Name(),
		ToolDesc:   t.Description(),
		ToolSchema: t.Schema(),
		ToolCap:    t.Capability(),
		Exec:       t.Execute,
	})
}

func (a *toolRegAdapter) UnregisterPrefix(prefix string) {
	a.reg.UnregisterPrefix(prefix)
}

// toolCallerAdapter wraps tool.Registry to satisfy host.ToolCaller for plugin host functions.
type toolCallerAdapter struct {
	reg *tool.Registry
}

func (a *toolCallerAdapter) CallTool(ctx context.Context, name, input string) (string, error) {
	t, ok := a.reg.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return t.Execute(ctx, input)
}

// vaultAdapter wraps vault.Vault to satisfy host.VaultGetter (returns []byte, not Credential).
type vaultAdapter struct {
	v vault.Vault
}

func (a *vaultAdapter) Get(ctx context.Context, key string) ([]byte, error) {
	cred, err := a.v.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return cred.Value, nil
}

// toolProviderAdapter wraps tool.Registry to satisfy mcpserver.ToolProvider.
type toolProviderAdapter struct {
	reg *tool.Registry
}

func (a *toolProviderAdapter) All() []mcpserver.ToolEntry {
	tools := a.reg.All()
	entries := make([]mcpserver.ToolEntry, len(tools))
	for i, t := range tools {
		entries[i] = mcpserver.ToolEntry{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
			Capability:  t.Capability(),
			Executor:    t,
		}
	}
	return entries
}

// mergeAllowlists returns a new slice with `defaults` first, then `user` entries.
// No deduplication — each executor's allowlist matcher walks the slice linearly
// and treats duplicates as harmless. Returning a fresh slice protects callers
// from accidentally mutating either input.
func mergeAllowlists(defaults, user []string) []string {
	merged := make([]string, 0, len(defaults)+len(user))
	merged = append(merged, defaults...)
	merged = append(merged, user...)
	return merged
}

// funcToolRegistry adapts *tool.Registry to the narrow toolRegistry interface used by
// Channel/media/voice packages (channel/whatsapp, browser, shell/powershell, voice/*, media/*).
// This adapter bridges the flat Register(name,desc,schema,cap,exec) API expected by those
// packages with the tool.FuncTool approach used by the rest of the codebase.
type funcToolRegistry struct {
	reg *tool.Registry
}

func (a *funcToolRegistry) Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error)) {
	a.reg.Register(&tool.FuncTool{
		ToolName:   name,
		ToolDesc:   description,
		ToolSchema: schema,
		ToolCap:    capability,
		Exec:       exec,
	})
}

// a2aRunnerStub is an intentional placeholder task runner for A2A delegation.
// Real wiring requires the agent Factory, which depends on the full bootstrap
// context (model adapter, prompt composer, tool dispatcher, session manager).
// These aren't available at handler-creation time in the current bootstrap order.
// TODO(v0.2+): Replace with a Factory-backed runner once the bootstrap is
// refactored to support late-binding of the task runner.
type a2aRunnerStub struct{}

func (r *a2aRunnerStub) RunTask(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("A2A delegation is not configured: agent Factory is not yet wired to the A2A handler")
}

// dashboardRegistryAdapter adapts *a2aregistry.Registry to dashboard.RegistryBrowser.
type dashboardRegistryAdapter struct {
	reg *a2aregistry.Registry
}

func (a *dashboardRegistryAdapter) DiscoverAll(ctx context.Context) ([]dashboard.AgentRecord, error) {
	records, err := a.reg.DiscoverAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dashboard.AgentRecord, len(records))
	for i, r := range records {
		out[i] = dashboard.AgentRecord{
			Name:         r.Name,
			URL:          r.URL,
			Capabilities: r.Capabilities,
			LastSeen:     r.LastSeen,
			SuccessCount: r.SuccessCount,
			FailureCount: r.FailureCount,
		}
	}
	return out, nil
}

func (a *dashboardRegistryAdapter) Register(ctx context.Context, name, url string, capabilities []string, healthCheckURL string) error {
	return a.reg.Register(ctx, name, url, capabilities, healthCheckURL)
}

func (a *dashboardRegistryAdapter) Deregister(ctx context.Context, name string) error {
	return a.reg.Deregister(ctx, name)
}

// dashboardSwarmAdapter adapts the swarm tables to dashboard.SwarmStore.
type dashboardSwarmAdapter struct {
	db *sql.DB
}

func (a *dashboardSwarmAdapter) ListRuns(ctx context.Context, limit int) ([]dashboard.SwarmRun, error) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT id, run_id, goal, plan_json, status, total_cost_usd, COALESCE(trace_id,''), started_at, COALESCE(completed_at,'')
		 FROM swarm_runs ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []dashboard.SwarmRun
	for rows.Next() {
		var r dashboard.SwarmRun
		if err := rows.Scan(&r.ID, &r.RunID, &r.Goal, &r.PlanJSON, &r.Status,
			&r.TotalCost, &r.TraceID, &r.StartedAt, &r.CompletedAt); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (a *dashboardSwarmAdapter) GetRun(ctx context.Context, runID string) (*dashboard.SwarmRun, error) {
	var r dashboard.SwarmRun
	err := a.db.QueryRowContext(ctx,
		`SELECT id, run_id, goal, plan_json, status, total_cost_usd, COALESCE(trace_id,''), started_at, COALESCE(completed_at,'')
		 FROM swarm_runs WHERE run_id = ?`, runID).Scan(
		&r.ID, &r.RunID, &r.Goal, &r.PlanJSON, &r.Status,
		&r.TotalCost, &r.TraceID, &r.StartedAt, &r.CompletedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// compositeInstrDBAdapter bridges instructionAdapter to instruction.InstructionProvider.
type compositeInstrDBAdapter struct {
	inner *instructionAdapter
}

func (a *compositeInstrDBAdapter) ActiveForPrompt(ctx context.Context, channel, sessionID string) ([]instruction.InstructionEntry, error) {
	entries, err := a.inner.ActiveForPrompt(ctx, channel, sessionID)
	if err != nil {
		return nil, err
	}
	result := make([]instruction.InstructionEntry, len(entries))
	for i, e := range entries {
		result[i] = instruction.InstructionEntry{
			Content:  e.Content,
			Category: e.Category,
			Priority: e.Priority,
		}
	}
	return result, nil
}

func (a *compositeInstrDBAdapter) Count(ctx context.Context) (int, error) {
	return a.inner.Count(ctx)
}

// compositeInstrPromptAdapter bridges instruction.CompositeProvider to prompt.InstructionProvider.
type compositeInstrPromptAdapter struct {
	composite *instruction.CompositeProvider
}

func (a *compositeInstrPromptAdapter) ActiveForPrompt(ctx context.Context, channel, sessionID string) ([]prompt.InstructionEntry, error) {
	entries, err := a.composite.ActiveForPrompt(ctx, channel, sessionID)
	if err != nil {
		return nil, err
	}
	result := make([]prompt.InstructionEntry, len(entries))
	for i, e := range entries {
		result[i] = prompt.InstructionEntry{
			Content:  e.Content,
			Category: e.Category,
			Priority: e.Priority,
		}
	}
	return result, nil
}

func (a *compositeInstrPromptAdapter) Count(ctx context.Context) (int, error) {
	return a.composite.Count(ctx)
}

func (a *dashboardSwarmAdapter) GetTraces(ctx context.Context, runID string) ([]dashboard.TraceSpan, error) {
	// Look up trace_id from run_id first.
	var traceID string
	err := a.db.QueryRowContext(ctx, `SELECT COALESCE(trace_id,'') FROM swarm_runs WHERE run_id = ?`, runID).Scan(&traceID)
	if err != nil || traceID == "" {
		return nil, err
	}

	rows, err := a.db.QueryContext(ctx,
		`SELECT id, trace_id, span_id, COALESCE(parent_span_id,''), agent_id, COALESCE(peer_url,''),
		        COALESCE(task_summary,''), status, cost_usd, started_at, COALESCE(completed_at,'')
		 FROM swarm_trace WHERE trace_id = ? ORDER BY started_at`, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var spans []dashboard.TraceSpan
	for rows.Next() {
		var s dashboard.TraceSpan
		if err := rows.Scan(&s.ID, &s.TraceID, &s.SpanID, &s.ParentSpanID,
			&s.AgentID, &s.PeerURL, &s.TaskSummary, &s.Status, &s.CostUSD,
			&s.StartedAt, &s.CompletedAt); err != nil {
			return nil, err
		}
		spans = append(spans, s)
	}
	return spans, rows.Err()
}

// pluginRescanAdapter adapts *registry.Registry to rescan.PluginLister.
type pluginRescanAdapter struct {
	reg *registry.Registry
}

// mcpV2MemoryAdapter adapts *hybrid.Searcher to mcpserverv2.MemorySearcher.
type mcpV2MemoryAdapter struct {
	searcher *hybrid.Searcher
}

func (a *mcpV2MemoryAdapter) Search(ctx context.Context, query string) ([]mcpserverv2.SearchResult, error) {
	results, err := a.searcher.Search(ctx, query, 20)
	if err != nil {
		return nil, err
	}
	out := make([]mcpserverv2.SearchResult, len(results))
	for i, r := range results {
		out[i] = mcpserverv2.SearchResult{Content: r.Content, Score: r.Score}
	}
	return out, nil
}

// mcpV2ScheduleAdapter adapts *schedule.Store to mcpserverv2.ScheduleOps
// (list + create).
type mcpV2ScheduleAdapter struct {
	store *schedule.Store
}

func (a *mcpV2ScheduleAdapter) ListTasks(ctx context.Context) ([]mcpserverv2.ScheduleEntry, error) {
	tasks, err := a.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mcpserverv2.ScheduleEntry, len(tasks))
	for i, t := range tasks {
		out[i] = mcpserverv2.ScheduleEntry{ID: t.ID, Description: t.Name, CronExpr: t.CronExpr, Enabled: t.Enabled}
	}
	return out, nil
}

// CreateTask satisfies mcpserverv2.ScheduleOps. Generates a stable ID
// from the current nanosecond timestamp (same pattern used by the
// scheduler's tool surface) and persists the schedule as enabled.
func (a *mcpV2ScheduleAdapter) CreateTask(ctx context.Context, req mcpserverv2.CreateScheduleRequest) (string, error) {
	if a.store == nil {
		return "", fmt.Errorf("schedule store not available")
	}
	id := fmt.Sprintf("sched_%d", time.Now().UnixNano())
	sched := &schedule.Schedule{
		ID:        id,
		Name:      req.Description,
		CronExpr:  req.CronExpr,
		Task:      req.Task,
		Channel:   req.Channel,
		AccountID: req.AccountID,
		Enabled:   true,
	}
	if err := a.store.Create(ctx, sched); err != nil {
		return "", err
	}
	return id, nil
}

// mcpV2ChannelAdapter adapts *channel.Registry to mcpserverv2.ChannelOps
// (list + send).
type mcpV2ChannelAdapter struct {
	reg *channel.Registry
}

func (a *mcpV2ChannelAdapter) ListChannels() []string {
	all := a.reg.All()
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	return names
}

// SendMessage satisfies mcpserverv2.ChannelOps. Looks up the named
// channel and forwards a text-only OutgoingMessage. Unknown channel
// returns a precise error so the MCP caller sees which name was
// missing rather than a generic "send failed".
func (a *mcpV2ChannelAdapter) SendMessage(ctx context.Context, name, accountID, text string) error {
	if a.reg == nil {
		return fmt.Errorf("channel registry not available")
	}
	ch, ok := a.reg.Get(name)
	if !ok {
		return fmt.Errorf("channel %q not registered", name)
	}
	return ch.Send(ctx, accountID, channel.OutgoingMessage{Text: text})
}

// mcpV2AgentAdapter adapts *a2aregistry.Registry to mcpserverv2.AgentOps.
// Delegation is served by a pluggable runner set after construction
// (because the agent factory is built later in cmd_run.go, after app
// bootstrap).
type mcpV2AgentAdapter struct {
	reg    *a2aregistry.Registry
	mu     sync.Mutex
	runner mcpV2AgentRunner
}

// mcpV2AgentRunner is the narrow dependency DelegateTask needs:
// "run this task as an agent, return the final string answer."
type mcpV2AgentRunner interface {
	Run(ctx context.Context, sessionID, task, channel string) (*agent.Result, error)
}

// SetRunner wires the agent factory after it's constructed. Safe to call
// from cmd_run.go once the factory exists.
func (a *mcpV2AgentAdapter) SetRunner(runner mcpV2AgentRunner) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runner = runner
}

func (a *mcpV2AgentAdapter) ListAgents(ctx context.Context) ([]mcpserverv2.AgentEntry, error) {
	records, err := a.reg.DiscoverAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mcpserverv2.AgentEntry, len(records))
	for i, r := range records {
		out[i] = mcpserverv2.AgentEntry{Name: r.Name, URL: r.URL, Capabilities: r.Capabilities}
	}
	return out, nil
}

// DelegateTask satisfies mcpserverv2.AgentOps. Routes the task through
// the wired agent factory and returns the final output text.
// The DelegateRequest.Agent field is accepted for forward-compat with
// A2A peer delegation but not yet routed — in v0.1 this always runs on
// the local agent factory. A2A peer forwarding is tracked as a follow-up.
func (a *mcpV2AgentAdapter) DelegateTask(ctx context.Context, req mcpserverv2.DelegateRequest) (string, error) {
	a.mu.Lock()
	runner := a.runner
	a.mu.Unlock()
	if runner == nil {
		return "", fmt.Errorf("agent runner not yet configured (bootstrap still in progress?)")
	}
	sessionID := fmt.Sprintf("mcp-delegate-%d", time.Now().UnixNano())
	result, err := runner.Run(ctx, sessionID, req.Task, "mcp")
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("agent returned nil result")
	}
	return result.Output, nil
}

func (a *pluginRescanAdapter) ListManifests(ctx context.Context) ([]scanner.Manifest, error) {
	if a.reg == nil {
		return nil, nil
	}
	plugins, err := a.reg.List(ctx)
	if err != nil {
		return nil, err
	}
	manifests := make([]scanner.Manifest, len(plugins))
	for i, p := range plugins {
		manifests[i] = scanner.Manifest{
			Name:         p.Name,
			Version:      p.Version,
			Capabilities: p.Capabilities,
		}
	}
	return manifests, nil
}
