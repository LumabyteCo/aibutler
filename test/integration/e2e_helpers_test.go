//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/contact"
	"github.com/LumabyteCo/aibutler/internal/file"
	"github.com/LumabyteCo/aibutler/internal/finance"
	"github.com/LumabyteCo/aibutler/internal/i18n"
	"github.com/LumabyteCo/aibutler/internal/instruction"
	"github.com/LumabyteCo/aibutler/internal/iot"
	"github.com/LumabyteCo/aibutler/internal/media"
	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/model"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/proxy"
	"github.com/LumabyteCo/aibutler/internal/schedule"
	"github.com/LumabyteCo/aibutler/internal/services"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/internal/shell"
	"github.com/LumabyteCo/aibutler/internal/stopphrase"
	"github.com/LumabyteCo/aibutler/internal/taskctx"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/internal/vault"
	"github.com/LumabyteCo/aibutler/internal/voice"
	"github.com/LumabyteCo/aibutler/testutil"
)

// extPipeline extends the basic pipeline with direct DB access and extra channels.
type extPipeline struct {
	Router     *channel.Router
	Factory    *model.Factory
	Fake       *testutil.FakeModel
	Channel    *fakeChannel
	SM         *session.Manager
	Config     *config.Config
	DB         *sql.DB
	Registry   *tool.Registry
	Channels   *channel.Registry
	Auditor    *testutil.FakeAuditor
	Tracker    *prompt.Tracker
	Composer   *prompt.Composer
	FileDir    string           // temp dir for file tools
	IoTAdapter *iot.StubAdapter // IoT stub for assertions
	Caps       *capability.CapabilitySet
	ProxyMux   *http.ServeMux      // configurable proxy HTTP handler
	FakeVault  *testutil.FakeVault // credential vault for proxy tests
	ServiceMux *http.ServeMux      // configurable service HTTP handler
}

// pipelineOpts configures which tool families to register.
type pipelineOpts struct {
	Responses       []agent.Response
	WithMemory      bool
	WithInstruction bool
	WithTaskCtx     bool
	WithSchedule    bool
	WithFinance     bool
	WithVoice       bool
	WithMedia       bool
	WithChannelTool bool
	WithAuditor     bool
	WithFile        bool
	WithShell       bool
	WithIoT         bool
	WithDelegate    bool
	WithProxy       bool
	WithServices    bool
	CapOverride     *capability.CapabilitySet
	ConfigOverride  func(*config.Config)
}

