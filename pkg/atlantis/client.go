// Package atlantis is a minimal client for a running Atlantis server
// (https://runatlantis.io). It exposes the few read-only views the MCP server
// needs: locks (JSON API), jobs per pull request (scraped from the index page),
// and job output (streamed over a WebSocket).
package atlantis

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Config configures a Client.
type Config struct {
	BaseURL      string        // e.g. https://atlantis.example.com
	Username     string        // optional HTTP basic-auth username (web UI endpoints)
	Password     string        // optional HTTP basic-auth password
	Insecure     bool          // skip TLS certificate verification
	JobsCacheTTL time.Duration // how long ListJobs caches the parsed jobs page; <= 0 disables caching
}

// Client talks to a single Atlantis server over HTTP and WebSocket.
type Client struct {
	base     *url.URL
	http     *http.Client
	username string
	password string
	insecure bool

	// VersionRaw is the full version string reported by GET /status; Version is
	// its parsed "vX.Y.Z" prefix (empty if not parseable). Both are populated by
	// Status and kept for later feature-flagging.
	VersionRaw string
	Version    string

	// jobsCacheTTL, jobsCacheMu, jobsCached and jobsExpiresAt implement ListJobs'
	// cache of the full, unfiltered jobs page (see jobs.go).
	jobsCacheTTL  time.Duration
	jobsCacheMu   sync.Mutex
	jobsCached    []Job
	jobsExpiresAt time.Time
}

// New builds a Client from cfg. It returns an error only when BaseURL is
// missing or not a valid http(s) URL.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("atlantis base URL is required (set ATLANTIS_URL)")
	}
	base, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parsing base URL %q: %w", cfg.BaseURL, err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("base URL must be http or https, got %q", cfg.BaseURL)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in
	}

	return &Client{
		base:         base,
		http:         &http.Client{Timeout: 30 * time.Second, Transport: transport},
		username:     cfg.Username,
		password:     cfg.Password,
		insecure:     cfg.Insecure,
		jobsCacheTTL: cfg.JobsCacheTTL,
	}, nil
}

// urlFor returns the absolute URL for path p, preserving any base path prefix.
func (c *Client) urlFor(p string) string {
	return c.base.String() + "/" + strings.TrimPrefix(p, "/")
}

// get performs an authenticated GET and returns the body on a 2xx response.
func (c *Client) get(ctx context.Context, p string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.urlFor(p), nil)
	if err != nil {
		return nil, err
	}
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", p, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: unexpected status %s", p, resp.Status)
	}
	return body, nil
}

type statusResponse struct {
	ShuttingDown  bool   `json:"shutting_down"`
	InProgressOps int    `json:"in_progress_operations"`
	Version       string `json:"version"`
}

var versionRe = regexp.MustCompile(`v?\d+\.\d+\.\d+`)

// Status queries GET /status and stores the reported Atlantis version on the
// Client. It returns the raw version string.
func (c *Client) Status(ctx context.Context) (string, error) {
	body, err := c.get(ctx, "/status")
	if err != nil {
		return "", err
	}
	var s statusResponse
	if err := json.Unmarshal(body, &s); err != nil {
		return "", fmt.Errorf("decoding /status: %w", err)
	}
	c.VersionRaw = s.Version
	c.Version = versionRe.FindString(s.Version)
	return s.Version, nil
}
