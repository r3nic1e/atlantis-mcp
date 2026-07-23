package atlantis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestListLocks(t *testing.T) {
	const body = `{"Locks":[
		{"Name":"acme/infra/aws/dev/default","ProjectName":"dev","ProjectRepo":"acme/infra","ProjectRepoPath":"aws/dev","PullID":"19721","PullURL":"https://github.com/acme/infra/pull/19721","User":"alice","Workspace":"default","Time":"2026-07-20T16:38:58Z"}
	]}`
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/locks" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))

	locks, err := c.ListLocks(context.Background())
	if err != nil {
		t.Fatalf("ListLocks: %v", err)
	}
	if len(locks) != 1 {
		t.Fatalf("got %d locks, want 1", len(locks))
	}
	l := locks[0]
	if l.PullNum != 19721 { // PullID arrives as the quoted string "19721"
		t.Errorf("PullNum = %d, want 19721", l.PullNum)
	}
	if l.Repo != "acme/infra" {
		t.Errorf("Repo = %q", l.Repo)
	}
	if l.Path != "aws/dev" || l.Workspace != "default" || l.User != "alice" {
		t.Errorf("unexpected lock fields: %+v", l)
	}
	if l.Time.IsZero() {
		t.Errorf("Time not parsed: %+v", l)
	}
}

func TestListLocksEmpty(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Locks":null}`))
	}))
	locks, err := c.ListLocks(context.Background())
	if err != nil {
		t.Fatalf("ListLocks: %v", err)
	}
	if locks == nil {
		t.Fatal("locks should be non-nil empty slice")
	}
	if len(locks) != 0 {
		t.Fatalf("got %d locks, want 0", len(locks))
	}
}
