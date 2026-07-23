package atlantis

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Job is one Atlantis job (a plan/apply/hook step) tracked for a pull request.
type Job struct {
	JobID       string `json:"job_id"`
	Repo        string `json:"repo"`
	PullNum     int    `json:"pull_num"`
	Project     string `json:"project,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
	Step        string `json:"step,omitempty"`
	Description string `json:"description,omitempty"`
	Time        string `json:"time,omitempty"`
	URL         string `json:"url"`
}

// ListJobs returns jobs belonging to pullNum from the Atlantis index page
// (GET /), which is parsed at most once per JobsCacheTTL (see Config) rather
// than on every call. When repo (a full name like "owner/repo") is
// non-empty, results are further restricted to that repo, since PR numbers
// are not unique across repositories. expiresIn is how long the returned
// data can still be served from cache before the next call re-scrapes
// Atlantis; it is 0 when caching is disabled.
//
// Atlantis exposes the pull-request-to-job mapping only by rendering it into
// the index HTML, and it keeps that data in memory only, so jobs disappear when
// the server restarts or the PR is closed.
func (c *Client) ListJobs(ctx context.Context, pullNum int, repo string) (jobs []Job, expiresIn time.Duration, err error) {
	all, expiresIn, err := c.jobsSnapshot(ctx)
	if err != nil {
		return nil, 0, err
	}
	for _, j := range all {
		if j.PullNum != pullNum {
			continue
		}
		if repo != "" && j.Repo != repo {
			continue
		}
		jobs = append(jobs, j)
	}
	return jobs, expiresIn, nil
}

// jobsSnapshot returns every job currently listed on the Atlantis index page,
// across all pull requests, fetching and parsing it fresh only when the
// cache is empty or older than jobsCacheTTL.
func (c *Client) jobsSnapshot(ctx context.Context) ([]Job, time.Duration, error) {
	if c.jobsCacheTTL <= 0 {
		jobs, err := c.fetchJobs(ctx)
		return jobs, 0, err
	}

	c.jobsCacheMu.Lock()
	defer c.jobsCacheMu.Unlock()

	if now := time.Now(); now.Before(c.jobsExpiresAt) {
		return c.jobsCached, c.jobsExpiresAt.Sub(now), nil
	}

	jobs, err := c.fetchJobs(ctx)
	if err != nil {
		return nil, 0, err
	}
	c.jobsCached = jobs
	c.jobsExpiresAt = time.Now().Add(c.jobsCacheTTL)
	return jobs, c.jobsCacheTTL, nil
}

// fetchJobs fetches and parses the Atlantis index page, returning every job
// listed on it regardless of pull request.
func (c *Client) fetchJobs(ctx context.Context) ([]Job, error) {
	body, err := c.get(ctx, "/")
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parsing index page: %w", err)
	}

	var jobs []Job
	// Each pull-request/project group is a div.pulls-row with six
	// span.pulls-element cells: repo+PR, project, workspace, then aligned
	// per-job lists of times, step links, and descriptions.
	doc.Find("div.pulls-row").Each(func(_ int, row *goquery.Selection) {
		spans := row.Find("span.pulls-element")
		if spans.Length() < 5 {
			return
		}
		rowRepo, rowPull, ok := parseRepoPull(spans.Eq(0).Text())
		if !ok {
			return
		}

		project := strings.TrimSpace(spans.Eq(1).Text())
		workspace := strings.TrimSpace(spans.Eq(2).Text())

		times := divTexts(spans.Eq(3))
		var descriptions []string
		if spans.Length() >= 6 {
			descriptions = divTexts(spans.Eq(5))
		}

		i := 0
		spans.Eq(4).Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			jobID := jobIDFromHref(href)
			if jobID == "" {
				return
			}
			job := Job{
				JobID:     jobID,
				Repo:      rowRepo,
				PullNum:   rowPull,
				Project:   project,
				Workspace: workspace,
				Step:      strings.TrimSpace(a.Text()),
				URL:       href,
			}
			if i < len(times) {
				job.Time = times[i]
			}
			if i < len(descriptions) {
				job.Description = descriptions[i]
			}
			jobs = append(jobs, job)
			i++
		})
	})
	return jobs, nil
}

// divTexts returns the trimmed text of each direct-or-nested <div> under sel,
// in document order.
func divTexts(sel *goquery.Selection) []string {
	var out []string
	sel.Find("div").Each(func(_ int, s *goquery.Selection) {
		out = append(out, strings.TrimSpace(s.Text()))
	})
	return out
}

// parseRepoPull parses "owner/repo #123" into ("owner/repo", 123, true).
func parseRepoPull(s string) (repo string, pull int, ok bool) {
	s = strings.TrimSpace(s)
	idx := strings.LastIndex(s, "#")
	if idx < 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(s[idx+1:]))
	if err != nil {
		return "", 0, false
	}
	return strings.TrimSpace(s[:idx]), n, true
}

// jobIDFromHref extracts the job ID (final path segment) from a /jobs/{id} URL,
// returning "" if href is not a job link.
func jobIDFromHref(href string) string {
	if !strings.Contains(href, "/jobs/") {
		return ""
	}
	if i := strings.IndexAny(href, "?#"); i >= 0 {
		href = href[:i]
	}
	return path.Base(strings.TrimRight(href, "/"))
}
