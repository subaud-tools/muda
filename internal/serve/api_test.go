package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/subaud-tools/muda/internal/store"
	"github.com/subaud-tools/tools-common/msgstate"
)

type rig struct {
	t  *testing.T
	st *store.Store
	h  http.Handler
}

func newRig(t *testing.T) *rig {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "muda.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &rig{t, st, New(st).Handler()}
}

// do calls the API and decodes the answer into out, failing on any other status.
func (r *rig) do(method, path string, body any, want int, out any) {
	r.t.Helper()
	var rd bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&rd).Encode(body); err != nil {
			r.t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, httptest.NewRequest(method, path, &rd))
	if rec.Code != want {
		r.t.Fatalf("%s %s: %d, want %d: %s", method, path, rec.Code, want, rec.Body)
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			r.t.Fatalf("%s %s: %v: %s", method, path, err, rec.Body)
		}
	}
}

// seeded is a project with a job, two groups and a card that waits on you.
func (r *rig) seeded() int64 {
	r.t.Helper()
	ctx := context.Background()
	p, _, err := r.st.Ensure(ctx, "Delivery", []store.Group{{Kind: store.Repo, Key: "/w/a", Label: "a"}, {Kind: store.Account, Key: "prod", Label: "Production"}}, store.You)
	if err != nil {
		r.t.Fatal(err)
	}
	err = r.st.Post(ctx, p.ID, store.Posted{
		Jobs: []store.Job{{ID: "deploy", Label: "Deployments", Stages: []store.Stage{
			{ID: "base", Label: "Baseline", State: "done"}, {ID: "find", Label: "Findings", State: "now", Count: "1 of 2"}}}},
		Cards: []store.Card{{Job: "deploy", Group: "/w/a", Stage: "find", ID: "F1", Title: "Full clones", Class: "finding", Basis: "measured",
			Evidence: []store.Evidence{{Kind: "run", Ref: "run 1", Text: "90 s"}},
			Answers:  &store.Answers{Kind: "buttons", Options: []store.Option{{ID: "ok", Label: "Approve"}}}}},
	}, "s1")
	if err != nil {
		r.t.Fatal(err)
	}
	return p.ID
}

// The front page lists each project with where it stands and who it waits on.
func TestProjectsListSaysWhereEachStands(t *testing.T) {
	r := newRig(t)
	r.seeded()
	var got ProjectList
	r.do("GET", "/api/projects", nil, 200, &got)
	if got.Cursor == "" || len(got.Projects) != 1 {
		t.Fatalf("%+v", got)
	}
	p := got.Projects[0]
	if p.Title != "Delivery" || p.OnYou != 1 || p.OnAgent != 0 || p.Groups != 2 {
		t.Fatalf("%+v", p)
	}
	if len(p.Jobs) != 1 || p.Jobs[0].Label != "Deployments" || p.Jobs[0].Stage != "Findings" || p.Jobs[0].Count != "1 of 2" {
		t.Fatalf("%+v", p.Jobs)
	}
}

// A project comes with its jobs, stages, groups and cards.
func TestAProjectComesWhole(t *testing.T) {
	r := newRig(t)
	id := r.seeded()
	var v ProjectView
	r.do("GET", "/api/projects/"+strconv.FormatInt(id, 10), nil, 200, &v)
	if v.Title != "Delivery" || len(v.Jobs) != 1 || len(v.Jobs[0].Stages) != 2 || len(v.Groups) != 2 || len(v.Cards) != 1 || v.Thread == 0 {
		t.Fatalf("%+v", v)
	}
	if !v.Cards[0].NeedsYou || v.Cards[0].Evidence[0].Ref != "run 1" {
		t.Fatalf("%+v", v.Cards[0])
	}
	r.do("GET", "/api/projects/999", nil, 404, nil)
}

