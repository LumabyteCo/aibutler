package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Release describes an available software release.
type Release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Notes   string `json:"notes"`
}

// Updater checks for new versions periodically.
type Updater struct {
	currentVersion string
	checkURL       string
	checkInterval  time.Duration
	httpClient     *http.Client

	mu      sync.Mutex
	stopCh  chan struct{}
	running bool
	latest  *Release
}

// New creates an Updater.
func New(currentVersion, checkURL string, interval time.Duration) *Updater {
	return &Updater{
		currentVersion: currentVersion,
		checkURL:       checkURL,
		checkInterval:  interval,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
	}
}

// SetHTTPClient replaces the default HTTP client (for testing).
func (u *Updater) SetHTTPClient(c *http.Client) {
	u.httpClient = c
}

// Start begins the periodic version check goroutine.
func (u *Updater) Start(ctx context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.running {
		return fmt.Errorf("updater: already started")
	}
	u.stopCh = make(chan struct{})
	u.running = true

	go u.checkLoop(ctx)
	return nil
}

// Stop halts the periodic check goroutine.
func (u *Updater) Stop() error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if !u.running {
		return nil
	}
	close(u.stopCh)
	u.running = false
	return nil
}

// Check performs a manual version check against the check URL.
func (u *Updater) Check(ctx context.Context) (*Release, error) {
	if u.checkURL == "" {
		return nil, fmt.Errorf("updater: no check URL configured")
	}

	if !strings.HasPrefix(u.checkURL, "https://") {
		log.Printf("WARNING: update check URL %q uses unencrypted HTTP — updates may be tampered", u.checkURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.checkURL, nil)
	if err != nil {
		return nil, fmt.Errorf("updater: create request: %w", err)
	}

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("updater: check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("updater: server returned %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("updater: decode: %w", err)
	}

	u.mu.Lock()
	u.latest = &rel
	u.mu.Unlock()

	return &rel, nil
}

// Download fetches a release binary and verifies its SHA-256 hash.
func (u *Updater) Download(ctx context.Context, rel *Release, destPath string) error {
	if rel == nil || rel.URL == "" {
		return fmt.Errorf("updater: no release URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.URL, nil)
	if err != nil {
		return fmt.Errorf("updater: download request: %w", err)
	}

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("updater: download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("updater: download returned %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("updater: create file: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("updater: write: %w", err)
	}

	if rel.SHA256 != "" {
		actual := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(actual, rel.SHA256) {
			os.Remove(destPath)
			return fmt.Errorf("updater: hash mismatch: got %s, want %s", actual, rel.SHA256)
		}
	}

	return nil
}

// IsNewer compares two semantic version strings (v1.2.3 format).
// Returns true if remote is newer than current.
func IsNewer(current, remote string) bool {
	current = strings.TrimPrefix(current, "v")
	remote = strings.TrimPrefix(remote, "v")

	cParts := strings.Split(current, ".")
	rParts := strings.Split(remote, ".")

	for i := 0; i < 3; i++ {
		var c, r int
		if i < len(cParts) {
			fmt.Sscanf(cParts[i], "%d", &c)
		}
		if i < len(rParts) {
			fmt.Sscanf(rParts[i], "%d", &r)
		}
		if r > c {
			return true
		}
		if r < c {
			return false
		}
	}
	return false
}

// Latest returns the most recently fetched release, or nil.
func (u *Updater) Latest() *Release {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.latest
}

func (u *Updater) checkLoop(ctx context.Context) {
	// Check immediately.
	if rel, err := u.Check(ctx); err == nil && IsNewer(u.currentVersion, rel.Version) {
		log.Printf("updater: new version available: %s (current: %s)", rel.Version, u.currentVersion)
	}

	ticker := time.NewTicker(u.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-u.stopCh:
			return
		case <-ticker.C:
			if rel, err := u.Check(ctx); err == nil && IsNewer(u.currentVersion, rel.Version) {
				log.Printf("updater: new version available: %s (current: %s)", rel.Version, u.currentVersion)
			}
		}
	}
}
