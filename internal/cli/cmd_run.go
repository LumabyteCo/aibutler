package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"net/http"

	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/agent/bus"
	"github.com/LumabyteCo/aibutler/internal/agent/router"
	"github.com/LumabyteCo/aibutler/internal/agent/specialist"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/i18n"
	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	swarmws "github.com/LumabyteCo/aibutler/internal/memory/swarm"
	"github.com/LumabyteCo/aibutler/internal/memory/vector"
	"github.com/LumabyteCo/aibutler/internal/model"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	a2aclient "github.com/LumabyteCo/aibutler/internal/protocol/a2a/client"
	a2aregistry "github.com/LumabyteCo/aibutler/internal/protocol/a2a/registry"
	subprocpkg "github.com/LumabyteCo/aibutler/internal/bridge/subprocess"
	"github.com/LumabyteCo/aibutler/internal/ratelimit"
	"github.com/LumabyteCo/aibutler/internal/stopphrase"
	"github.com/LumabyteCo/aibutler/internal/swarm"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/internal/streaming"
)

// CmdRun starts the interactive mode:
// 1. Log startup message
// 2. Start scheduler (if enabled)
// 3. Resolve AI pipeline or fall back to echo
// 4. Start all enabled channels
// 5. Block until SIGINT/SIGTERM
// 6. Graceful shutdown
func CmdRun(app *App, _ []string, w io.Writer) error {
	fmt.Fprintf(w, "AI Butler v%s starting...\n", Version)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Resolve message handler: real AI pipeline or echo fallback.
	handler := resolveHandler(app, w)

	// Start A2A HTTP server if enabled (runner was wired in resolveHandler).
	var a2aSrv *http.Server
	if app.Config.Configurations.A2A.Enabled && app.A2AHandler != nil {
		// Apply swarm safety config to handler.
		app.A2AHandler.SetMaxDepth(app.Config.Configurations.Swarm.MaxDepth)

		port := app.Config.Configurations.A2A.Port
		if port == 0 {
			port = 8081
		}
		// Wrap A2A handler with rate limiter (100 requests per minute per IP).
		rl := ratelimit.New(100, time.Minute)
		a2aHandler := rl.Middleware(app.A2AHandler)

		// Bind to localhost only by default to prevent unintended network exposure.
		// Use a reverse proxy or explicit BindAddress config for external access.
		bindAddr := "127.0.0.1"
		if app.Config.Configurations.A2A.BindAddress != "" {
			bindAddr = app.Config.Configurations.A2A.BindAddress
		}
		a2aSrv = &http.Server{
			Addr:         fmt.Sprintf("%s:%d", bindAddr, port),
			Handler:      a2aHandler,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("PANIC recovered in a2a-server: %v", r)
				}
			}()
			if err := a2aSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("a2a: server error: %v", err)
			}
		}()
		fmt.Fprintf(w, "A2A server listening on %s:%d\n", bindAddr, port)
	}

	// Start scheduler if enabled (after handler resolved so factory can be set as runner).
	if app.Scheduler != nil {
		app.Scheduler.Start(ctx)
		// Recover any missed cron runs from downtime.
		if recovered, err := app.Scheduler.RecoverMissed(ctx); err != nil {
			log.Printf("scheduler: recover missed: %v", err)
		} else if recovered > 0 {
			fmt.Fprintf(w, "Scheduler recovered %d missed runs.\n", recovered)
		}
		fmt.Fprintln(w, "Scheduler started.")
	}

	// Start token lifecycle manager (background OAuth refresh).
	if app.Lifecycle != nil {
		app.Lifecycle.Start(ctx)
	}

	// Start background session cleanup (configurable interval and max age).
	app.Sessions.StartCleanupLoop(ctx,
		app.Config.Options.Sessions.CleanupInterval,
		app.Config.Options.Sessions.MaxAge)

	// Start auth session cleanup (remove expired login sessions every 10 minutes).
	if app.Authenticator != nil {
		app.Authenticator.StartCleanup(ctx, 10*time.Minute)
	}

	// Start all registered channels.
	channels := app.Channels.All()
	started := 0
	for name, ch := range channels {
		if err := ch.Start(ctx, handler); err != nil {
			log.Printf("channel %s failed to start: %v", name, err)
			continue
		}
		fmt.Fprintf(w, "Channel started: %s\n", name)
		started++
	}

	if started == 0 && app.Scheduler == nil {
		fmt.Fprintln(w, "No channels or scheduler running. Use 'aibutler setup' to configure.")
		return nil
	}

	// Start LAN discovery (if webchat is enabled).
	for _, ch := range app.Config.Settings.ActiveChannels {
		if ch == "webchat" && app.LANDiscovery != nil {
			if err := app.LANDiscovery.Start(ctx); err != nil {
				log.Printf("lan: start error: %v", err)
			} else {
				fmt.Fprintln(w, "LAN discovery started.")
			}
			break
		}
	}

	// Start auto-updater (if check URL is configured).
	if app.Updater != nil {
		if err := app.Updater.Start(ctx); err != nil {
			log.Printf("updater: start error: %v", err)
		}
	}

	// U5: Start config file watcher for hot-reload of hooks and permissions.
	var stopWatcher func()
	if app.configPath != "" {
		stopWatcher = app.Config.StartWatcher(ctx, app.configPath, 60*time.Second, func(newCfg *config.Config) {
			log.Println("config: hot-reload applied (hooks + permissions)")
		})
	}

	// Start workspace TTL enforcer (purges stale swarm workspace entries).
	var stopTTL func()
	if app.SwarmWorkspace != nil {
		ttlHours := app.Config.Configurations.Swarm.WorkspaceTTLHours
		if ttlHours <= 0 {
			ttlHours = 24
		}
		stopTTL = swarmws.StartTTLEnforcer(ctx, app.SwarmWorkspace, time.Hour, ttlHours)
	}

	// Show WebChat URL if webchat is an active channel.
	for _, ch := range app.Config.Settings.ActiveChannels {
		if ch == "webchat" {
			web := app.Config.Configurations.Web
			fmt.Fprintf(w, "\nWebChat: http://%s:%d\n", web.BindAddress, web.Port)
			fmt.Fprintf(w, "Dashboard: http://%s:%d/api/dashboard/stats\n", web.BindAddress, web.Port)
			break
		}
	}

	fmt.Fprintln(w, "\nReady. Press Ctrl+C to stop.")

	// Block until signal.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Fprintln(w, "\nShutting down...")
	cancel()

	// Stop channels gracefully.
	stopCtx := context.Background()
	for name, ch := range channels {
		if err := ch.Stop(stopCtx); err != nil {
			log.Printf("channel %s stop error: %v", name, err)
		}
	}

	// Stop workspace TTL enforcer.
	if stopTTL != nil {
		stopTTL()
	}

	// Stop LAN discovery.
	if app.LANDiscovery != nil {
		app.LANDiscovery.Stop()
	}

	// Stop config watcher.
	if stopWatcher != nil {
		stopWatcher()
	}

	// Stop updater.
	if app.Updater != nil {
		app.Updater.Stop()
	}

	// Stop A2A server if running.
	if a2aSrv != nil {
		if err := a2aSrv.Shutdown(stopCtx); err != nil {
			log.Printf("a2a: shutdown error: %v", err)
		}
	}

	fmt.Fprintln(w, "Stopped.")
	return nil
}

