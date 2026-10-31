package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/subaud-tools/muda/internal/store"
)

// A session outside tmux has no window: proj is not asked, and a tab shows the
// project, or none is open and the channel opens the browser.
func TestOpenWithNoWhereUsesATab(t *testing.T) {
	dir := t.TempDir()
	ran := filepath.Join(dir, "ran")
	script := "#!/bin/sh\ntouch \"" + ran + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "proj"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "muda.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(st)
	open := func() (tab string, project int64) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"session": "s1", "title": "Delivery"})
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/agent/open", bytes.NewReader(body)))
		var out struct {
			Tab     string `json:"tab"`
			Project int64  `json:"project"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%v: %s", err, rec.Body)
		}
		return out.Tab, out.Project
	}

	tab, project := open()
	if tab != "opened" {
		t.Fatalf("no tab open: %q, want opened", tab)
	}
	s.events.Tabs.Seen("t1", strconv.FormatInt(project, 10))
	if tab, _ := open(); tab != "reused" {
		t.Fatalf("a tab on the project: %q, want reused", tab)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("proj ran for a session with no Where")
	}
}

// The database is closed before the entry is withdrawn, so a restart never opens
// it under a serve still closing it.
func TestStoppingClosesTheDatabase(t *testing.T) {
	t.Setenv("MUDA_HOME", t.TempDir())
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "muda.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, st, Options{Version: "test"}) }()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := Live().Running(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("serve never wrote its entry")
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := st.DB.Ping(); err == nil {
		t.Fatal("the database is still open after serve stopped")
	}
}
