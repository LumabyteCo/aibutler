package permissions

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, _, _, _ string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

func TestRegisterTool(t *testing.T) {
	reg := newMockRegistry()
	RegisterTool(reg)
	if _, ok := reg.exec["permissions.check"]; !ok {
		t.Fatal("permissions.check not registered")
	}
}

func TestRegisteredTool_ReturnsValidJSON(t *testing.T) {
	reg := newMockRegistry()
	RegisterTool(reg)
	tool := reg.exec["permissions.check"]

	out, err := tool(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("tool exec: %v", err)
	}
	var report Report
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("output isn't valid JSON Report: %v\noutput: %s", err, out)
	}
	if report.Platform != runtime.GOOS {
		t.Errorf("Platform = %q, want %q", report.Platform, runtime.GOOS)
	}
	if len(report.Permissions) == 0 {
		t.Errorf("expected at least one permission entry, got 0")
	}
	if report.Summary == "" {
		t.Errorf("expected non-empty summary")
	}
}

func TestSummarize(t *testing.T) {
	cases := []struct {
		name   string
		perms  []Permission
		want   string // substring that must appear in summary
	}{
		{
			name:  "empty",
			perms: nil,
			want:  "no permission",
		},
		{
			name: "all granted",
			perms: []Permission{
				{Status: StatusGranted}, {Status: StatusGranted},
			},
			want: "2 of 2 permissions granted",
		},
		{
			name: "mixed with denied",
			perms: []Permission{
				{Status: StatusGranted}, {Status: StatusDenied}, {Status: StatusGranted},
			},
			want: "settings_url",
		},
		{
			name: "all unknown — no settings_url hint",
			perms: []Permission{
				{Status: StatusUnknown}, {Status: StatusUnknown},
			},
			want: "0 of 2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summarize(tc.perms)
			if !strings.Contains(got, tc.want) {
				t.Errorf("summarize(%s) = %q, want to contain %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestCheck_PlatformMatchesGOOS(t *testing.T) {
	r := Check(context.Background())
	if r.Platform != runtime.GOOS {
		t.Errorf("Platform = %q, want %q", r.Platform, runtime.GOOS)
	}
}

func TestCheck_NonDarwin_ReturnsNotApplicable(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin path tested off macOS")
	}
	r := Check(context.Background())
	if len(r.Permissions) != 1 {
		t.Errorf("non-darwin Report should have exactly 1 not-applicable entry, got %d", len(r.Permissions))
	}
	if r.Permissions[0].Status != StatusNotApplicable {
		t.Errorf("expected StatusNotApplicable, got %q", r.Permissions[0].Status)
	}
}

func TestCheck_Darwin_HasExpectedPermissions(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin permission set tested on macOS")
	}
	r := Check(context.Background())

	// Expect entries for at least: Automation: System Events, Automation:
	// Finder, Screen Recording.
	wantNames := []string{"Automation: System Events", "Automation: Finder", "Screen Recording"}
	for _, want := range wantNames {
		found := false
		for _, p := range r.Permissions {
			if p.Name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a permission named %q in the macOS report; got: %+v", want, r.Permissions)
		}
	}
}

func TestCheck_Darwin_AllEntriesHaveActionableFields(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("actionable-field check tested on macOS")
	}
	r := Check(context.Background())
	for _, p := range r.Permissions {
		if p.Why == "" {
			t.Errorf("permission %q missing Why", p.Name)
		}
		if p.SettingsURL == "" {
			t.Errorf("permission %q missing SettingsURL", p.Name)
		}
		if p.HowToGrant == "" {
			t.Errorf("permission %q missing HowToGrant", p.Name)
		}
	}
}