// resolveHandler tries to create the real AI pipeline handler.
// Falls back to echo if no API key is configured.
func resolveHandler(app *App, w io.Writer) channel.MessageHandler {
	adapter, provider := resolveModelAdapter(app)
	if adapter == nil {
		fmt.Fprintln(w, "No API key found. Running in echo mode.")
		fmt.Fprintln(w, "  Add key: ./aibutler vault set anthropic_api_key sk-ant-...")
		fmt.Fprintln(w, "  Or:      ./aibutler vault set openai_api_key sk-...")
		return echoHandler(app)
	}

	fmt.Fprintf(w, "AI provider connected: %s\n", provider)

	// Wire embedding provider for vector search (if available).
	if app.HybridSearcher != nil && app.VectorStore != nil {
		embedder, embedProvider := resolveEmbedder(app)
		if embedder != nil {
			app.HybridSearcher.SetVectorSearch(app.VectorStore, embedder.Embed)
			// Also wire the indexer into the shared memory store so that
			// SaveThought and SaveTranscript automatically populate
			// memory_vectors on the way in. Without this, memory_vectors
			// stays empty and hybrid search silently degrades to FTS5+graph.
			if app.MemStore != nil {
				indexer := memory.NewVectorIndexer(app.VectorStore, embedder.Embed, embedProvider)
				app.MemStore.SetIndexer(indexer)
			}
			fmt.Fprintf(w, "Embedding provider connected: %s\n", embedProvider)
		} else {
			fmt.Fprintln(w, "Vector search disabled (no embedding provider found).")
			fmt.Fprintln(w, "  To enable semantic search, do one of:")
			fmt.Fprintln(w, "  1. Install Ollama (https://ollama.com) and run: ollama pull nomic-embed-text")
			fmt.Fprintln(w, "  2. Add OpenAI key: ./aibutler vault set openai_api_key sk-...")
			fmt.Fprintln(w, "  3. Set in config.yaml: configurations.embedding.provider: ollama")
			fmt.Fprintln(w, "  Memory search will use keyword matching (FTS5) until embeddings are configured.")
		}
	}

	// Load skills for the prompt composer.
	if err := app.Composer.LoadSkills(); err != nil {
		log.Printf("warning: could not load skills: %v", err)
	}

	// Resolve custom role router (for ModeCustom).
	var roleRouter *agent.RoleRouter
	if app.Config.Settings.AgentMode == "custom" {
		var roles []agent.CustomRole
		// Load roles from config first.
		for _, spec := range app.Config.Configurations.Agents.CustomRoles {
			roles = append(roles, agent.CustomRole{
				Name:        spec.Name,
				Description: spec.Description,
				Model:       spec.Model,
				Tools:       spec.Tools,
				Prompt:      spec.Prompt,
			})
		}
		// Merge roles from DB (overrides config if same name).
		dbRoles, _ := agent.LoadRolesFromDB(context.Background(), app.DB.Conn())
		nameSet := make(map[string]bool, len(roles))
		for _, r := range roles {
			nameSet[r.Name] = true
		}
		for _, r := range dbRoles {
			if !nameSet[r.Name] {
				roles = append(roles, r)
			}
		}
		routing := app.Config.Configurations.Agents.Routing
		if routing == "" {
			routing = "classify"
		}
		roleRouter = agent.NewRoleRouter(roles, routing, app.DB.Conn())
	}

	// Create post-run processor for FTS5 indexing + entity extraction.
	// Reuse the shared Store/EntityStore from Bootstrap so that anything
	// wired up here (like the vector indexer above) also applies to the
	// transcript-save path taken by every chat turn.
	postProc := &postRunProcessor{mem: app.MemStore, entities: app.EntityStore}

	// Create compactor for context window management.
	compactor := prompt.NewCompactor(prompt.DefaultCompactorConfig())

	// Create agent factory.
	// Capability set = messaging defaults + IoT overlay (iot.sensor.read +
	// iot.device.discover). Comfort / safety IoT controls are NOT granted
	// by default — they require explicit config or the user granting them
	// via the interactive prompter when the agent asks. This keeps the
	// demo smart-home experience functional out-of-the-box while preserving
	// the deny-by-default posture for destructive actions.
	defaultCaps := append([]capability.Capability{}, capability.MessagingDefaults()...)
	defaultCaps = append(defaultCaps, capability.IoTDefaults()...)
	factory := model.NewFactory(model.FactoryConfig{
		Composer:      app.Composer,
		Model:         adapter,
		Tools:         app.Dispatcher,
		Caps:          capability.NewCapabilitySet(defaultCaps),
		Tracker:       app.Tracker,
		DB:            app.DB.Conn(),
		Config:        app.Config,
		RoleRouter:    roleRouter,
		PostProcessor:  postProc,
		Compactor:      compactor,
		BatchExecutor:  app.BatchExecutor,
	})

	// Wire factory as scheduler runner so scheduled tasks can execute.
	if app.Scheduler != nil {
		app.Scheduler.SetRunner(factory)
	}

	// Wire factory as A2A task runner (replaces the bootstrap-time stub).
	if app.A2AHandler != nil {
		app.A2AHandler.SetRunner(&factoryRunner{factory: factory})
	}

	// Wire factory into the MCP v2 server so `butler.agent.delegate` from
	// external MCP clients actually runs a task end-to-end instead of
	// returning "not yet implemented". The MCP server was constructed in
	// app.go before the factory existed; we inject the runner here now
	// that it's built.
	if app.MCPAgentAdapter != nil {
		app.MCPAgentAdapter.SetRunner(factory)
	}

	// Wire swarm orchestrator (when enabled in config).
	if app.Config.Configurations.Swarm.Enabled {
		orch := swarm.New(app.DB.Conn(), adapter, &registryAdapter{reg: app.AgentRegistry}, &factoryRunner{factory: factory})
		// Apply budget cap from config.
		if app.Config.Configurations.Swarm.BudgetUSD > 0 {
			orch.SetBudget(app.Config.Configurations.Swarm.BudgetUSD)
		}
		swarmName, swarmDesc, swarmSchema, swarmCap, swarmExec := swarm.NewSwarmTool(orch, app.SwarmWorkspace)
		app.Tools.Register(&tool.FuncTool{ToolName: swarmName, ToolDesc: swarmDesc, ToolSchema: swarmSchema, ToolCap: swarmCap, Exec: swarmExec})

		// Wire orch into the MCP v2 server so `butler.swarm.run` dispatches
		// to the real orchestrator instead of the configured-out error.
		// Only wired when swarm is enabled — MCP clients get a precise
		// "not configured in this build" error otherwise.
		if app.MCPServerV2 != nil {
			app.MCPServerV2.SetSwarmRunner(&mcpSwarmRunnerAdapter{orch: orch})
		}
	}

	// Wire swarm mode components: router + message bus.
	if app.Config.Settings.AgentMode == "swarm" {
		specs := specialist.DefaultSpecialists()
		routes := specialist.BuildRoutes(specs)
		agentRouter := router.New(routes, adapter, app.DB.Conn())
		app.AgentRouter = agentRouter // Available for swarm message routing
		msgBus := bus.New()
		app.MessageBus = msgBus // Available for inter-agent pub/sub communication

		fmt.Fprintln(w, "Swarm mode active: router + message bus wired.")
		fmt.Fprintf(w, "  Specialists: %d configured\n", len(specs))

		// Double concurrency limit in swarm mode.
		maxConcurrent := app.Config.Configurations.Agents.MaxConcurrent * 2
		if maxConcurrent < 10 {
			maxConcurrent = 10
		}
		app.Config.Configurations.Agents.MaxConcurrent = maxConcurrent
	}

	// Register agent orchestration tools.
	agentSem := agent.NewSemaphore(app.Config.Configurations.Agents.MaxConcurrent)
	userSem := agent.NewUserSemaphore(agentSem, app.Config.Options.Agents.PerUserAgentLimit)
	delegateCfg := agent.DelegateConfig{
		Model:             adapter,
		Tools:             app.Dispatcher,
		Caps:              capability.NewCapabilitySet(capability.MessagingDefaults()),
		DB:                app.DB.Conn(),
		Timeout:           app.Config.Options.Agents.SubagentTimeout,
		MaxDepth:          app.Config.Configurations.Agents.MaxDepth,
		PerSubagentBudget: app.Config.Options.Agents.PerSubagentBudget,
		BackgroundMax:     app.Config.Options.Agents.BackgroundMax,
		Semaphore:         agentSem,
		UserSemaphore:     userSem,
	}
	dName, dDesc, dSchema, dCap, dExec := agent.NewDelegateTool(delegateCfg)
	app.Tools.Register(&tool.FuncTool{ToolName: dName, ToolDesc: dDesc, ToolSchema: dSchema, ToolCap: dCap, Exec: dExec})
	sName, sDesc, sSchema, sCap, sExec := agent.NewSpawnTool(delegateCfg)
	app.Tools.Register(&tool.FuncTool{ToolName: sName, ToolDesc: sDesc, ToolSchema: sSchema, ToolCap: sCap, Exec: sExec})

	// Agent lifecycle tools (status, cancel, list).
	conn := app.DB.Conn()
	for _, toolFactory := range []func(*sql.DB) (string, string, string, string, func(context.Context, string) (string, error)){
		agent.NewStatusTool,
		agent.NewCancelTool,
		agent.NewListBackgroundTool,
	} {
		tName, tDesc, tSchema, tCap, tExec := toolFactory(conn)
		app.Tools.Register(&tool.FuncTool{ToolName: tName, ToolDesc: tDesc, ToolSchema: tSchema, ToolCap: tCap, Exec: tExec})
	}

	// Agent mesh tools (peer delegation, critic, task status).
	meshReg := &meshRegistryAdapter{reg: app.AgentRegistry}
	meshRunner := &factoryRunner{factory: factory}
	// Wire A2A v2 client as the mesh A2A adapter.
	a2aHTTPClient := a2aclient.New(app.Config.Options.Models.RetryCount)
	meshA2A := a2aclient.NewMeshAdapter(a2aHTTPClient)

	// E7: Create composite A2A client that tries A2A first, then subprocess bridges.
	var peerA2A agent.A2AClient = meshA2A
	if len(app.SubprocessBridges) > 0 {
		peerA2A = &compositeA2AClient{a2a: meshA2A, bridges: app.SubprocessBridges}
	}
	peerName, peerDesc, peerSchema, peerCap, peerExec := agent.NewPeerTool(meshReg, meshRunner, peerA2A)
	app.Tools.Register(&tool.FuncTool{ToolName: peerName, ToolDesc: peerDesc, ToolSchema: peerSchema, ToolCap: peerCap, Exec: peerExec})
	criticName, criticDesc, criticSchema, criticCap, criticExec := agent.NewCriticTool(meshRunner)
	app.Tools.Register(&tool.FuncTool{ToolName: criticName, ToolDesc: criticDesc, ToolSchema: criticSchema, ToolCap: criticCap, Exec: criticExec})
	statusName, statusDesc, statusSchema, statusCap, statusExec := agent.NewTaskStatusTool(meshA2A)
	app.Tools.Register(&tool.FuncTool{ToolName: statusName, ToolDesc: statusDesc, ToolSchema: statusSchema, ToolCap: statusCap, Exec: statusExec})

	// U1+U2+U4: Wire streaming pipeline components at runtime.
	// streamDeliverFn creates a StreamDelivery for channels that support streaming.
	// BackpressureRelay pipes model output through backpressure handling before
	// delivery, and CollectStreamResponse aggregates the stream into a Response.
	streamDeliverFn := func(ctx context.Context, ch channel.Channel, accountID string, events <-chan agent.StreamEvent) agent.Response {
		// U4: Relay events through backpressure handler.
		relayed := make(chan agent.StreamEvent, streaming.DefaultStreamConfig().BufferSize)
		done := make(chan struct{})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("PANIC recovered in streaming-relay: %v", r)
				}
			}()
			streaming.BackpressureRelay(events, relayed, done)
		}()

		// U1: Create a StreamDelivery for the target channel.
		delivery := channel.NewStreamDelivery(ch, accountID, 500*time.Millisecond)

		// Tee: deliver tokens to channel while collecting for response.
		collected := make(chan agent.StreamEvent, streaming.DefaultStreamConfig().BufferSize)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("PANIC recovered in stream-delivery: %v", r)
				}
			}()
			defer close(collected)
			for evt := range relayed {
				// Forward to collector.
				collected <- evt
				// Deliver text deltas to channel.
				if evt.Type == "text_delta" && evt.Text != "" {
					_ = delivery.DeliverToken(ctx, evt.Text)
				}
			}
			_ = delivery.Flush(ctx)
		}()

		// U2: Collect all events into a final Response for cost tracking.
		return model.CollectStreamResponse(collected)
	}
	// Create router with all dependencies.
	bundle := i18n.New(app.Config.Settings.Language)
	stop := stopphrase.NewMatcher(bundle)
	typing := channel.NewTypingManager(
		time.Duration(app.Config.Options.Typing.IntervalMs)*time.Millisecond,
		time.Duration(app.Config.Options.Typing.TimeoutMs)*time.Millisecond,
	)
	router := channel.NewRouter(channel.RouterConfig{
		Sessions:      app.Sessions,
		Stop:          stop,
		Typing:        typing,
		Channels:      app.Channels,
		Config:        app.Config,
		I18n:          bundle,
		DB:            app.DB.Conn(),
		Agent:         factory,
		Voice:         app.Voice,
		Media:         app.MediaPipeline,
		Tracker:       app.Tracker,
		StreamDeliver: streamDeliverFn,
	})

	return router.HandleMessage
}

