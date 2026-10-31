package packs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/subaud-tools/muda/internal/content"
)

const packToml = "id = \"ci\"\nmuda = \">=0.1.0\"\nstacks = [\"go\"]\n"

const recipe40 = "---\nid: R40\ntitle: A fix\nwaste: [W1]\nsignals: [slow-step]\nstacks: [go]\n---\n\n## Symptom\n\nSlow.\n"

// archive is a pack's files as the service sends them: a gzip tar, base64.
func archive(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// cached is a cache that holds ci at tree "aaa", with one old recipe.
func cached(t *testing.T) Cache {
	t.Helper()
	c := Cache{Dir: t.TempDir()}
	for name, data := range map[string]string{"ci/pack.toml": packToml, "ci/old.md": "old"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(c.Dir, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(c.Dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.writeState(State{Packs: []Entry{{ID: "ci", Status: Owned, Tree: "aaa"}}}); err != nil {
		t.Fatal(err)
	}
	return c
}

// service answers /packs/sync with answer, after checking the request.
func service(t *testing.T, answer string, check func(r *http.Request, have map[string]string)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Have map[string]string `json:"have"`
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &in); err != nil {
			t.Errorf("request body %s: %v", b, err)
		}
		if check != nil {
			check(r, in.Have)
		}
		if strings.HasPrefix(answer, "{\"error\"") {
			w.WriteHeader(http.StatusUnauthorized)
		}
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestSyncSendsTheCachedHashesWithTheToken(t *testing.T) {
	c := cached(t)
	api := service(t, `{"packs":[{"id":"ci","status":"unchanged","tree":"aaa"}]}`, func(r *http.Request, have map[string]string) {
		if r.Method != http.MethodPost || r.URL.Path != "/packs/sync" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sbt_test" {
			t.Errorf("Authorization %q", got)
		}
		if len(have) != 1 || have["ci"] != "aaa" {
			t.Errorf("have %v", have)
		}
	})
	rs, err := c.Sync(context.Background(), api, nil, "sbt_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Status != "unchanged" {
		t.Fatalf("%+v", rs)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "ci", "old.md")); err != nil {
		t.Error("an unchanged pack was touched:", err)
	}
}

func TestSyncReplacesAChangedPack(t *testing.T) {
	c := cached(t)
	api := service(t, `{"packs":[{"id":"ci","status":"changed","tree":"bbb","archive":"`+archive(t, map[string]string{"pack.toml": packToml, "recipes/R40.md": recipe40})+`"}]}`, nil)
	if _, err := c.Sync(context.Background(), api, nil, "sbt_test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "ci", "recipes", "R40.md")); err != nil {
		t.Error("the new recipe is not cached:", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "ci", "old.md")); err == nil {
		t.Error("the old copy's file is still cached")
	}
	s, err := c.State()
	if err != nil || len(s.Packs) != 1 || s.Packs[0].Tree != "bbb" {
		t.Fatalf("state %+v: %v", s, err)
	}
}

func TestSyncRemovesAPackNoLongerOwned(t *testing.T) {
	c := cached(t)
	api := service(t, `{"packs":[{"id":"ci","status":"not_owned","account":"https://subaud.tools/account/"}]}`, nil)
	if _, err := c.Sync(context.Background(), api, nil, "sbt_test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "ci")); err == nil {
		t.Error("a pack the account no longer owns is still cached")
	}
	s, _ := c.State()
	if len(s.Packs) != 1 || s.Packs[0].Status != NotOwned || s.Packs[0].Account != "https://subaud.tools/account/" {
		t.Fatalf("state %+v", s)
	}
}

func TestSyncRefusesAnArchiveThatLeavesThePack(t *testing.T) {
	c := cached(t)
	api := service(t, `{"packs":[{"id":"ci","status":"changed","tree":"bbb","archive":"`+archive(t, map[string]string{"pack.toml": packToml, "../escaped.md": "x"})+`"}]}`, nil)
	if _, err := c.Sync(context.Background(), api, nil, "sbt_test"); err == nil {
		t.Fatal("an archive path outside the pack was accepted")
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "escaped.md")); err == nil {
		t.Error("a file was written outside the pack")
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "ci", "old.md")); err != nil {
		t.Error("the cached copy was lost:", err)
	}
}

func TestSyncSignInErrorSaysToLogIn(t *testing.T) {
	c := cached(t)
	api := service(t, `{"error":"sign_in"}`, nil)
	_, err := c.Sync(context.Background(), api, nil, "sbt_expired")
	if err == nil || !strings.Contains(err.Error(), "muda login") {
		t.Fatalf("want a hint to run muda login: %v", err)
	}
}

func TestLibraryReadsTheOwnedCachedPacks(t *testing.T) {
	c := cached(t)
	if err := os.MkdirAll(filepath.Join(c.Dir, "ci", "recipes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Dir, "ci", "recipes", "R40.md"), []byte(recipe40), 0o600); err != nil {
		t.Fatal(err)
	}
	lib, err := c.Library("0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(lib.Recipes, func(r content.Recipe) bool { return r.ID == "R40" && r.Pack == "ci" }) {
		t.Errorf("cached recipe missing: %+v", lib.Recipes)
	}
}

// testdata/packs-sync-changed.json is the access service's own example answer,
// copied from tools-ops access/API.md (example packs-sync-changed), whose
// contract test checks it against the service. muda must read it as is.
func TestSyncReadsTheServicesDocumentedAnswer(t *testing.T) {
	answer, err := os.ReadFile("testdata/packs-sync-changed.json")
	if err != nil {
		t.Fatal(err)
	}
	c := cached(t)
	api := service(t, string(answer), nil)
	rs, err := c.Sync(context.Background(), api, nil, "sbt_test")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rs {
		got[r.ID] = r.Status
	}
	if got["ci"] != "changed" || got["aws"] != "unavailable" {
		t.Fatalf("statuses %v", got)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "ci", "pack.toml")); err != nil {
		t.Error("the documented archive was not unpacked:", err)
	}
}

func TestSkillsListsTheOwnedCachedPacksThatHaveOne(t *testing.T) {
	c := cached(t) // "ci" has no skill
	for _, name := range []string{"widget/skill/SKILL.md", "gone/skill/SKILL.md"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(c.Dir, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(c.Dir, name), []byte("text"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.writeState(State{Packs: []Entry{{ID: "ci", Status: Owned, Tree: "aaa"}, {ID: "widget", Status: Owned, Tree: "bbb"}, {ID: "gone", Status: NotOwned}}}); err != nil {
		t.Fatal(err)
	}
	got, err := c.Skills()
	if err != nil || len(got) != 1 || got[0].ID != "widget" {
		t.Fatalf("%v %v", got, err)
	}
	if b, err := fs.ReadFile(got[0].Skill, "SKILL.md"); err != nil || string(b) != "text" {
		t.Fatalf("%q %v", b, err)
	}
}

func TestSkillsOfAnEmptyCacheIsNone(t *testing.T) {
	got, err := Cache{Dir: t.TempDir()}.Skills()
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}
