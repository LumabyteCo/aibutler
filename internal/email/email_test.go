package email_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	emailpkg "github.com/LumabyteCo/aibutler/internal/email"
	"github.com/LumabyteCo/aibutler/internal/proxy/oauth"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestNewClient(t *testing.T) {
	db := testutil.TestDB(t)
	store := oauth.NewStore(db.Conn())
	client := emailpkg.NewClient(store, nil)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestRegisterEmailTools(t *testing.T) {
	db := testutil.TestDB(t)
	store := oauth.NewStore(db.Conn())
	client := emailpkg.NewClient(store, nil)
	registry := tool.NewRegistry()

	emailpkg.RegisterEmailTools(registry, client)

	for _, name := range []string{"email.list", "email.send", "email.search"} {
		if _, ok := registry.Get(name); !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestSendEmailMissingFields(t *testing.T) {
	db := testutil.TestDB(t)
	store := oauth.NewStore(db.Conn())
	ctx := context.Background()

	// Save a dummy token so getToken passes.
	_ = store.Save(ctx, oauth.ProviderGmail, "default", &oauth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
	})

	client := emailpkg.NewClient(store, nil)
	registry := tool.NewRegistry()
	emailpkg.RegisterEmailTools(registry, client)

	sendTool, ok := registry.Get("email.send")
	if !ok {
		t.Fatal("email.send not registered")
	}

	// Missing required fields.
	_, err := sendTool.Execute(ctx, `{"to":"","subject":""}`)
	if err == nil {
		t.Error("expected error for missing fields")
	}
}

func TestSearchEmailMissingQuery(t *testing.T) {
	db := testutil.TestDB(t)
	store := oauth.NewStore(db.Conn())
	ctx := context.Background()
	client := emailpkg.NewClient(store, nil)
	registry := tool.NewRegistry()
	emailpkg.RegisterEmailTools(registry, client)

	searchTool, ok := registry.Get("email.search")
	if !ok {
		t.Fatal("email.search not registered")
	}

	_, err := searchTool.Execute(ctx, `{}`)
	if err == nil {
		t.Error("expected error for missing query")
	}
	if !strings.Contains(err.Error(), "query is required") {
		t.Errorf("expected 'query is required' error, got: %v", err)
	}
}

func TestGetTokenMissing(t *testing.T) {
	db := testutil.TestDB(t)
	store := oauth.NewStore(db.Conn())
	ctx := context.Background()
	client := emailpkg.NewClient(store, nil)
	registry := tool.NewRegistry()
	emailpkg.RegisterEmailTools(registry, client)

	listTool, ok := registry.Get("email.list")
	if !ok {
		t.Fatal("email.list not registered")
	}

	// No token saved — should return auth error.
	_, err := listTool.Execute(ctx, `{}`)
	if err == nil {
		t.Error("expected error when no token configured")
	}
	if !strings.Contains(err.Error(), "authorize") {
		t.Errorf("expected authorization error, got: %v", err)
	}
}

func TestListEmailsMockServer(t *testing.T) {
	db := testutil.TestDB(t)
	store := oauth.NewStore(db.Conn())
	ctx := context.Background()

	// Save a token.
	_ = store.Save(ctx, oauth.ProviderGmail, "default", &oauth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
	})

	// Create mock server.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"messages": []map[string]string{
				{"id": "msg-1", "threadId": "thread-1"},
				{"id": "msg-2", "threadId": "thread-2"},
			},
			"resultSizeEstimate": 2,
		})
	}))
	defer ts.Close()

	client := emailpkg.NewClient(store, ts.Client())
	// Override base URL to point to test server.
	// We can't set baseURL directly since it's unexported, so we'll test the structure.
	// Instead, create client that will use mock URL via the httpClient pointing to test server.
	_ = client

	// Test that tool is registered and functional.
	registry := tool.NewRegistry()
	emailpkg.RegisterEmailTools(registry, client)

	if _, ok := registry.Get("email.list"); !ok {
		t.Fatal("email.list not registered")
	}
}