// resolveModelAdapter checks for API keys in the vault and creates the appropriate ModelAdapter.
// It creates a single pooled HTTP client shared across all adapters for connection reuse.
func resolveModelAdapter(app *App) (agent.ModelAdapter, string) {
	cfg := app.Config
	timeout := cfg.Options.Models.RequestTimeout
	retries := cfg.Options.Models.RetryCount
	modelName := cfg.Configurations.Models.Primary

	// Create a single pooled HTTP client for all model adapters.
	pooledClient, _ := model.NewPooledClient(model.DefaultPoolConfig(), timeout)

	ctx := context.Background()

	// Check for Anthropic API key.
	if cred, err := app.Vault.Get(ctx, "anthropic_api_key"); err == nil && len(cred.Value) > 0 {
		if modelName == "" || strings.HasPrefix(modelName, "claude") {
			if modelName == "" {
				modelName = "claude-sonnet-4-6"
			}
			adapter := model.NewClaude(string(cred.Value), modelName, timeout, retries)
			adapter.SetHTTPClient(pooledClient)
			adapter.SetMaxTokens(cfg.Options.Models.MaxTokens)
			adapter.SetTemperature(cfg.Options.Models.Temperature)
			return adapter, "Anthropic Claude"
		}
	}

	// Check for OpenAI API key.
	if cred, err := app.Vault.Get(ctx, "openai_api_key"); err == nil && len(cred.Value) > 0 {
		if modelName == "" || strings.HasPrefix(modelName, "gpt") {
			if modelName == "" {
				modelName = "gpt-4o"
			}
			adapter := model.NewOpenAI(string(cred.Value), modelName, timeout, retries)
			adapter.SetHTTPClient(pooledClient)
			adapter.SetMaxTokens(cfg.Options.Models.MaxTokens)
			adapter.SetTemperature(cfg.Options.Models.Temperature)
			return adapter, "OpenAI"
		}
	}

	// Check for Google Gemini API key.
	if cred, err := app.Vault.Get(ctx, "gemini_api_key"); err == nil && len(cred.Value) > 0 {
		if modelName == "" || strings.HasPrefix(modelName, "gemini") {
			if modelName == "" {
				modelName = "gemini-2.0-flash"
			}
			adapter := model.NewGemini(string(cred.Value), modelName, timeout, retries)
			adapter.SetHTTPClient(pooledClient)
			adapter.SetMaxTokens(cfg.Options.Models.MaxTokens)
			adapter.SetTemperature(cfg.Options.Models.Temperature)
			return adapter, "Google Gemini"
		}
	}

	// Check for xAI (Grok) API key.
	if cred, err := app.Vault.Get(ctx, "xai_api_key"); err == nil && len(cred.Value) > 0 {
		if modelName == "" || strings.HasPrefix(modelName, "grok") {
			if modelName == "" {
				modelName = "grok-2"
			}
			adapter := model.NewOpenAICompat("https://api.x.ai/v1/chat/completions", string(cred.Value), modelName, timeout, retries)
			adapter.SetHTTPClient(pooledClient)
			adapter.SetMaxTokens(cfg.Options.Models.MaxTokens)
			adapter.SetTemperature(cfg.Options.Models.Temperature)
			return adapter, "xAI Grok"
		}
	}

	// Check for OpenAI-compatible endpoint (Ollama, LM Studio, etc.).
	// If model name is not claude/gpt/gemini/grok, assume local model.
	if modelName != "" && !strings.HasPrefix(modelName, "claude") && !strings.HasPrefix(modelName, "gpt") && !strings.HasPrefix(modelName, "gemini") && !strings.HasPrefix(modelName, "grok") {
		// Strip provider-hint prefixes. Docs and setup tell users to write
		// "ollama/llama3", "lmstudio/qwen", "vllm/mistral", etc., so they can
		// see at a glance which backend the model is for. The OpenAI-compat
		// endpoint itself wants the bare model name — stripping here means
		// both forms work and users following the docs don't silently end up
		// with Ollama rejecting the "ollama/" prefix as an unknown model.
		bareName := modelName
		for _, p := range []string{"ollama/", "lmstudio/", "vllm/", "localai/", "groq/", "deepseek/", "openai_compat/", "local/"} {
			if strings.HasPrefix(bareName, p) {
				bareName = strings.TrimPrefix(bareName, p)
				break
			}
		}
		compatURL := cfg.Configurations.Models.BaseURL
		if compatURL == "" {
			compatURL = model.DefaultOllamaURL
		}
		// Normalize: if the user supplied only the base hostname
		// (e.g. "https://ollama.com"), append the standard chat path so
		// they don't have to remember the trailing /v1/chat/completions.
		if !strings.Contains(compatURL, "/chat/completions") {
			compatURL = strings.TrimRight(compatURL, "/") + "/v1/chat/completions"
		}
		apiKey := ""
		if cred, err := app.Vault.Get(ctx, "openai_compat_api_key"); err == nil {
			apiKey = string(cred.Value)
		}
		adapter := model.NewOpenAICompat(compatURL, apiKey, bareName, timeout, retries)
		adapter.SetHTTPClient(pooledClient)
		adapter.SetMaxTokens(cfg.Options.Models.MaxTokens)
		adapter.SetTemperature(cfg.Options.Models.Temperature)
		label := "Local"
		if strings.Contains(compatURL, "ollama.com") {
			label = "Ollama Cloud"
		}
		return adapter, label + " (" + bareName + ")"
	}

	return nil, ""
}

