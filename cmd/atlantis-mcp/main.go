// Command atlantis-mcp is an MCP server that exposes read-only views of a
// running Atlantis server: active locks, jobs for a pull request, and job
// output. It serves MCP over Streamable HTTP.
//
// Configuration is via environment variables:
//
//	ATLANTIS_URL             base URL, e.g. https://atlantis.example.com (required)
//	ATLANTIS_WEB_USERNAME    HTTP basic-auth username for the web UI (optional)
//	ATLANTIS_WEB_PASSWORD    HTTP basic-auth password for the web UI (optional)
//	ATLANTIS_INSECURE        set to "true" to skip TLS verification (optional)
//	ATLANTIS_LISTEN_ADDR     address to serve MCP on (default ":8080")
//	ATLANTIS_JOBS_CACHE_TTL  how long to cache the parsed jobs list (default "1m"; "0" disables caching)
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/r3nic1e/atlantis-mcp/pkg/atlantis"
)

// version is the server version, overridable at build time via
// -ldflags "-X main.version=...".
var version = "0.1.0"

func main() {
	if err := run(); err != nil {
		log.Fatalf("atlantis-mcp: %v", err)
	}
}

func run() error {
	jobsCacheTTL, err := jobsCacheTTLFromEnv()
	if err != nil {
		return err
	}

	client, err := atlantis.New(atlantis.Config{
		BaseURL:      os.Getenv("ATLANTIS_URL"),
		Username:     os.Getenv("ATLANTIS_WEB_USERNAME"),
		Password:     os.Getenv("ATLANTIS_WEB_PASSWORD"),
		Insecure:     os.Getenv("ATLANTIS_INSECURE") == "true",
		JobsCacheTTL: jobsCacheTTL,
	})
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Best-effort: record the Atlantis version for later feature flags. A
	// failure here must not stop the tools from working.
	if v, err := client.Status(ctx); err != nil {
		log.Printf("warning: could not fetch /status: %v", err)
	} else {
		log.Printf("connected to Atlantis %s", v)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "atlantis-mcp",
		Version: version,
	}, nil)

	registerTools(server, client)

	addr := os.Getenv("ATLANTIS_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	log.Printf("listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

// jobsCacheTTLFromEnv parses ATLANTIS_JOBS_CACHE_TTL, defaulting to 1 minute
// when unset. "0" (or any non-positive duration) disables caching.
func jobsCacheTTLFromEnv() (time.Duration, error) {
	s := os.Getenv("ATLANTIS_JOBS_CACHE_TTL")
	if s == "" {
		return time.Minute, nil
	}
	return time.ParseDuration(s)
}

func registerTools(server *mcp.Server, client *atlantis.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_locks",
		Description: "List all active Atlantis project locks.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, listLocksOutput, error) {
		locks, err := client.ListLocks(ctx)
		if err != nil {
			return nil, listLocksOutput{}, err
		}
		return nil, listLocksOutput{Locks: locks}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_jobs",
		Description: "List Atlantis jobs (plan/apply/hook steps) for a pull request number. " +
			"Jobs come from Atlantis' in-memory state, so they are only available while the PR is open and the server has not restarted. " +
			"Results may be served from a short-lived cache; result_expires_in tells you how long until the next call re-scrapes Atlantis.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listJobsInput) (*mcp.CallToolResult, listJobsOutput, error) {
		jobs, expiresIn, err := client.ListJobs(ctx, in.PullNum, in.Repo)
		if err != nil {
			return nil, listJobsOutput{}, err
		}
		return nil, listJobsOutput{Jobs: jobs, ResultExpiresIn: expiresIn.Round(time.Second).String()}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_job_output",
		Description: "Get the log output for an Atlantis job by its ID (a UUID, e.g. from list_jobs). " +
			"Streams the job's buffered and live output. If the job is still running, returns the output so far with complete=false.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getJobOutputInput) (*mcp.CallToolResult, atlantis.JobOutput, error) {
		secs := in.TimeoutSeconds
		switch {
		case secs <= 0:
			secs = 30
		case secs > 300:
			secs = 300
		}
		out, err := client.GetJobOutput(ctx, in.JobID, time.Duration(secs)*time.Second)
		if err != nil {
			return nil, atlantis.JobOutput{}, err
		}
		return nil, out, nil
	})
}

type noInput struct{}

type listLocksOutput struct {
	Locks []atlantis.Lock `json:"locks"`
}

type listJobsInput struct {
	PullNum int    `json:"pull_num" jsonschema:"the pull request number"`
	Repo    string `json:"repo,omitempty" jsonschema:"optional full repository name (owner/repo) to disambiguate when the same PR number exists in multiple repositories"`
}

type listJobsOutput struct {
	Jobs            []atlantis.Job `json:"jobs"`
	ResultExpiresIn string         `json:"result_expires_in"`
}

type getJobOutputInput struct {
	JobID          string `json:"job_id" jsonschema:"the job ID (a UUID)"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"maximum seconds to wait for output (default 30, max 300)"`
}
