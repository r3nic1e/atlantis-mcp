package atlantis

import (
	"context"
	"net/http"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

// indexHTML mirrors the jobs section of Atlantis' index.html.tmpl: a
// div.pulls-row per PR/project group with six span.pulls-element cells.
const indexHTML = `<!DOCTYPE html><html><body>
<section>
<div class="lock-grid">
  <div class="pulls-row">
    <span class="pulls-element">acme/infra #19721</span>
    <span class="pulls-element"><code>aws/dev</code></span>
    <span class="pulls-element"><code>default</code></span>
    <span class="pulls-element">
      <div><span class="lock-datetime">2026-07-20 16:38:58</span></div>
      <div><span class="lock-datetime">2026-07-20 16:39:10</span></div>
    </span>
    <span class="pulls-element">
      <div><a href="/jobs/2ef79b34-860c-425a-9819-b437bbb9ad10" target="_blank">pre apply #0</a></div>
      <div><a href="/jobs/a67a5a21-165c-4005-8283-1560f97d4d3f" target="_blank">apply #0</a></div>
    </span>
    <span class="pulls-element">
      <div>Pre workflow hook #0</div>
      <div></div>
    </span>
  </div>
  <div class="pulls-row">
    <span class="pulls-element">otherorg/repo #19721</span>
    <span class="pulls-element"></span>
    <span class="pulls-element"></span>
    <span class="pulls-element"><div><span class="lock-datetime">2026-07-19 10:00:00</span></div></span>
    <span class="pulls-element"><div><a href="/jobs/1c14989f-b840-4d11-a18b-c01f1756cb89" target="_blank">plan #0</a></div></span>
    <span class="pulls-element"><div></div></span>
  </div>
  <div class="pulls-row">
    <span class="pulls-element">acme/infra #19722</span>
    <span class="pulls-element"><code>aws/prod</code></span>
    <span class="pulls-element"><code>default</code></span>
    <span class="pulls-element"><div><span class="lock-datetime">2026-07-18 09:00:00</span></div></span>
    <span class="pulls-element"><div><a href="/jobs/deadbeef-0000-0000-0000-000000000000" target="_blank">plan #0</a></div></span>
    <span class="pulls-element"><div></div></span>
  </div>
</div>
</section>
</body></html>`

func jobsTestClient(t *testing.T) *Client {
	return newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(indexHTML))
	}))
}

func TestListJobsByPull(t *testing.T) {
	c := jobsTestClient(t)
	jobs, _, err := c.ListJobs(context.Background(), 19721, "")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	// Both #19721 rows (2 jobs + 1 job) match; #19722 must not.
	if len(jobs) != 3 {
		t.Fatalf("got %d jobs, want 3: %+v", len(jobs), jobs)
	}
	ids := make([]string, len(jobs))
	for i, j := range jobs {
		ids[i] = j.JobID
		if j.PullNum != 19721 {
			t.Errorf("job %s PullNum = %d, want 19721", j.JobID, j.PullNum)
		}
	}
	sort.Strings(ids)
	want := []string{
		"1c14989f-b840-4d11-a18b-c01f1756cb89",
		"2ef79b34-860c-425a-9819-b437bbb9ad10",
		"a67a5a21-165c-4005-8283-1560f97d4d3f",
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("job IDs = %v, want %v", ids, want)
		}
	}

	// First job's aligned fields.
	var first Job
	for _, j := range jobs {
		if j.JobID == "2ef79b34-860c-425a-9819-b437bbb9ad10" {
			first = j
		}
	}
	if first.Repo != "acme/infra" || first.Project != "aws/dev" || first.Workspace != "default" {
		t.Errorf("unexpected group fields: %+v", first)
	}
	if first.Step != "pre apply #0" || first.Time != "2026-07-20 16:38:58" || first.Description != "Pre workflow hook #0" {
		t.Errorf("unexpected aligned fields: %+v", first)
	}
}

func TestListJobsByPullAndRepo(t *testing.T) {
	c := jobsTestClient(t)
	jobs, _, err := c.ListJobs(context.Background(), 19721, "acme/infra")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2 (repo filter): %+v", len(jobs), jobs)
	}
	for _, j := range jobs {
		if j.Repo != "acme/infra" {
			t.Errorf("job %s repo = %q, want acme/infra", j.JobID, j.Repo)
		}
	}
}

func TestListJobsNoMatch(t *testing.T) {
	c := jobsTestClient(t)
	jobs, _, err := c.ListJobs(context.Background(), 99999, "")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("got %d jobs, want 0", len(jobs))
	}
}

func TestListJobsCaches(t *testing.T) {
	var hits int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(indexHTML))
	}))
	c.jobsCacheTTL = time.Minute

	_, expiresIn1, err := c.ListJobs(context.Background(), 19721, "")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if expiresIn1 <= 0 || expiresIn1 > time.Minute {
		t.Fatalf("expiresIn = %v, want (0, 1m]", expiresIn1)
	}

	// A second call for a different PR should reuse the same cached page.
	_, expiresIn2, err := c.ListJobs(context.Background(), 19722, "")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if expiresIn2 <= 0 || expiresIn2 > expiresIn1 {
		t.Fatalf("expiresIn2 = %v, want (0, %v]", expiresIn2, expiresIn1)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("index page fetched %d times, want 1 (should be served from cache)", got)
	}
}

func TestListJobsCacheDisabled(t *testing.T) {
	var hits int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(indexHTML))
	}))
	// jobsCacheTTL is left at its zero value (caching disabled) by newTestClient.

	if _, expiresIn, err := c.ListJobs(context.Background(), 19721, ""); err != nil {
		t.Fatalf("ListJobs: %v", err)
	} else if expiresIn != 0 {
		t.Fatalf("expiresIn = %v, want 0 (caching disabled)", expiresIn)
	}
	if _, _, err := c.ListJobs(context.Background(), 19721, ""); err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("index page fetched %d times, want 2 (caching disabled)", got)
	}
}

func TestParseRepoPull(t *testing.T) {
	cases := []struct {
		in       string
		wantRepo string
		wantPull int
		wantOK   bool
	}{
		{"acme/infra #19721", "acme/infra", 19721, true},
		{"  owner/repo   #42  ", "owner/repo", 42, true},
		{"no number here", "", 0, false},
		{"owner/repo #notanumber", "", 0, false},
	}
	for _, tc := range cases {
		repo, pull, ok := parseRepoPull(tc.in)
		if ok != tc.wantOK || repo != tc.wantRepo || pull != tc.wantPull {
			t.Errorf("parseRepoPull(%q) = (%q,%d,%v), want (%q,%d,%v)", tc.in, repo, pull, ok, tc.wantRepo, tc.wantPull, tc.wantOK)
		}
	}
}