// resolveEmbedder checks config and vault for embedding providers.
// Priority: explicit config > OpenAI API key > Ollama auto-detect.
func resolveEmbedder(app *App) (vector.Embedder, string) {
	cfg := app.Config.Configurations.Embedding
	timeout := app.Config.Options.Models.RequestTimeout
	ctx := context.Background()

	provider := cfg.Provider
	embedModel := cfg.Model

	// Explicit provider in config.
	switch provider {
	case "openai":
		if cred, err := app.Vault.Get(ctx, "openai_api_key"); err == nil && len(cred.Value) > 0 {
			if embedModel == "" {
				embedModel = "text-embedding-3-small"
			}
			return model.NewEmbeddingOpenAI(string(cred.Value), embedModel, timeout), "OpenAI Embeddings"
		}
		return nil, ""
	case "ollama":
		if embedModel == "" {
			embedModel = "nomic-embed-text"
		}
		baseURL := cfg.BaseURL
		if baseURL != "" {
			// User specified a custom base URL — append /api/embed if not present.
			if !strings.HasSuffix(baseURL, "/api/embed") {
				baseURL = strings.TrimRight(baseURL, "/") + "/api/embed"
			}
		}
		return model.NewEmbeddingOllamaWithURL(baseURL, embedModel, timeout), "Ollama Embeddings (" + embedModel + ")"
	case "openai_compat":
		apiKey := ""
		if cred, err := app.Vault.Get(ctx, "openai_compat_api_key"); err == nil {
			apiKey = string(cred.Value)
		}
		if embedModel == "" {
			embedModel = "nomic-embed-text"
		}
		return model.NewEmbeddingCompat(cfg.BaseURL, apiKey, embedModel, timeout), "OpenAI-Compatible Embeddings (" + embedModel + ")"
	}

	// Auto-detect: check for OpenAI key.
	if cred, err := app.Vault.Get(ctx, "openai_api_key"); err == nil && len(cred.Value) > 0 {
		if embedModel == "" {
			embedModel = "text-embedding-3-small"
		}
		return model.NewEmbeddingOpenAI(string(cred.Value), embedModel, timeout), "OpenAI Embeddings (auto)"
	}

	// Auto-detect: check for Ollama running locally.
	ollamaBase := cfg.BaseURL
	if ollamaBase == "" {
		ollamaBase = "http://localhost:11434"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get(ollamaBase + "/api/tags"); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			// If the user didn't pick an embed model explicitly, probe the
			// installed Ollama models and match the first embedding-capable
			// one. Users commonly have "nomic-embed-text:v1.5" or
			// "bge-large:latest" installed but the old auto-detect asked
			// for a bare "nomic-embed-text" tag that Ollama rejects with 404.
			if embedModel == "" {
				if picked, ok := pickOllamaEmbedModel(resp); ok {
					embedModel = picked
				} else {
					embedModel = "nomic-embed-text"
				}
			}
			embedURL := strings.TrimRight(ollamaBase, "/") + "/api/embed"
			return model.NewEmbeddingOllamaWithURL(embedURL, embedModel, timeout), "Ollama Embeddings (auto: " + embedModel + ")"
		}
	}

	return nil, ""
}

