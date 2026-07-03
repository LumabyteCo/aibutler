package dashboard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/webchat/dashboard"
	"github.com/LumabyteCo/aibutler/testutil"
)

func newFactsServer(t *testing.T) (*httptest.Server, *memory.Store) {
	t.Helper()
	db := testutil.TestDB(t)
	store := memory.NewStore(db.Conn())
	d := dashboard.New(db.Conn(), nil, nil)
	d.SetFactStore(store, nil)
	srv := httptest.NewServer(d.Handler())
	t.Cleanup(srv.Close)
	return srv, store
}

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestFactsEndpointsLifecycle(t *testing.T) {
	srv, store := newFactsServer(t)
	ctx := context.Background()

	id, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Cairo", Category: "identity", FactKey: "user.location",
	})
	if err != nil {
		t.Fatal(err)
	}

	// List shows the fact.
	resp, err := http.Get(srv.URL + "/api/dashboard/memory/facts")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var listBody struct {
		Facts []memory.KeyFact `json:"facts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Facts) != 1 || listBody.Facts[0].ID != id {
		t.Fatalf("list = %+v, want the saved fact", listBody.Facts)
	}

	// Correct it.
	resp = postJSON(t, srv.URL+"/api/dashboard/memory/facts/correct",
		`{"id":`+jsonID(id)+`,"fact":"User lives in Giza"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("correct status = %d", resp.StatusCode)
	}

	// Pin the new fact.
	facts, _ := store.GetKeyFacts(ctx, "identity", 10)
	if len(facts) != 1 {
		t.Fatalf("expected 1 active fact after correction, got %d", len(facts))
	}
	resp = postJSON(t, srv.URL+"/api/dashboard/memory/facts/pin",
		`{"id":`+jsonID(facts[0].ID)+`,"pinned":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pin status = %d", resp.StatusCode)
	}

	// Forget it.
	resp = postJSON(t, srv.URL+"/api/dashboard/memory/facts/forget",
		`{"fact_id":`+jsonID(facts[0].ID)+`}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("forget status = %d", resp.StatusCode)
	}
}

func TestFactsEndpointsRejectNonJSONPost(t *testing.T) {
	srv, _ := newFactsServer(t)
	// A cross-origin page can send text/plain without a CORS preflight; the
	// destructive endpoints must refuse it.
	resp, err := http.Post(srv.URL+"/api/dashboard/memory/facts/forget",
		"text/plain", strings.NewReader(`{"fact_id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
}

func TestFactsEndpointsMethodAndValidation(t *testing.T) {
	srv, _ := newFactsServer(t)

	// GET on a mutating endpoint.
	resp, err := http.Get(srv.URL + "/api/dashboard/memory/facts/forget")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET forget status = %d, want 405", resp.StatusCode)
	}

	// Both ids or neither id.
	resp = postJSON(t, srv.URL+"/api/dashboard/memory/facts/forget", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty forget status = %d, want 400", resp.StatusCode)
	}
	resp = postJSON(t, srv.URL+"/api/dashboard/memory/facts/forget", `{"fact_id":1,"thought_id":2}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("double forget status = %d, want 400", resp.StatusCode)
	}

	// Missing rows are 404, not 500.
	resp = postJSON(t, srv.URL+"/api/dashboard/memory/facts/forget", `{"fact_id":99999}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing fact status = %d, want 404", resp.StatusCode)
	}
	resp = postJSON(t, srv.URL+"/api/dashboard/memory/facts/pin", `{"id":99999,"pinned":true}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing pin status = %d, want 404", resp.StatusCode)
	}
}

func TestConflictReviewAndRestoreEndpoints(t *testing.T) {
	srv, store := newFactsServer(t)
	ctx := context.Background()

	oldID, _ := store.SaveFact(ctx, memory.FactInput{
		Fact: "User works at LumaByte", Category: "identity",
		FactKey: "user.employer", Confidence: 0.95,
	})
	if _, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User works at Initech", Category: "identity",
		FactKey: "user.employer", Confidence: memory.ConfidenceToolOutput,
	}); err != nil {
		t.Fatal(err)
	}

	// The queue lists exactly the flagged replacement.
	resp, err := http.Get(srv.URL + "/api/dashboard/memory/conflicts?unreviewed=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Conflicts []memory.Conflict `json:"conflicts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Conflicts) != 1 {
		t.Fatalf("pending conflicts = %d, want 1", len(body.Conflicts))
	}

	// Restore the old value.
	resp = postJSON(t, srv.URL+"/api/dashboard/memory/conflicts/restore",
		`{"id":`+jsonID(body.Conflicts[0].ID)+`}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restore status = %d", resp.StatusCode)
	}
	facts, _ := store.GetKeyFacts(ctx, "identity", 10)
	if len(facts) != 1 || facts[0].ID != oldID {
		t.Fatalf("expected restored old fact active, got %+v", facts)
	}
}

func TestFactsEndpointsWithoutStoreReturn503(t *testing.T) {
	db := testutil.TestDB(t)
	d := dashboard.New(db.Conn(), nil, nil)
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/dashboard/memory/facts")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func jsonID(id int64) string {
	b, _ := json.Marshal(id)
	return string(b)
}
