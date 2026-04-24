package calendar_test

import (
	"context"
	"strings"
	"testing"

	calpkg "github.com/LumabyteCo/aibutler/internal/calendar"
	"github.com/LumabyteCo/aibutler/internal/proxy/oauth"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestNewClient(t *testing.T) {
	db := testutil.TestDB(t)
	store := oauth.NewStore(db.Conn())
	client := calpkg.NewClient(store, nil)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestRegisterCalendarTools(t *testing.T) {
	db := testutil.TestDB(t)
	store := oauth.NewStore(db.Conn())
	client := calpkg.NewClient(store, nil)
	registry := tool.NewRegistry()

	calpkg.RegisterCalendarTools(registry, client)

	for _, name := range []string{"calendar.list_events", "calendar.create_event", "calendar.delete_event"} {
		if _, ok := registry.Get(name); !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestGetTokenMissing(t *testing.T) {
	db := testutil.TestDB(t)
	store := oauth.NewStore(db.Conn())
	ctx := context.Background()
	client := calpkg.NewClient(store, nil)
	registry := tool.NewRegistry()
	calpkg.RegisterCalendarTools(registry, client)

	listTool, ok := registry.Get("calendar.list_events")
	if !ok {
		t.Fatal("calendar.list_events not registered")
	}

	_, err := listTool.Execute(ctx, `{}`)
	if err == nil {
		t.Error("expected error when no token configured")
	}
	if !strings.Contains(err.Error(), "authorize") {
		t.Errorf("expected authorization error, got: %v", err)
	}
}

func TestCreateEventMissingFields(t *testing.T) {
	db := testutil.TestDB(t)
	store := oauth.NewStore(db.Conn())
	ctx := context.Background()

	_ = store.Save(ctx, oauth.ProviderGoogleCalendar, "default", &oauth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
	})

	client := calpkg.NewClient(store, nil)
	registry := tool.NewRegistry()
	calpkg.RegisterCalendarTools(registry, client)

	createTool, ok := registry.Get("calendar.create_event")
	if !ok {
		t.Fatal("calendar.create_event not registered")
	}

	_, err := createTool.Execute(ctx, `{"title":""}`)
	if err == nil {
		t.Error("expected error for missing title/start/end")
	}
}

func TestDeleteEventMissingID(t *testing.T) {
	db := testutil.TestDB(t)
	store := oauth.NewStore(db.Conn())
	ctx := context.Background()

	_ = store.Save(ctx, oauth.ProviderGoogleCalendar, "default", &oauth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
	})

	client := calpkg.NewClient(store, nil)
	registry := tool.NewRegistry()
	calpkg.RegisterCalendarTools(registry, client)

	deleteTool, ok := registry.Get("calendar.delete_event")
	if !ok {
		t.Fatal("calendar.delete_event not registered")
	}

	_, err := deleteTool.Execute(ctx, `{}`)
	if err == nil {
		t.Error("expected error for missing event_id")
	}
	if !strings.Contains(err.Error(), "event_id is required") {
		t.Errorf("expected 'event_id is required' error, got: %v", err)
	}
}

func TestCreateEventSuccess(t *testing.T) {
	db := testutil.TestDB(t)
	store := oauth.NewStore(db.Conn())
	ctx := context.Background()

	_ = store.Save(ctx, oauth.ProviderGoogleCalendar, "default", &oauth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
	})

	client := calpkg.NewClient(store, nil)
	registry := tool.NewRegistry()
	calpkg.RegisterCalendarTools(registry, client)

	createTool, _ := registry.Get("calendar.create_event")
	result, err := createTool.Execute(ctx, `{"title":"Meeting","start":"2026-01-01T10:00:00Z","end":"2026-01-01T11:00:00Z"}`)
	if err != nil {
		t.Fatalf("createEvent: %v", err)
	}
	if !strings.Contains(result, "created") {
		t.Errorf("expected 'created' in result, got: %s", result)
	}
}

func TestDeleteEventSuccess(t *testing.T) {
	db := testutil.TestDB(t)
	store := oauth.NewStore(db.Conn())
	ctx := context.Background()

	_ = store.Save(ctx, oauth.ProviderGoogleCalendar, "default", &oauth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
	})

	client := calpkg.NewClient(store, nil)
	registry := tool.NewRegistry()
	calpkg.RegisterCalendarTools(registry, client)

	deleteTool, _ := registry.Get("calendar.delete_event")
	result, err := deleteTool.Execute(ctx, `{"event_id":"event-123"}`)
	if err != nil {
		t.Fatalf("deleteEvent: %v", err)
	}
	if !strings.Contains(result, "deleted") {
		t.Errorf("expected 'deleted' in result, got: %s", result)
	}
}
