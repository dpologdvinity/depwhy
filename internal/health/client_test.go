package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServices is a local stand-in for OSV, npm, PyPI and crates.io. Behavior is
// controlled through the exported-looking fields; counters record the traffic.
type fakeServices struct {
	srv *httptest.Server

	osvFail   bool                // answer OSV with HTTP 500
	advisory  map[string][]string // package name -> advisory IDs
	npm       map[string]string   // "name/version" -> deprecation message
	pypi      map[string]bool     // "name/version" -> yanked
	cargo     map[string]bool     // "name/version" -> yanked
	delay     time.Duration       // sleep before every registry answer
	osvCalls  atomic.Int32        // querybatch requests
	regCalls  atomic.Int32        // registry requests
	inFlight  atomic.Int32        // registry requests being served right now
	maxFlight atomic.Int32        // highest value inFlight reached
	uaSeen    sync.Map            // User-Agent values seen
	rawPaths  []string            // raw request paths of registry requests
	mu        sync.Mutex
}

// newFake starts the services and returns a client wired to them (no cache).
func newFake(t *testing.T) (*fakeServices, *Client) {
	t.Helper()
	f := &fakeServices{advisory: map[string][]string{}, npm: map[string]string{}, pypi: map[string]bool{}, cargo: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/osv/v1/querybatch", f.serveOSV)
	mux.HandleFunc("/npm/", f.serveNPM)
	mux.HandleFunc("/pypi/", f.servePyPI)
	mux.HandleFunc("/crates/", f.serveCrates)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f, &Client{
		HTTP: f.srv.Client(), OSVURL: f.srv.URL + "/osv", NPMURL: f.srv.URL + "/npm",
		PyPIURL: f.srv.URL + "/pypi", CratesURL: f.srv.URL + "/crates", UserAgent: "depwhy/test",
	}
}

// registry records one registry request and applies the configured delay.
func (f *fakeServices) registry(r *http.Request) func() {
	f.regCalls.Add(1)
	f.uaSeen.Store(r.Header.Get("User-Agent"), true)
	f.mu.Lock()
	f.rawPaths = append(f.rawPaths, r.URL.EscapedPath())
	f.mu.Unlock()
	n := f.inFlight.Add(1)
	for {
		m := f.maxFlight.Load()
		if n <= m || f.maxFlight.CompareAndSwap(m, n) {
			break
		}
	}
	time.Sleep(f.delay)
	return func() { f.inFlight.Add(-1) }
}

func (f *fakeServices) serveOSV(w http.ResponseWriter, r *http.Request) {
	f.osvCalls.Add(1)
	if f.osvFail {
		http.Error(w, "down", http.StatusInternalServerError)
		return
	}
	var req osvRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var parts []string
	for _, q := range req.Queries {
		var vulns []string
		for _, id := range f.advisory[q.Package.Name] {
			vulns = append(vulns, fmt.Sprintf(`{"id":%q}`, id))
		}
		parts = append(parts, fmt.Sprintf(`{"vulns":[%s]}`, strings.Join(vulns, ",")))
	}
	fmt.Fprintf(w, `{"results":[%s]}`, strings.Join(parts, ","))
}

func (f *fakeServices) serveNPM(w http.ResponseWriter, r *http.Request) {
	defer f.registry(r)()
	key := strings.TrimPrefix(r.URL.Path, "/npm/") // decoded: "@x/y/1.0.0"
	if msg, ok := f.npm[key]; ok {
		fmt.Fprintf(w, `{"name":"x","deprecated":%q}`, msg)
		return
	}
	if strings.HasPrefix(key, "known/") {
		fmt.Fprint(w, `{"name":"known"}`)
		return
	}
	http.NotFound(w, r)
}

func (f *fakeServices) servePyPI(w http.ResponseWriter, r *http.Request) {
	defer f.registry(r)()
	key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/pypi/"), "/json")
	if y, ok := f.pypi[key]; ok {
		fmt.Fprintf(w, `{"info":{"yanked":%t}}`, y)
		return
	}
	http.NotFound(w, r)
}

func (f *fakeServices) serveCrates(w http.ResponseWriter, r *http.Request) {
	defer f.registry(r)()
	key := strings.TrimPrefix(r.URL.Path, "/crates/")
	if y, ok := f.cargo[key]; ok {
		fmt.Fprintf(w, `{"version":{"yanked":%t}}`, y)
		return
	}
	http.NotFound(w, r)
}

// decodeJSON decodes a request body.
func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// TestStatusPerEcosystem checks the three registry lookups, scoped npm names
// (the slash must be sent escaped) and that 404 means "no information".
func TestStatusPerEcosystem(t *testing.T) {
	f, c := newFake(t)
	f.npm["@x/y/1.0.0"] = "use z instead"
	f.pypi["old/1.0.0"] = true
	f.cargo["gone/0.1.0"] = true
	ctx := context.Background()

	info, err := c.status(ctx, Pkg{"npm", "@x/y", "1.0.0"})
	if err != nil || info.Deprecated != "use z instead" {
		t.Fatalf("npm: %+v %v", info, err)
	}
	if got := f.rawPaths[0]; got != "/npm/@x%2Fy/1.0.0" {
		t.Fatalf("scoped name must be escaped, got %q", got)
	}
	if info, err = c.status(ctx, Pkg{"python", "old", "1.0.0"}); err != nil || !info.Yanked {
		t.Fatalf("pypi: %+v %v", info, err)
	}
	if info, err = c.status(ctx, Pkg{"cargo", "gone", "0.1.0"}); err != nil || !info.Yanked {
		t.Fatalf("cargo: %+v %v", info, err)
	}
	if info, err = c.status(ctx, Pkg{"cargo", "unknown", "9.9.9"}); err != nil || !reflect.DeepEqual(info, Info{}) {
		t.Fatalf("404 must be empty and not an error: %+v %v", info, err)
	}
	if info, err = c.status(ctx, Pkg{"npm", "known", "1.0.0"}); err != nil || info.Deprecated != "" {
		t.Fatalf("not deprecated: %+v %v", info, err)
	}
	if _, ok := f.uaSeen.Load("depwhy/test"); !ok {
		t.Fatal("User-Agent header missing")
	}
}

// TestLookupCombinesAndCaches checks the full path: advisories and status are
// merged, unlookable versions are skipped, and a second run is served from the
// cache with no network traffic, until --refresh.
func TestLookupCombinesAndCaches(t *testing.T) {
	f, c := newFake(t)
	now := time.Now()
	c.Cache = &Cache{Dir: t.TempDir(), TTL: 24 * time.Hour, Now: func() time.Time { return now }}
	f.advisory["lodash"] = []string{"GHSA-1"}
	f.npm["lodash/4.0.0"] = "old"
	f.cargo["time/0.1.0"] = true
	pkgs := []Pkg{
		{"npm", "lodash", "4.0.0"}, {"cargo", "time", "0.1.0"},
		{"npm", "lodash", "4.0.0"},        // duplicate
		{"npm", "local", "file:../local"}, // not a registry version
		{"npm", "novers", ""},             // no version
	}
	got, warns := c.Lookup(context.Background(), pkgs)
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	want := map[Pkg]Info{
		{"npm", "lodash", "4.0.0"}: {Advisories: []string{"GHSA-1"}, Deprecated: "old"},
		{"cargo", "time", "0.1.0"}: {Yanked: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if f.osvCalls.Load() != 1 || f.regCalls.Load() != 2 {
		t.Fatalf("osv=%d registry=%d", f.osvCalls.Load(), f.regCalls.Load())
	}

	got, _ = c.Lookup(context.Background(), pkgs)
	if !reflect.DeepEqual(got, want) || f.osvCalls.Load() != 1 || f.regCalls.Load() != 2 {
		t.Fatalf("second run must come from the cache: osv=%d reg=%d", f.osvCalls.Load(), f.regCalls.Load())
	}

	c.Refresh = true
	c.Lookup(context.Background(), pkgs)
	if f.osvCalls.Load() != 2 || f.regCalls.Load() != 4 {
		t.Fatalf("--refresh must hit the network: osv=%d reg=%d", f.osvCalls.Load(), f.regCalls.Load())
	}
	c.Refresh = false
	now = now.Add(25 * time.Hour)
	c.Lookup(context.Background(), pkgs)
	if f.osvCalls.Load() != 3 {
		t.Fatalf("expired entries must be refetched: osv=%d", f.osvCalls.Load())
	}
}

// TestLookupWarnsAndDoesNotCacheFailures checks that an OSV outage becomes one
// warning, status lookups still work, and the incomplete answer is not cached.
func TestLookupWarnsAndDoesNotCacheFailures(t *testing.T) {
	f, c := newFake(t)
	c.Cache = &Cache{Dir: t.TempDir(), TTL: time.Hour, Now: time.Now}
	f.osvFail = true
	f.npm["a/1.0.0"] = "dep"
	pkgs := []Pkg{{"npm", "a", "1.0.0"}, {"npm", "b", "1.0.0"}}

	got, warns := c.Lookup(context.Background(), pkgs)
	if len(warns) != 1 || !strings.HasPrefix(warns[0], "online lookup failed: OSV advisories: ") {
		t.Fatalf("warnings = %v", warns)
	}
	if got[pkgs[0]].Deprecated != "dep" {
		t.Fatalf("status must survive an OSV outage: %+v", got)
	}
	f.osvFail = false
	f.advisory["a"] = []string{"GHSA-9"}
	got, warns = c.Lookup(context.Background(), pkgs)
	if len(warns) != 0 || !reflect.DeepEqual(got[pkgs[0]].Advisories, []string{"GHSA-9"}) {
		t.Fatalf("failed lookups must not be cached: %v %+v", warns, got)
	}
}

// TestLookupAggregatesRegistryFailures checks that many failing registry calls
// give one warning per registry, not one per package.
func TestLookupAggregatesRegistryFailures(t *testing.T) {
	_, c := newFake(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close() // its address now refuses connections immediately
	c.NPMURL = dead.URL
	pkgs := []Pkg{{"npm", "a", "1.0.0"}, {"npm", "b", "1.0.0"}, {"npm", "c", "1.0.0"}}
	_, warns := c.Lookup(context.Background(), pkgs)
	if len(warns) != 1 || !strings.HasPrefix(warns[0], "online lookup failed: npm registry (3 packages): ") {
		t.Fatalf("warnings = %v", warns)
	}
}

// TestLookupTimesOut checks the per-request timeout turns a hung service into a warning.
func TestLookupTimesOut(t *testing.T) {
	f, c := newFake(t)
	f.delay = 600 * time.Millisecond
	c.Timeout = 100 * time.Millisecond
	_, warns := c.Lookup(context.Background(), []Pkg{{"python", "slow", "1.0.0"}})
	// A loaded machine could also time out the fast OSV call, so look for the PyPI warning.
	if !strings.Contains(strings.Join(warns, "\n"), "online lookup failed: PyPI (1 package): ") {
		t.Fatalf("warnings = %v", warns)
	}
}

// TestLookupLimitsConcurrency checks that no more than Concurrency registry
// requests are in flight, and that requests really do overlap.
func TestLookupLimitsConcurrency(t *testing.T) {
	f, c := newFake(t)
	f.delay = 20 * time.Millisecond
	c.Concurrency = 3
	var pkgs []Pkg
	for i := 0; i < 12; i++ {
		pkgs = append(pkgs, Pkg{"cargo", fmt.Sprintf("c%d", i), "1.0.0"})
	}
	c.Lookup(context.Background(), pkgs)
	if m := f.maxFlight.Load(); m > 3 || m < 2 {
		t.Fatalf("max in flight = %d, want 2..3", m)
	}
}

// TestLookupWithNothingToDo checks that no request is made for an empty list.
func TestLookupWithNothingToDo(t *testing.T) {
	f, c := newFake(t)
	got, warns := c.Lookup(context.Background(), []Pkg{{"npm", "x", ""}})
	if len(got) != 0 || len(warns) != 0 || f.osvCalls.Load() != 0 {
		t.Fatalf("got %v %v osv=%d", got, warns, f.osvCalls.Load())
	}
}
