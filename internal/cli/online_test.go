package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dpologdvinity/depwhy/internal/health"
)

// fakeOnline is a local stand-in for OSV and the three registries. Its data
// matches the shared fixtures: d has an advisory, a (npm) is deprecated, c@2.0.0
// has an advisory, and uv's c and cargo's c@1.0.0 are yanked.
type fakeOnline struct {
	osvFail  bool
	requests atomic.Int32
}

// useFakeOnline points --online at a fresh fake service with an empty cache, for
// the duration of the test. The returned value counts requests.
func useFakeOnline(t *testing.T) *fakeOnline {
	t.Helper()
	f := &fakeOnline{}
	mux := http.NewServeMux()
	mux.HandleFunc("/osv/v1/querybatch", func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if f.osvFail {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		var req struct {
			Queries []struct {
				Package struct{ Name, Ecosystem string }
				Version string
			}
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var results []string
		for _, q := range req.Queries {
			id := ""
			switch {
			case q.Package.Name == "d" && q.Package.Ecosystem == "npm":
				id = `{"id":"GHSA-dddd-0001"}`
			case q.Package.Name == "c" && q.Version == "2.0.0":
				id = `{"id":"RUSTSEC-0000-0002"},{"id":"GHSA-cccc-0002"}`
			}
			results = append(results, fmt.Sprintf(`{"vulns":[%s]}`, id))
		}
		fmt.Fprintf(w, `{"results":[%s]}`, strings.Join(results, ","))
	})
	mux.HandleFunc("/npm/", func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if r.URL.Path == "/npm/a/1.0.0" {
			fmt.Fprint(w, `{"deprecated":"use b instead"}`)
			return
		}
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/pypi/", func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		fmt.Fprintf(w, `{"info":{"yanked":%t}}`, r.URL.Path == "/pypi/c/1.0.0/json")
	})
	mux.HandleFunc("/crates/", func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		fmt.Fprintf(w, `{"version":{"yanked":%t}}`, r.URL.Path == "/crates/c/1.0.0")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	old := healthClient
	healthClient = func(refresh bool) *health.Client {
		return &health.Client{
			HTTP: srv.Client(), OSVURL: srv.URL + "/osv", NPMURL: srv.URL + "/npm",
			PyPIURL: srv.URL + "/pypi", CratesURL: srv.URL + "/crates", UserAgent: "depwhy/test",
			Cache: &health.Cache{Dir: cacheDir, TTL: 24 * time.Hour, Now: time.Now}, Refresh: refresh,
		}
	}
	t.Cleanup(func() { healthClient = old })
	return f
}

// TestOnlineGolden runs annotated output against the fake service and compares it
// with golden files.
func TestOnlineGolden(t *testing.T) {
	useFakeOnline(t)
	cases := []struct {
		fixture string
		args    []string
	}{
		{"npm", []string{"--online", "d"}},
		{"npm", []string{"--online", "--remove", "a"}},
		{"npm", []string{"--online", "--dupes"}},
		{"npm", []string{"--online", "--json", "d"}},
		{"uv", []string{"--online", "c"}},
		{"cargo", []string{"--online", "--dupes"}},
	}
	for _, c := range cases {
		var parts []string
		for _, a := range c.args {
			parts = append(parts, nonAlnum.ReplaceAllString(a, ""))
		}
		name := c.fixture + "__" + strings.Join(parts, "_") + ".txt"
		t.Run(name, func(t *testing.T) {
			dir := stageFixture(t, project(c.fixture))
			code, out, errs := run(append([]string{"-C", dir, "--no-color"}, c.args...)...)
			checkGolden(t, name, fmt.Sprintf("%s\n--- stderr\n%s\n--- exit=%d\n", out, errs, code))
		})
	}
}

// TestOfflineNeverTouchesTheNetwork checks that without --online no request is
// made and the output has no findings.
func TestOfflineNeverTouchesTheNetwork(t *testing.T) {
	f := useFakeOnline(t)
	code, out, _ := run("-C", project("npm"), "--no-color", "d")
	if code != 0 || f.requests.Load() != 0 || strings.Contains(out, "⚠") {
		t.Fatalf("code=%d requests=%d out=%q", code, f.requests.Load(), out)
	}
}

// TestOnlineOutageIsAWarning checks that a failing service gives a warning, still
// prints the result, and leaves the exit code at 0.
func TestOnlineOutageIsAWarning(t *testing.T) {
	f := useFakeOnline(t)
	f.osvFail = true
	code, out, errs := run("-C", project("npm"), "--no-color", "--online", "d")
	if code != 0 || !strings.Contains(out, "d@1.0.0  npm") || strings.Contains(out, "⚠") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(errs, "depwhy: warning: online lookup failed: OSV advisories: ") {
		t.Fatalf("stderr = %q", errs)
	}
	code, out, _ = run("-C", project("npm"), "--no-color", "--online", "--json", "d")
	if code != 0 || !strings.Contains(out, "online lookup failed") {
		t.Fatalf("JSON warnings: code=%d out=%q", code, out)
	}
}

// TestOnlineCacheAndRefresh checks that a repeat run is served from the cache and
// --refresh goes back to the network.
func TestOnlineCacheAndRefresh(t *testing.T) {
	f := useFakeOnline(t)
	args := []string{"-C", project("npm"), "--no-color", "--online", "d"}
	run(args...)
	first := f.requests.Load()
	if first == 0 {
		t.Fatal("first run made no requests")
	}
	run(args...)
	if f.requests.Load() != first {
		t.Fatalf("second run must use the cache: %d -> %d", first, f.requests.Load())
	}
	run(append(args, "--refresh")...)
	if f.requests.Load() != 2*first {
		t.Fatalf("--refresh must repeat the lookups: %d -> %d", first, f.requests.Load())
	}
}

// TestRefreshNeedsOnline checks the usage error from SPEC §12.2.
func TestRefreshNeedsOnline(t *testing.T) {
	code, _, errs := run("--refresh", "x")
	if code != 2 || !strings.Contains(errs, "--refresh needs --online") {
		t.Fatalf("got %d %q", code, errs)
	}
}

// TestOnlineNotFoundMakesNoRequests checks that nothing is looked up when the
// query matches nothing.
func TestOnlineNotFoundMakesNoRequests(t *testing.T) {
	f := useFakeOnline(t)
	code, _, _ := run("-C", project("npm"), "--online", "zzz")
	if code != 1 || f.requests.Load() != 0 {
		t.Fatalf("code=%d requests=%d", code, f.requests.Load())
	}
}