// pickOllamaEmbedModel inspects the /api/tags response body and returns the
// first model name whose name looks like an embedding model. Preference
// order: exact "nomic-embed-text*" > any "*embed*" > any "*bge*".
func pickOllamaEmbedModel(resp *http.Response) (string, bool) {
	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", false
	}
	var nomic, embed, bge string
	for _, m := range payload.Models {
		n := strings.ToLower(m.Name)
		switch {
		case strings.HasPrefix(n, "nomic-embed-text") && nomic == "":
			nomic = m.Name
		case strings.Contains(n, "embed") && embed == "":
			embed = m.Name
		case strings.Contains(n, "bge") && bge == "":
			bge = m.Name
		}
	}
	if nomic != "" {
		return nomic, true
	}
	if embed != "" {
		return embed, true
	}
	if bge != "" {
		return bge, true
	}
	return "", false
}

// postRunProcessor saves conversation turns to session_transcripts (FTS5)
// and extracts entities from all messages for the knowledge graph.
type postRunProcessor struct {
	mem      *memory.Store
	entities *entity.Store
}

func (p *postRunProcessor) AfterAgentRun(ctx context.Context, sessionID, userMsg, assistantMsg string, toolOutputs []agent.ToolOutput) {
	// Use session-scoped turn numbering: query max existing turn for this session
	// to avoid reset-to-0 across multiple runs in the same session.
	turn := p.mem.NextTurnNumber(ctx, sessionID)

	// Save user message + extract entities.
	if userMsg != "" {
		if _, err := p.mem.SaveTranscript(ctx, sessionID, "user", userMsg, turn); err != nil {
			log.Printf("postrun: save user transcript: %v", err)
		}
		turn++
		p.extractAndSaveEntities(ctx, sessionID, userMsg)
	}

	// Save tool outputs + extract entities.
	for _, out := range toolOutputs {
		content := out.ToolName + ": " + out.Output
		if _, err := p.mem.SaveTranscript(ctx, sessionID, "tool", content, turn); err != nil {
			log.Printf("postrun: save tool transcript: %v", err)
		}
		turn++
		p.extractAndSaveEntities(ctx, sessionID, out.Output)
	}

	// Save assistant response + extract entities.
	if assistantMsg != "" {
		if _, err := p.mem.SaveTranscript(ctx, sessionID, "assistant", assistantMsg, turn); err != nil {
			log.Printf("postrun: save assistant transcript: %v", err)
		}
		p.extractAndSaveEntities(ctx, sessionID, assistantMsg)
	}
}