// Picks and notes are drafts until Send, which delivers them as one Send.
func TestDraftsGoOutTogetherOnSend(t *testing.T) {
	r := newRig(t)
	id := r.seeded()
	base := "/api/projects/" + strconv.FormatInt(id, 10)
	var v ProjectView
	r.do("GET", base, nil, 200, &v)
	var pick, note, gone DraftView
	r.do("POST", base+"/drafts", DraftBody{Thread: v.Cards[0].Thread, Text: "Approve", Answer: &store.Answer{Kind: "buttons", Choice: "ok"}}, 200, &pick)
	r.do("POST", base+"/drafts", DraftBody{Thread: v.Thread, Text: "first note"}, 200, &note)
	r.do("POST", base+"/drafts", DraftBody{Thread: v.Thread, Text: "never mind"}, 200, &gone)
	r.do("PUT", base+"/drafts/"+strconv.FormatInt(note.ID, 10), DraftBody{Text: "a note"}, 204, nil)
	r.do("DELETE", base+"/drafts/"+strconv.FormatInt(gone.ID, 10), nil, 204, nil)

	r.do("GET", base, nil, 200, &v)
	if len(v.Drafts) != 2 || v.Drafts[1].Text != "a note" || v.Drafts[0].Answer == nil || v.Drafts[0].Answer.Choice != "ok" {
		t.Fatalf("%+v", v.Drafts)
	}
	if len(v.Messages) != 0 || !v.Cards[0].NeedsYou {
		t.Fatalf("a draft went out: %+v", v.Messages)
	}

	var sent struct {
		Sent int `json:"sent"`
	}
	r.do("POST", base+"/send", nil, 200, &sent)
	if sent.Sent != 2 {
		t.Fatalf("%+v", sent)
	}
	r.do("GET", base, nil, 200, &v)
	if len(v.Drafts) != 0 || len(v.Messages) != 2 || v.Messages[0].State != msgstate.Sent || v.Messages[0].Who != store.You || v.Cards[0].NeedsYou {
		t.Fatalf("%+v", v)
	}
	if v.OnAgent != 1 {
		t.Fatalf("two messages are one Send: %d", v.OnAgent)
	}
	r.do("POST", base+"/send", nil, 409, nil) // nothing to send
}

// A draft goes only into a thread of its own project.
func TestADraftStaysInItsProject(t *testing.T) {
	r := newRig(t)
	id := r.seeded()
	other, _, err := r.st.Ensure(context.Background(), "Other", nil, store.You)
	if err != nil {
		t.Fatal(err)
	}
	r.do("POST", "/api/projects/"+strconv.FormatInt(id, 10)+"/drafts", DraftBody{Thread: other.Thread, Text: "x"}, 404, nil)
}

// New project creates it with the groups you ticked, and more can be added.
func TestNewProjectAddsTheTickedGroups(t *testing.T) {
	r := newRig(t)
	var p store.Project
	r.do("POST", "/api/projects", ProjectBody{Title: "Mine", Groups: []store.Group{{Kind: store.Repo, Key: "/w/a", Label: "a"}}}, 200, &p)
	r.do("POST", "/api/projects/"+strconv.FormatInt(p.ID, 10)+"/groups", ProjectBody{Groups: []store.Group{{Kind: store.Repo, Key: "/x/b", Label: "b"}}}, 204, nil)
	gs, err := r.st.Groups(context.Background(), p.ID)
	if err != nil || len(gs) != 2 || gs[1].Key != "/x/b" || gs[1].AddedBy != store.You {
		t.Fatalf("%+v %v", gs, err)
	}
	r.do("POST", "/api/projects", ProjectBody{Title: " "}, 422, nil)
}

func repo(t *testing.T, dir, origin string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if origin != "" {
		if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", origin).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
}

// The scan lists GitHub repositories to two levels, each with whether it has a
// record, and nothing it read of the record.
func TestScanListsGitHubRepositoriesToTwoLevels(t *testing.T) {
	r := newRig(t)
	dir := t.TempDir()
	repo(t, filepath.Join(dir, "one"), "git@github.com:o/one.git")
	repo(t, filepath.Join(dir, "group", "two"), "https://github.com/o/two")
	repo(t, filepath.Join(dir, "group", "deep", "three"), "https://github.com/o/three.git")
	repo(t, filepath.Join(dir, "elsewhere"), "https://gitlab.com/o/elsewhere.git")
	repo(t, filepath.Join(dir, "local"), "")
	repo(t, filepath.Join(dir, "one", "inner"), "https://github.com/o/inner.git")
	if err := os.MkdirAll(filepath.Join(dir, "group", "two", ".muda"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "group", "two", ".muda", "muda.toml"), []byte("not toml {{"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got ScanResult
	r.do("GET", "/api/scan?folder="+dir, nil, 200, &got)
	want := []ScannedRepo{
		{Path: filepath.Join(dir, "group", "two"), Name: "o/two", HasRecord: true},
		{Path: filepath.Join(dir, "one"), Name: "o/one"},
	}
	if len(got.Repos) != len(want) || got.Repos[0] != want[0] || got.Repos[1] != want[1] {
		t.Fatalf("%+v", got.Repos)
	}
}

func TestScanNeedsAFolder(t *testing.T) {
	r := newRig(t)
	r.do("GET", "/api/scan?folder=/no/such/folder", nil, 422, nil)
	r.do("GET", "/api/scan", nil, 422, nil)
}
