//go:build e2e

// Package e2e drives the built atlantis-mcp binary as a subprocess, speaking
// MCP over its Streamable HTTP endpoint via the official MCP client, and
// exercises every tool against a real Atlantis server (see
// docker-compose.e2e.yml). It validates that the client's assumptions about
// Atlantis' real responses hold — something the in-process unit fixtures
// cannot.
//
// It is gated behind the `e2e` build tag and is a no-op unless both MCP_BINARY
// (path to a built atlantis-mcp) and ATLANTIS_URL are set, so `go test ./...`
// never runs or compiles it. Run it with:
//
//	go test -tags e2e -v ./e2e/...
package e2e

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newSession starts the MCP server binary listening on addr and pointed at
// ATLANTIS_URL, and returns a connected client session. Connecting at all
// proves the binary launched, reached Atlantis' /status, and is serving MCP
// over HTTP (all logged to stderr).
func newSession(t *testing.T, addr string) *mcp.ClientSession {
	t.Helper()

	bin := os.Getenv("MCP_BINARY")
	atlantisURL := os.Getenv("ATLANTIS_URL")
	if bin == "" || atlantisURL == "" {
		t.Skip("set MCP_BINARY and ATLANTIS_URL to run e2e tests")
	}

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "ATLANTIS_URL="+atlantisURL, "ATLANTIS_LISTEN_ADDR="+addr)
	cmd.Stderr = os.Stderr // surface the server's "connected to Atlantis" / "listening on" logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %q: %v", bin, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	client := mcp.NewClient(&mcp.Implementation{Name: "atlantis-mcp-e2e", Version: "test"}, nil)
	transport := &mcp.StreamableClientTransport{URL: "http://" + addr + "/mcp"}

	// The server needs a moment to start listening; retry the connect instead
	// of requiring the test to guess a fixed startup delay.
	deadline := time.Now().Add(30 * time.Second)
	var sess *mcp.ClientSession
	var err error
	for {
		connectCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		sess, err = client.Connect(connectCtx, transport, nil)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("connect to MCP server at %q: %v", transport.URL, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// structuredField returns the named field of a tool result's structured output.
func structuredField(res *mcp.CallToolResult, key string) (any, bool) {
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		return nil, false
	}
	v, ok := m[key]
	return v, ok
}

// toolErrText concatenates the text content of a result (used for error messages).
func toolErrText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// TestListLocks calls list_locks against a fresh Atlantis. Success doubles as
// validation that the real GET /api/locks contract matches the client.
func TestListLocks(t *testing.T) {
	sess := newSession(t, "127.0.0.1:18081")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "list_locks"})
	if err != nil {
		t.Fatalf("CallTool list_locks: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_locks returned a tool error (check the real /api/locks contract): %s", toolErrText(res))
	}
	locks, ok := structuredField(res, "locks")
	if !ok {
		t.Fatalf("list_locks result has no 'locks' field; structured=%v", res.StructuredContent)
	}
	// A fresh Atlantis has no locks; the field must be a (possibly empty) array or null.
	if locks != nil {
		if _, ok := locks.([]any); !ok {
			t.Errorf("locks = %T, want array or null", locks)
		}
	}
}

// TestListJobs calls list_jobs for a PR that does not exist; the index page
// scraping must succeed and return no jobs.
func TestListJobs(t *testing.T) {
	sess := newSession(t, "127.0.0.1:18082")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_jobs",
		Arguments: map[string]any{"pull_num": 1},
	})
	if err != nil {
		t.Fatalf("CallTool list_jobs: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_jobs returned a tool error: %s", toolErrText(res))
	}
	jobs, ok := structuredField(res, "jobs")
	if !ok {
		t.Fatalf("list_jobs result has no 'jobs' field; structured=%v", res.StructuredContent)
	}
	if jobs != nil {
		arr, ok := jobs.([]any)
		if !ok {
			t.Errorf("jobs = %T, want array or null", jobs)
		} else if len(arr) != 0 {
			t.Errorf("jobs = %v, want empty for a fresh Atlantis", arr)
		}
	}
}

// TestGetJobOutputBogusID asks for the output of a job that does not exist. The
// point is that the WebSocket path is exercised and the call returns promptly
// (near its timeout) rather than hanging.
func TestGetJobOutputBogusID(t *testing.T) {
	sess := newSession(t, "127.0.0.1:18083")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_job_output",
		Arguments: map[string]any{
			"job_id":          "00000000-0000-0000-0000-000000000000",
			"timeout_seconds": 5,
		},
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("CallTool get_job_output: %v", err)
	}
	if elapsed > 15*time.Second {
		t.Fatalf("get_job_output took %v; expected it to return near the 5s timeout, not hang", elapsed)
	}
	// Either outcome is acceptable for a non-existent job: a clean tool error
	// (Atlantis rejects the unknown job) or an empty, incomplete output.
	t.Logf("get_job_output(bogus) returned in %v (isError=%v)", elapsed, res.IsError)
}