// setupPipelineWithOpts creates a full E2E pipeline with configurable tool families.
func setupPipelineWithOpts(t *testing.T, opts pipelineOpts) *extPipeline {
	t.Helper()

	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	if opts.ConfigOverride != nil {
		opts.ConfigOverride(cfg)
	}

	fake := testutil.NewFakeModel(opts.Responses...)
	db := database.Conn()

	sm := session.NewManager(db, cfg)
	tracker := prompt.NewTracker(db, cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, db)

	// Tool registry with data tools always registered.
	registry := tool.NewRegistry()
	tool.RegisterDataTools(registry, db)

	// Optional tool families.
	if opts.WithMemory {
		memStore := memory.NewStore(db)
		memory.RegisterMemoryTools(registry, memStore, nil)
	}
	if opts.WithInstruction {
		instrStore := instruction.NewStore(db)
		instruction.RegisterInstructionTools(registry, instrStore)
	}
	if opts.WithTaskCtx {
		ctxStore := taskctx.NewStore(db)
		taskctx.RegisterTaskContextTools(registry, ctxStore)
	}
	if opts.WithSchedule {
		schedStore := schedule.NewStore(db)
		schedule.RegisterScheduleTools(registry, schedStore, nil)
	}
	if opts.WithFinance {
		watchStore := finance.NewWatchlistStore(db)
		finance.RegisterFinanceTools(registry, &stubFinanceProvider{}, watchStore)
	}

	// File tools.
	var fileDir string
	if opts.WithFile {
		fileDir = t.TempDir()
		file.RegisterFileTools(registry, []string{fileDir})
	}

	// Shell tools.
	if opts.WithShell {
		shellCfg := config.ShellConfig{
			Mode:    "allowlist",
			Allowed: []string{"echo", "printf", "true", "false"},
		}
		shellExec := shell.NewExecutor(shellCfg, nil)
		shell.RegisterShellTools(registry, shellExec)
	}

	// Capability engine + auditor.
	var auditor *testutil.FakeAuditor
	var capAuditor capability.Auditor
	if opts.WithAuditor {
		auditor = testutil.NewFakeAuditor()
		capAuditor = auditor
	}
	engine := capability.NewEngine(capAuditor)
	dispatcher := tool.NewDispatcher(registry, engine, capAuditor)

	caps := capability.NewCapabilitySet(capability.MessagingDefaults())
	if opts.CapOverride != nil {
		caps = opts.CapOverride
	}

	// IoT tools (needs engine for capability checks).
	var iotAdapter *iot.StubAdapter
	if opts.WithIoT {
		iotAdapter = iot.NewStubAdapter()
		devices := []iot.Device{
			{ID: "temp-1", Name: "Living Room Temp", DeviceType: "sensor", Tier: iot.TierSensor, Enabled: true},
			{ID: "light-1", Name: "Living Room Light", DeviceType: "light", Tier: iot.TierComfort, Enabled: true},
			{ID: "thermo-1", Name: "Thermostat", DeviceType: "thermostat", Tier: iot.TierComfort, Enabled: true},
			{ID: "lock-1", Name: "Front Door Lock", DeviceType: "lock", Tier: iot.TierSafety, Enabled: true},
		}
		for _, d := range devices {
			iotAdapter.AddDevice(d)
		}
		iotAdapter.AddReading("temp-1", iot.SensorReading{DeviceID: "temp-1", Metric: "temperature", Value: 22.5, Unit: "°C"})

		fakeVault := testutil.NewFakeVault()
		pin := iot.NewPINVerifier(fakeVault)
		_ = pin.SetPIN(context.Background(), "1234")

		controller := iot.NewController(iotAdapter, engine, pin)
		for _, d := range devices {
			controller.RegisterDevice(d)
		}
		iot.RegisterIoTTools(registry, controller)
	}

	// Proxy tools (needs engine for internal cap check).
	var proxyMux *http.ServeMux
	var fakeVlt *testutil.FakeVault
	if opts.WithProxy {
		proxyMux = http.NewServeMux()
		fakeVlt = testutil.NewFakeVault()
		svcReg, _ := vault.NewServiceRegistry("")
		resolver := proxy.NewCredentialResolver(svcReg, fakeVlt)
		executor := proxy.NewHTTPExecutor(10 * time.Second)
		executor.SetClient(&http.Client{Transport: &localRoundTripper{handler: proxyMux}})
		refresher := proxy.NewTokenRefresher(fakeVlt, nil)
		p := proxy.NewProxy(engine, resolver, executor, refresher, capAuditor)
		proxy.RegisterProxyTools(registry, p)
	}

	// Services tools (weather, news, maps with intercepted HTTP).
	var serviceMux *http.ServeMux
	if opts.WithServices {
		serviceMux = http.NewServeMux()
		svcClient := &http.Client{Transport: &localRoundTripper{handler: serviceMux}}
		services.RegisterEverydayServices(registry, "test-weather-key", "test-news-key", "", "", "", "", "", svcClient)
	}

	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer,
		Model:    fake,
		Tools:    dispatcher,
		Caps:     caps,
		Tracker:  tracker,
		DB:       db,
		Config:   cfg,
	})

	// Delegate/spawn tools (needs factory's model and dispatcher).
	if opts.WithDelegate {
		delegateCfg := agent.DelegateConfig{
			Model:        fake,
			Tools:        dispatcher,
			Caps:         caps,
			DB:           db,
			MaxDepth:     3,
			CurrentDepth: 0,
		}
		dName, dDesc, dSchema, dCap, dExec := agent.NewDelegateTool(delegateCfg)
		registry.Register(&tool.FuncTool{ToolName: dName, ToolDesc: dDesc, ToolSchema: dSchema, ToolCap: dCap, Exec: dExec})
		sName, sDesc, sSchema, sCap, sExec := agent.NewSpawnTool(delegateCfg)
		registry.Register(&tool.FuncTool{ToolName: sName, ToolDesc: sDesc, ToolSchema: sSchema, ToolCap: sCap, Exec: sExec})
	}

	// Channel registry with default webchat fakeChannel.
	fch := &fakeChannel{name: "webchat"}
	chanReg := channel.NewRegistry()
	chanReg.Register(fch)

	// Optional channel tools registration.
	if opts.WithChannelTool {
		resolver := contact.NewResolver(db)
		channel.RegisterChannelToolsWithDeps(registry, chanReg, db, resolver)
	}

	bundle := i18n.New("en")
	stop := stopphrase.NewMatcher(bundle)

	routerCfg := channel.RouterConfig{
		Sessions: sm,
		Stop:     stop,
		Channels: chanReg,
		Config:   cfg,
		I18n:     bundle,
		DB:       db,
		Agent:    factory,
		Tracker:  tracker,
	}

	// Optional voice pipeline.
	if opts.WithVoice {
		stt := &voice.StubSTTProvider{Text: "transcribed text", Language: "en"}
		tts := &voice.StubTTSProvider{}
		normalizer := voice.NewNormalizer()
		voicePipeline := voice.NewPipeline(stt, tts, normalizer, "auto")
		routerCfg.Voice = voicePipeline
	}

	// Optional media pipeline.
	if opts.WithMedia {
		mediaPipeline := media.NewPipeline(20 * 1024 * 1024) // 20MB
		routerCfg.Media = mediaPipeline
	}

	router := channel.NewRouter(routerCfg)

	return &extPipeline{
		Router:     router,
		Factory:    factory,
		Fake:       fake,
		Channel:    fch,
		SM:         sm,
		Config:     cfg,
		DB:         db,
		Registry:   registry,
		Channels:   chanReg,
		Auditor:    auditor,
		Tracker:    tracker,
		Composer:   composer,
		FileDir:    fileDir,
		IoTAdapter: iotAdapter,
		Caps:       caps,
		ProxyMux:   proxyMux,
		FakeVault:  fakeVlt,
		ServiceMux: serviceMux,
	}
}