// extractAndSaveEntities runs entity extraction on text and persists results.
func (p *postRunProcessor) extractAndSaveEntities(ctx context.Context, sessionID, text string) {
	extracted := entity.Extract(text)
	for _, name := range extracted.People {
		if _, err := p.entities.SaveOrUpdate(ctx, entity.TypePerson, name, sessionID, nil); err != nil {
			log.Printf("postrun: save entity person: %v", err)
		}
	}
	for _, name := range extracted.Projects {
		if _, err := p.entities.SaveOrUpdate(ctx, entity.TypeProject, name, sessionID, nil); err != nil {
			log.Printf("postrun: save entity project: %v", err)
		}
	}
	for _, text := range extracted.Decisions {
		if _, err := p.entities.SaveOrUpdate(ctx, entity.TypeDecision, text, sessionID, nil); err != nil {
			log.Printf("postrun: save entity decision: %v", err)
		}
	}
	for _, text := range extracted.ActionItems {
		if _, err := p.entities.SaveOrUpdate(ctx, entity.TypeActionItem, text, sessionID, nil); err != nil {
			log.Printf("postrun: save entity action_item: %v", err)
		}
	}
	for _, text := range extracted.Insights {
		if _, err := p.entities.SaveOrUpdate(ctx, entity.TypeInsight, text, sessionID, nil); err != nil {
			log.Printf("postrun: save entity insight: %v", err)
		}
	}
}

