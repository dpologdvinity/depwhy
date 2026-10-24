package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Default service locations and limits (SPEC §12.4).
const (
	osvURL         = "https://api.osv.dev"
	npmURL         = "https://registry.npmjs.org"
	pypiURL        = "https://pypi.org/pypi"
	cratesURL      = "https://crates.io/api/v1/crates"
	requestTimeout = 5 * time.Second
	maxConcurrent  = 8
	cacheTTL       = 24 * time.Hour
	maxBody        = 32 << 20 // refuse to read more than 32 MiB from a service
)

// Client talks to the advisory and registry services. The URL fields exist so
// tests can point at local servers; New fills in the real ones.
type Client struct {
	HTTP        *http.Client
	OSVURL      string
	NPMURL      string
	PyPIURL     string
	CratesURL   string
	UserAgent   string
	Timeout     time.Duration // per request; 0 means requestTimeout
	Concurrency int           // simultaneous requests; 0 means maxConcurrent
	Cache       *Cache        // nil disables caching
	Refresh     bool          // skip cache reads (writes still happen)
}

// New returns a client for the real services. version is depwhy's version, sent
// in the User-Agent header; refresh bypasses cached answers.
func New(version string, refresh bool) *Client {
	c := &Client{
		HTTP:      &http.Client{},
		OSVURL:    osvURL,
		NPMURL:    npmURL,
		PyPIURL:   pypiURL,
		CratesURL: cratesURL,
		UserAgent: fmt.Sprintf("depwhy/%s (+https://github.com/dpologdvinity/depwhy)", version),
		Refresh:   refresh,
	}
	if dir := DefaultCacheDir(); dir != "" {
		c.Cache = &Cache{Dir: dir, TTL: cacheTTL, Now: time.Now}
	}
	return c
}

// do sends one request with the per-request timeout and decodes a 200 response
// into out. It returns the HTTP status; a 404 is not an error (the caller treats
// it as "no information"). Any other non-200 status, a network failure, or an
// undecodable body is an error.
func (c *Client) do(ctx context.Context, method, url string, body any, out any) (int, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = requestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("invalid response: %w", err)
		}
		return resp.StatusCode, nil
	case http.StatusNotFound:
		return resp.StatusCode, nil
	}
	return resp.StatusCode, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
}