// setupE2E is the standard E2E setup with just data tools (simplest case).
func setupE2E(t *testing.T, responses ...agent.Response) *extPipeline {
	return setupPipelineWithOpts(t, pipelineOpts{Responses: responses})
}

// sendMsg sends a text message from the default webchat user.
func (p *extPipeline) sendMsg(t *testing.T, text string) {
	t.Helper()
	env := channel.Envelope{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		Channel:   "webchat",
		AccountID: "user-e2e",
		Type:      channel.TypeText,
		Text:      text,
		Timestamp: time.Now(),
	}
	if err := p.Router.HandleMessage(context.Background(), env); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
}

// sendMsgAs sends a text message from a specific channel/account.
func (p *extPipeline) sendMsgAs(t *testing.T, ch, account, text string) {
	t.Helper()
	env := channel.Envelope{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		Channel:   ch,
		AccountID: account,
		Type:      channel.TypeText,
		Text:      text,
		Timestamp: time.Now(),
	}
	if err := p.Router.HandleMessage(context.Background(), env); err != nil {
		t.Fatalf("HandleMessage(%s/%s): %v", ch, account, err)
	}
}

// sendMsgWithThread sends a text message with a thread ID.
func (p *extPipeline) sendMsgWithThread(t *testing.T, ch, account, thread, text string) {
	t.Helper()
	env := channel.Envelope{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		Channel:   ch,
		AccountID: account,
		ThreadID:  thread,
		Type:      channel.TypeText,
		Text:      text,
		Timestamp: time.Now(),
	}
	if err := p.Router.HandleMessage(context.Background(), env); err != nil {
		t.Fatalf("HandleMessage(%s/%s/%s): %v", ch, account, thread, err)
	}
}

// sendEnvelope sends a custom envelope for voice/media tests.
func (p *extPipeline) sendEnvelope(t *testing.T, env channel.Envelope) error {
	t.Helper()
	return p.Router.HandleMessage(context.Background(), env)
}

// lastResponse returns the most recently sent message text.
func (p *extPipeline) lastResponse(t *testing.T) string {
	t.Helper()
	p.Channel.mu.Lock()
	defer p.Channel.mu.Unlock()
	if len(p.Channel.sent) == 0 {
		t.Fatal("no responses sent")
	}
	return p.Channel.sent[len(p.Channel.sent)-1].Text
}

// responseCount returns how many messages were sent to the channel.
func (p *extPipeline) responseCount() int {
	p.Channel.mu.Lock()
	defer p.Channel.mu.Unlock()
	return len(p.Channel.sent)
}

// allResponses returns all sent messages.
func (p *extPipeline) allResponses() []channel.OutgoingMessage {
	p.Channel.mu.Lock()
	defer p.Channel.mu.Unlock()
	cp := make([]channel.OutgoingMessage, len(p.Channel.sent))
	copy(cp, p.Channel.sent)
	return cp
}

