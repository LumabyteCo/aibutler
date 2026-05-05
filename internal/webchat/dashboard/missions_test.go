package dashboard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/webchat/dashboard"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestMissions_List_Empty(t *testing.T) {
	tdb := testutil.TestDB(t)
	d := dashboard.New(tdb.Conn(), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/missions", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var got []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\nbody: %s", err, w.Body.String())
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %d rows", len(got))
	}
}

func TestMissions_List_FiltersAndStats(t *testing.T) {
	tdb := testutil.TestDB(t)
	conn := tdb.Conn()
	ctx := context.Background()

	// Seed missions in three different states.
	insert := func(id, goal, state, supervisor string, cost float64) {
		t.Helper()
		_, err := conn.ExecContext(ctx,
			`INSERT INTO missions (id, goal, state, budget_usd, cost_so_far_usd,
			                       supervisor_agent_id, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, goal, state, 5.0, cost, supervisor, time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("mis_1", "running mission", "running", "sup_a", 0.50)
	insert("mis_2", "completed mission", "completed", "sup_a", 1.20)
	insert("mis_3", "waiting mission", "waiting_user", "sup_b", 0.10)

	d := dashboard.New(conn, nil, nil)
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	t.Run("default excludes completed", func(t *testing.T) {
		var got []map[string]interface{}
		mustGetJSON(t, srv.URL+"/api/dashboard/missions", &got)
		if len(got) != 2 {
			t.Errorf("default list should have 2 (running + waiting), got %d", len(got))
		}
	})
	t.Run("include_done returns all", func(t *testing.T) {
		var got []map[string]interface{}
		mustGetJSON(t, srv.URL+"/api/dashboard/missions?include_done=true", &got)
		if len(got) != 3 {
			t.Errorf("include_done should have 3, got %d", len(got))
		}
	})
	t.Run("filter by state", func(t *testing.T) {
		var got []map[string]interface{}
		mustGetJSON(t, srv.URL+"/api/dashboard/missions?state=running", &got)
		if len(got) != 1 || got[0]["id"] != "mis_1" {
			t.Errorf("expected mis_1 only, got %+v", got)
		}
	})
	t.Run("filter by supervisor with include_done", func(t *testing.T) {
		var got []map[string]interface{}
		mustGetJSON(t, srv.URL+"/api/dashboard/missions?supervisor=sup_a&include_done=true", &got)
		if len(got) != 2 {
			t.Errorf("expected 2 sup_a missions, got %d", len(got))
		}
	})
	t.Run("limit clamping", func(t *testing.T) {
		var got []map[string]interface{}
		mustGetJSON(t, srv.URL+"/api/dashboard/missions?limit=1&include_done=true", &got)
		if len(got) != 1 {
			t.Errorf("expected limit=1 to return 1 row, got %d", len(got))
		}
	})

	t.Run("stats endpoint", func(t *testing.T) {
		var stats struct {
			Total         int     `json:"total"`
			Active        int     `json:"active"`
			Completed     int     `json:"completed"`
			TotalCostUSD  float64 `json:"total_cost_usd"`
			ActiveCostUSD float64 `json:"active_cost_usd"`
		}
		mustGetJSON(t, srv.URL+"/api/dashboard/missions/stats", &stats)
		if stats.Total != 3 || stats.Active != 2 || stats.Completed != 1 {
			t.Errorf("counts wrong: %+v", stats)
		}
		// Cost: total = 0.50+1.20+0.10 = 1.80; active (running+waiting) = 0.60.
		if stats.TotalCostUSD < 1.79 || stats.TotalCostUSD > 1.81 {
			t.Errorf("TotalCostUSD = %v, want ~1.80", stats.TotalCostUSD)
		}
		if stats.ActiveCostUSD < 0.59 || stats.ActiveCostUSD > 0.61 {
			t.Errorf("ActiveCostUSD = %v, want ~0.60", stats.ActiveCostUSD)
		}
	})
}

func TestMissions_GetDetail_WithStepsAndEvents(t *testing.T) {
	tdb := testutil.TestDB(t)
	conn := tdb.Conn()
	ctx := context.Background()

	_, err := conn.ExecContext(ctx,
		`INSERT INTO missions (id, goal, state, budget_usd, cost_so_far_usd,
		                       supervisor_agent_id, created_at, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"mis_X", "do a thing", "running", 1.0, 0.30, "sup_x",
		time.Now().Add(-30*time.Minute), time.Now().Add(-25*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range []struct{ id, task, state string }{
		{"step_a", "first task", "completed"},
		{"step_b", "second task", "running"},
	} {
		_, err := conn.ExecContext(ctx,
			`INSERT INTO mission_steps (id, mission_id, task, state, created_at)
			 VALUES (?, ?, ?, ?, ?)`,
			s.id, "mis_X", s.task, s.state, time.Now().Add(-20*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
	}

	for _, e := range []struct{ etype, payload string }{
		{"mission.created", `{"detail":"do a thing"}`},
		{"mission.started", ""},
		{"worker.completed", `{"step_id":"step_a"}`},
	} {
		_, err := conn.ExecContext(ctx,
			`INSERT INTO mission_events (mission_id, timestamp, event_type, payload_json)
			 VALUES (?, ?, ?, ?)`,
			"mis_X", time.Now(), e.etype, e.payload)
		if err != nil {
			t.Fatal(err)
		}
	}

	d := dashboard.New(conn, nil, nil)
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	var detail struct {
		Mission struct {
			ID         string `json:"id"`
			State      string `json:"state"`
			StepCount  int    `json:"step_count"`
			EventCount int    `json:"event_count"`
		} `json:"mission"`
		Steps  []map[string]interface{} `json:"steps"`
		Events []map[string]interface{} `json:"events"`
	}
	mustGetJSON(t, srv.URL+"/api/dashboard/missions/mis_X", &detail)

	if detail.Mission.ID != "mis_X" || detail.Mission.State != "running" {
		t.Errorf("mission: %+v", detail.Mission)
	}
	if detail.Mission.StepCount != 2 {
		t.Errorf("StepCount = %d, want 2", detail.Mission.StepCount)
	}
	if detail.Mission.EventCount != 3 {
		t.Errorf("EventCount = %d, want 3", detail.Mission.EventCount)
	}
	if len(detail.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(detail.Steps))
	}
	if len(detail.Events) != 3 {
		t.Errorf("expected 3 events, got %d", len(detail.Events))
	}
}

func TestMissions_GetDetail_NotFound(t *testing.T) {
	tdb := testutil.TestDB(t)
	d := dashboard.New(tdb.Conn(), nil, nil)
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/dashboard/missions/ghost")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestMissions_EventsRoute(t *testing.T) {
	tdb := testutil.TestDB(t)
	conn := tdb.Conn()
	ctx := context.Background()

	_, _ = conn.ExecContext(ctx,
		`INSERT INTO missions (id, goal, state, created_at) VALUES ('mis_E', 'g', 'running', ?)`,
		time.Now())
	_, _ = conn.ExecContext(ctx,
		`INSERT INTO mission_events (mission_id, timestamp, event_type) VALUES ('mis_E', ?, 'evt.one'), ('mis_E', ?, 'evt.two')`,
		time.Now(), time.Now())

	d := dashboard.New(conn, nil, nil)
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	var events []map[string]interface{}
	mustGetJSON(t, srv.URL+"/api/dashboard/missions/mis_E/events", &events)
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
}

func TestMissions_SubRoute_UnknownReturns404(t *testing.T) {
	tdb := testutil.TestDB(t)
	d := dashboard.New(tdb.Conn(), nil, nil)
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/dashboard/missions/anything/unknown-route")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestMissions_PostNotAllowed(t *testing.T) {
	tdb := testutil.TestDB(t)
	d := dashboard.New(tdb.Conn(), nil, nil)
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/dashboard/missions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

// mustGetJSON is a small test helper that GETs `url` and decodes the
// response body into `dest`. Fails the test on any HTTP / JSON error.
func mustGetJSON(t *testing.T, url string, dest interface{}) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status=%d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}