// factoryRunner adapts *model.Factory to the a2a.TaskRunner interface.
// Each inbound A2A task gets a synthetic session ID so conversations are isolated.
type factoryRunner struct {
	factory *model.Factory
}

func (r *factoryRunner) RunTask(ctx context.Context, task string) (string, error) {
	sessionID := fmt.Sprintf("a2a-%d", time.Now().UnixNano())
	result, err := r.factory.Run(ctx, sessionID, task, "a2a")
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

// registryAdapter adapts *a2aregistry.Registry to swarm.RegistryLookup.
type registryAdapter struct {
	reg *a2aregistry.Registry
}

func (r *registryAdapter) Discover(ctx context.Context, capability string) ([]swarm.RegistryEntry, error) {
	records, err := r.reg.Discover(ctx, capability)
	if err != nil {
		return nil, err
	}
	entries := make([]swarm.RegistryEntry, len(records))
	for i, rec := range records {
		entries[i] = swarm.RegistryEntry{Name: rec.Name, URL: rec.URL}
	}
	return entries, nil
}

// meshRegistryAdapter adapts *a2aregistry.Registry to agent.RegistryLookup.
type meshRegistryAdapter struct {
	reg *a2aregistry.Registry
}

func (r *meshRegistryAdapter) Discover(ctx context.Context, capability string) ([]agent.RegistryEntry, error) {
	records, err := r.reg.Discover(ctx, capability)
	if err != nil {
		return nil, err
	}
	entries := make([]agent.RegistryEntry, len(records))
	for i, rec := range records {
		entries[i] = agent.RegistryEntry{
			Name:         rec.Name,
			URL:          rec.URL,
			Capabilities: rec.Capabilities,
		}
	}
	return entries, nil
}

// compositeA2AClient wraps an A2A client with subprocess bridge fallback.
// When A2A delegation fails, it attempts to find a subprocess bridge that
// matches the peer URL or name and executes the task via that bridge.
type compositeA2AClient struct {
	a2a     agent.A2AClient
	bridges map[string]*subprocpkg.Adapter
}

func (c *compositeA2AClient) Discover(ctx context.Context, url string) (interface{}, error) {
	return c.a2a.Discover(ctx, url)
}

func (c *compositeA2AClient) Delegate(ctx context.Context, peerURL, token, task string) (agent.A2ATaskResult, error) {
	// Try A2A first.
	result, err := c.a2a.Delegate(ctx, peerURL, token, task)
	if err == nil {
		return result, nil
	}

	// Fallback: try subprocess bridges by name (peerURL may contain the bridge name).
	for name, bridge := range c.bridges {
		if strings.Contains(peerURL, name) || name == peerURL {
			output, bErr := bridge.Execute(ctx, task)
			if bErr != nil {
				return nil, fmt.Errorf("subprocess bridge %s: %w", name, bErr)
			}
			return &subprocTaskResult{output: output}, nil
		}
	}

	return nil, err
}

func (c *compositeA2AClient) GetTaskStatus(ctx context.Context, peerURL, taskID string) (string, error) {
	return c.a2a.GetTaskStatus(ctx, peerURL, taskID)
}

// subprocTaskResult implements agent.A2ATaskResult for subprocess bridge output.
type subprocTaskResult struct {
	output string
}

func (r *subprocTaskResult) GetStatus() string { return "completed" }
func (r *subprocTaskResult) GetOutput() string { return r.output }
func (r *subprocTaskResult) GetError() string  { return "" }
func (r *subprocTaskResult) GetTaskID() string { return "subprocess" }

// mcpSwarmRunnerAdapter wraps *swarm.Orchestrator so it satisfies
// mcpserverv2.SwarmRunner (which takes ctx + goal, not ctx + runID + goal
// — the MCP tool doesn't expose runID and lets the orchestrator auto-
// generate one).
type mcpSwarmRunnerAdapter struct {
	orch *swarm.Orchestrator
}

func (a *mcpSwarmRunnerAdapter) Run(ctx context.Context, goal string) (string, error) {
	if a.orch == nil {
		return "", fmt.Errorf("swarm orchestrator not configured")
	}
	// Empty runID triggers auto-generation inside Run().
	return a.orch.Run(ctx, "", goal)
}

// echoHandler returns a handler that echoes messages back.
func echoHandler(app *App) channel.MessageHandler {
	return func(ctx context.Context, env channel.Envelope) error {
		log.Printf("message from %s/%s: %s", env.Channel, env.AccountID, env.Text)

		ch, ok := app.Channels.Get(env.Channel)
		if !ok {
			return nil
		}
		reply := channel.OutgoingMessage{
			Text: fmt.Sprintf("Echo: %s\n\n(AI provider not connected yet — run `aibutler setup` to add your API key)", env.Text),
		}
		return ch.Send(ctx, env.AccountID, reply)
	}
}
