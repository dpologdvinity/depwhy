package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

// osvRequest is the body depwhy sends to OSV.
type osvRequest struct {
	Queries []struct {
		Package struct{ Name, Ecosystem string }
		Version string
	}
}

// TestAdvisoriesMapsEcosystemsAndResults checks the request shape (names,
// versions, OSV ecosystem ids, User-Agent) and that results are matched to
// packages by position, sorted and de-duplicated.
func TestAdvisoriesMapsEcosystemsAndResults(t *testing.T) {
	var got osvRequest
	var ua, method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, method, path = r.Header.Get("User-Agent"), r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"results":[{"vulns":[{"id":"GHSA-b"},{"id":"GHSA-a"},{"id":"GHSA-a"}]},{},{"vulns":[{"id":"RUSTSEC-1"}]}]}`)
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), OSVURL: srv.URL, UserAgent: "depwhy/test"}
	pkgs := []Pkg{{"npm", "lodash", "4.0.0"}, {"python", "requests", "2.0.0"}, {"cargo", "time", "0.1.0"}}

	res, err := c.advisories(context.Background(), pkgs)
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/v1/querybatch" || ua != "depwhy/test" {
		t.Fatalf("method=%s path=%s ua=%s", method, path, ua)
	}
	wantEco := []string{"npm", "PyPI", "crates.io"}
	for i, q := range got.Queries {
		if q.Package.Ecosystem != wantEco[i] || q.Package.Name != pkgs[i].Name || q.Version != pkgs[i].Version {
			t.Errorf("query %d = %+v", i, q)
		}
	}
	if !reflect.DeepEqual(res[pkgs[0]], []string{"GHSA-a", "GHSA-b"}) || len(res[pkgs[1]]) != 0 || !reflect.DeepEqual(res[pkgs[2]], []string{"RUSTSEC-1"}) {
		t.Fatalf("res = %v", res)
	}
}

// TestAdvisoriesBatchesAt1000 checks that 2,500 packages become three requests
// of 1000, 1000 and 500 queries.
func TestAdvisoriesBatchesAt1000(t *testing.T) {
	var sizes []int
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req osvRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		sizes = append(sizes, len(req.Queries))
		fmt.Fprintf(w, `{"results":[%s]}`, repeat(`{}`, len(req.Queries)))
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), OSVURL: srv.URL}
	var pkgs []Pkg
	for i := 0; i < 2500; i++ {
		pkgs = append(pkgs, Pkg{"npm", fmt.Sprintf("p%d", i), "1.0.0"})
	}
	if _, err := c.advisories(context.Background(), pkgs); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sizes, []int{1000, 1000, 500}) {
		t.Fatalf("batch sizes = %v", sizes)
	}
}

// repeat joins n copies of s with commas.
func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

// TestAdvisoriesRejectsBadResponses checks the error paths: wrong result count
// (position matching would be unsafe), HTTP errors, and invalid JSON.
func TestAdvisoriesRejectsBadResponses(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"count":  func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"results":[]}`) },
		"status": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
		"json":   func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{`) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()
			c := &Client{HTTP: srv.Client(), OSVURL: srv.URL}
			if _, err := c.advisories(context.Background(), []Pkg{{"npm", "a", "1.0.0"}}); err == nil {
				t.Fatal("want error")
			}
		})
	}
}
