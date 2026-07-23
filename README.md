# atlantis-mcp

An MCP server that gives an LLM read-only access to a running [Atlantis](https://runatlantis.io)
server: locks, jobs, and job logs. Serves MCP over Streamable HTTP.

## Tools

| Tool | Description | Params |
| --- | --- | --- |
| `list_locks` | List all active project locks. | — |
| `list_jobs` | List plan/apply/hook jobs for a pull request. | `pull_num` (required), `repo` (optional, disambiguates when a PR number exists in more than one repo) |
| `get_job_output` | Get the log output of a job by ID. | `job_id` (required, a UUID from `list_jobs`), `timeout_seconds` (optional, default 30, max 300) |

`list_jobs` is backed by a short-lived cache (see `ATLANTIS_JOBS_CACHE_TTL` below) since it works by
scraping and parsing Atlantis' HTML index page. Its result includes `result_expires_in`, telling you
how long until the next call re-scrapes Atlantis instead of returning cached data.

## Environment variables

| Variable | Default | Description |
| --- | --- | --- |
| `ATLANTIS_URL` | — (required) | Base URL of the Atlantis server, e.g. `https://atlantis.example.com` |
| `ATLANTIS_WEB_USERNAME` | — | HTTP basic-auth username, if the Atlantis web UI is protected |
| `ATLANTIS_WEB_PASSWORD` | — | HTTP basic-auth password |
| `ATLANTIS_INSECURE` | `false` | Set to `true` to skip TLS certificate verification |
| `ATLANTIS_LISTEN_ADDR` | `:8080` | Address this server binds to |
| `ATLANTIS_JOBS_CACHE_TTL` | `1m` | How long `list_jobs` results are cached; `0` disables caching |

## Running it

```sh
docker run -p 8080:8080 -e ATLANTIS_URL=https://atlantis.example.com ghcr.io/r3nic1e/atlantis-mcp
```

Then point an MCP client at `http://localhost:8080/mcp`.

Or build and run locally:

```sh
go build -o atlantis-mcp ./cmd/atlantis-mcp
ATLANTIS_URL=https://atlantis.example.com ./atlantis-mcp
```

## License

MIT, see [LICENSE](LICENSE).

---

No human braincells were harmed in making of this project.
