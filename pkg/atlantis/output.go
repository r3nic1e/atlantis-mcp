package atlantis

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// idleTimeout is how long GetJobOutput waits for another line before assuming
// it has caught up to a still-running job's live output.
const idleTimeout = 3 * time.Second

// JobOutput is the collected log output for a job. Complete is true when the
// server closed the stream (the job finished); false when GetJobOutput returned
// because the job is still running or the timeout elapsed.
type JobOutput struct {
	Output   string `json:"output"`
	Complete bool   `json:"complete"`
}

// GetJobOutput connects to a job's WebSocket (GET /jobs/{id}/ws), which replays
// the job's buffered log and then live-tails it. It collects output until the
// server closes the stream (Complete=true) or until the stream goes idle / the
// overall timeout elapses (Complete=false). A default timeout of 30s is used
// when timeout <= 0.
func (c *Client) GetJobOutput(ctx context.Context, jobID string, timeout time.Duration) (JobOutput, error) {
	if strings.TrimSpace(jobID) == "" {
		return JobOutput{}, fmt.Errorf("job_id is required")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	wsURL := c.wsURL("/jobs/" + url.PathEscape(jobID) + "/ws")

	header := http.Header{}
	if c.username != "" || c.password != "" {
		r, _ := http.NewRequest(http.MethodGet, wsURL, nil)
		r.SetBasicAuth(c.username, c.password)
		header.Set("Authorization", r.Header.Get("Authorization"))
	}

	// Use a dedicated TLS config for the dialer. Sharing the HTTP transport's
	// config would advertise HTTP/2 via ALPN and break the WebSocket upgrade.
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second
	if c.insecure {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in
	}

	conn, resp, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		if resp != nil {
			return JobOutput{}, fmt.Errorf("connecting to job %s: server returned %s (the job may not exist or is no longer retained in memory)", jobID, resp.Status)
		}
		return JobOutput{}, fmt.Errorf("connecting to job %s: %w", jobID, err)
	}
	defer conn.Close()

	overall := time.Now().Add(timeout)
	var b strings.Builder
	complete := false
	for {
		now := time.Now()
		if !now.Before(overall) {
			break // hit overall timeout; job still running
		}
		deadline := now.Add(idleTimeout)
		if deadline.After(overall) {
			deadline = overall
		}
		_ = conn.SetReadDeadline(deadline)

		_, msg, err := conn.ReadMessage()
		if err != nil {
			// A read deadline (idle/overall) means the socket is still open and
			// the job is running. Any other error means the server closed the
			// stream, i.e. the job's output is complete.
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				complete = false
			} else {
				complete = true
			}
			break
		}
		// Atlantis frames each log line as "\r" + line + "\n".
		line := strings.TrimSuffix(strings.TrimPrefix(string(msg), "\r"), "\n")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return JobOutput{Output: b.String(), Complete: complete}, nil
}

// wsURL converts the base http(s) URL into a ws(s) URL for path p, preserving
// any base path prefix.
func (c *Client) wsURL(p string) string {
	u := *c.base
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimPrefix(p, "/")
	return u.String()
}
