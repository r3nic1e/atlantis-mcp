package atlantis

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// wsJobHandler emulates Atlantis' /jobs/{id}/ws endpoint: it upgrades, writes
// each frame as "\r"+line+"\n", then either closes (job complete) or holds the
// connection open (job still running).
func wsJobHandler(frames []string, keepOpen bool) http.HandlerFunc {
	up := websocket.Upgrader{}
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/ws") {
			http.NotFound(w, r)
			return
		}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for _, f := range frames {
			_ = conn.WriteMessage(websocket.BinaryMessage, []byte("\r"+f+"\n"))
		}
		if keepOpen {
			for { // block until the client disconnects
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}
	}
}

func TestGetJobOutputComplete(t *testing.T) {
	c := newTestClient(t, wsJobHandler([]string{"line one", "line two"}, false))
	out, err := c.GetJobOutput(context.Background(), "job-1", 5*time.Second)
	if err != nil {
		t.Fatalf("GetJobOutput: %v", err)
	}
	if !out.Complete {
		t.Errorf("Complete = false, want true (server closed the stream)")
	}
	if out.Output != "line one\nline two\n" {
		t.Errorf("Output = %q, want %q", out.Output, "line one\nline two\n")
	}
}

func TestGetJobOutputStillRunning(t *testing.T) {
	c := newTestClient(t, wsJobHandler([]string{"partial"}, true))
	start := time.Now()
	out, err := c.GetJobOutput(context.Background(), "job-2", 1*time.Second)
	if err != nil {
		t.Fatalf("GetJobOutput: %v", err)
	}
	if out.Complete {
		t.Errorf("Complete = true, want false (job still running)")
	}
	if out.Output != "partial\n" {
		t.Errorf("Output = %q, want %q", out.Output, "partial\n")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("took %v, expected to return near the 1s timeout", elapsed)
	}
}

func TestGetJobOutputNotFound(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid key", http.StatusBadRequest)
	}))
	_, err := c.GetJobOutput(context.Background(), "missing", 5*time.Second)
	if err == nil {
		t.Fatal("expected an error for an unknown job")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error = %q, want it to mention the 400 status", err.Error())
	}
}

func TestGetJobOutputEmptyID(t *testing.T) {
	c, err := New(Config{BaseURL: "https://atlantis.example.com"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.GetJobOutput(context.Background(), "  ", time.Second); err == nil {
		t.Fatal("expected an error for an empty job_id")
	}
}
