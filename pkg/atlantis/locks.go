package atlantis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Lock is one active project lock.
type Lock struct {
	Name        string    `json:"name"`
	ProjectName string    `json:"project_name"`
	Repo        string    `json:"repo"`
	Path        string    `json:"path"`
	Workspace   string    `json:"workspace"`
	PullNum     int       `json:"pull_num"`
	PullURL     string    `json:"pull_url"`
	User        string    `json:"user"`
	Time        time.Time `json:"time"`
}

// apiLockDetail matches the JSON emitted by GET /api/locks. Atlantis serializes
// the (deprecated) LockDetail struct: capitalized Go field names, no JSON tags,
// and PullID rendered as a quoted string.
type apiLockDetail struct {
	Name            string    `json:"Name"`
	ProjectName     string    `json:"ProjectName"`
	ProjectRepo     string    `json:"ProjectRepo"`
	ProjectRepoPath string    `json:"ProjectRepoPath"`
	PullID          int       `json:"PullID,string"`
	PullURL         string    `json:"PullURL"`
	User            string    `json:"User"`
	Workspace       string    `json:"Workspace"`
	Time            time.Time `json:"Time"`
}

type apiLocksResult struct {
	Locks []apiLockDetail `json:"Locks"`
}

// ListLocks returns all active locks from GET /api/locks. The result is empty
// (not nil) when there are no locks.
func (c *Client) ListLocks(ctx context.Context) ([]Lock, error) {
	body, err := c.get(ctx, "/api/locks")
	if err != nil {
		return nil, err
	}
	var result apiLocksResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decoding /api/locks: %w", err)
	}

	locks := make([]Lock, 0, len(result.Locks))
	for _, l := range result.Locks {
		locks = append(locks, Lock{
			Name:        l.Name,
			ProjectName: l.ProjectName,
			Repo:        l.ProjectRepo,
			Path:        l.ProjectRepoPath,
			Workspace:   l.Workspace,
			PullNum:     l.PullID,
			PullURL:     l.PullURL,
			User:        l.User,
			Time:        l.Time,
		})
	}
	return locks, nil
}