// countRows returns the number of rows in a table.
func (p *extPipeline) countRows(t *testing.T, table string) int {
	t.Helper()
	var count int
	err := p.DB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&count)
	if err != nil {
		t.Fatalf("countRows(%s): %v", table, err)
	}
	return count
}

// querySingleString queries a single string result.
func (p *extPipeline) querySingleString(t *testing.T, query string, args ...any) string {
	t.Helper()
	var result string
	err := p.DB.QueryRowContext(context.Background(), query, args...).Scan(&result)
	if err != nil {
		t.Fatalf("querySingleString: %v", err)
	}
	return result
}

// querySingleInt queries a single int result.
func (p *extPipeline) querySingleInt(t *testing.T, query string, args ...any) int {
	t.Helper()
	var result int
	err := p.DB.QueryRowContext(context.Background(), query, args...).Scan(&result)
	if err != nil {
		t.Fatalf("querySingleInt: %v", err)
	}
	return result
}

// addFakeChannel registers an additional fake channel.
func (p *extPipeline) addFakeChannel(name string) *fakeChannel {
	ch := &fakeChannel{name: name}
	p.Channels.Register(ch)
	return ch
}

// toolCallResponse builds an agent.Response with tool calls.
func toolCallResponse(content string, calls ...agent.ToolCall) agent.Response {
	return agent.Response{
		Content:   content,
		ToolCalls: calls,
		TokensIn:  20,
		TokensOut: 10,
	}
}

// finalResponse builds an agent.Response with just content (no tool calls).
func finalResponse(content string) agent.Response {
	return agent.Response{
		Content:   content,
		TokensIn:  15,
		TokensOut: 8,
	}
}

// tc builds an agent.ToolCall.
func tc(id, name, input string) agent.ToolCall {
	return agent.ToolCall{ID: id, Name: name, Input: input}
}

// stubFinanceProvider is a fake finance.Provider for testing.
type stubFinanceProvider struct{}

func (s *stubFinanceProvider) Quote(_ context.Context, symbol string) (*finance.Quote, error) {
	return &finance.Quote{
		Symbol:    symbol,
		Name:      symbol + " Inc",
		Price:     150.00,
		Currency:  "USD",
		Change:    1.50,
		Timestamp: time.Now(),
	}, nil
}

// multiChannelFakeChannel is a fake channel that can track sent messages separately.
type multiChannelFakeChannel = fakeChannel

// getSent safely returns sent messages for a fakeChannel.
func getSent(ch *fakeChannel) []channel.OutgoingMessage {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	cp := make([]channel.OutgoingMessage, len(ch.sent))
	copy(cp, ch.sent)
	return cp
}

// sentCount safely returns the number of sent messages.
func sentCount(ch *fakeChannel) int {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return len(ch.sent)
}

// waitForResponses waits for a specific number of responses (for concurrent tests).
func waitForResponses(ch *fakeChannel, expected int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ch.mu.Lock()
		n := len(ch.sent)
		ch.mu.Unlock()
		if n >= expected {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// sendMsgWithContext sends a text message using a custom context (for caps injection).
func (p *extPipeline) sendMsgWithContext(t *testing.T, ctx context.Context, text string) {
	t.Helper()
	env := channel.Envelope{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		Channel:   "webchat",
		AccountID: "user-e2e",
		Type:      channel.TypeText,
		Text:      text,
		Timestamp: time.Now(),
	}
	if err := p.Router.HandleMessage(ctx, env); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
}

// localRoundTripper intercepts all HTTP requests and routes them to a local handler.
// This avoids starting a real HTTP server and keeps tests deterministic.
type localRoundTripper struct {
	handler http.Handler
}

func (rt *localRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	rt.handler.ServeHTTP(w, req)
	return w.Result(), nil
}

// concurrentSend sends messages from multiple goroutines and waits for all to complete.
func concurrentSend(t *testing.T, p *extPipeline, messages []struct {
	Channel   string
	AccountID string
	Text      string
}) {
	t.Helper()
	var wg sync.WaitGroup
	for _, m := range messages {
		wg.Add(1)
		go func(ch, acct, text string) {
			defer wg.Done()
			env := channel.Envelope{
				ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
				Channel:   ch,
				AccountID: acct,
				Type:      channel.TypeText,
				Text:      text,
				Timestamp: time.Now(),
			}
			if err := p.Router.HandleMessage(context.Background(), env); err != nil {
				t.Errorf("concurrent HandleMessage(%s/%s): %v", ch, acct, err)
			}
		}(m.Channel, m.AccountID, m.Text)
	}
	wg.Wait()
}
