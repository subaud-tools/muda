package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/subaud-tools/muda/internal/cli"
)

// TestMain keeps every test off the developer's own pack cache and token.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "muda-test-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("MUDA_HOME", dir)
	_ = os.Setenv("SUBAUD_HOME", dir)
	_ = os.Unsetenv("SUBAUD_TOKEN")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

const r40 = "---\nid: R40\ntitle: A fix from the pack\nwaste: [W1]\nsignals: [slow-step]\nstacks: [go]\n---\n\n## Symptom\n\nSlow.\n"

// packArchive is the base64 gzip tar the access service sends for a pack.
func packArchive(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, data); err != nil {
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

// signedIn points muda's cache and the subaud token at temp dirs, and the
// access service at answer.
func signedIn(t *testing.T, token, answer string) cli.Env {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MUDA_HOME", t.TempDir())
	t.Setenv("SUBAUD_HOME", t.TempDir())
	t.Setenv("SUBAUD_TOKEN", token)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"sign_in"}`)
			return
		}
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(srv.Close)
	env := cli.DefaultEnv()
	env.PacksAPI = srv.URL
	return env
}

func run(t *testing.T, env cli.Env, args ...string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	code := newApp(env).Dispatch(args, &out, &errs)
	return code, out.String(), errs.String()
}

func TestPacksSyncThenRecipesReadThePack(t *testing.T) {
	env := signedIn(t, "sbt_test", `{"packs":[{"id":"ci","status":"changed","tree":"bbb","archive":"`+
		packArchive(t, map[string]string{"pack.toml": "id = \"ci\"\nmuda = \">=0.1.0\"\nstacks = [\"go\"]\n", "recipes/R40.md": r40})+`"},`+
		`{"id":"gcp","status":"not_owned","account":"https://subaud.tools/account/"}]}`)
	if code, out, errs := run(t, env, "packs", "sync"); code != 0 || !strings.Contains(out, "ci: updated") || !strings.Contains(out, "gcp: not owned") {
		t.Fatalf("sync: %d %s %s", code, out, errs)
	}
	if code, out, errs := run(t, env, "packs"); code != 0 || !strings.Contains(out, "ci: owned, cached bbb; stacks go") || !strings.Contains(out, "gcp: not owned") {
		t.Errorf("packs: %d %s %s", code, out, errs)
	}
	if code, out, errs := run(t, env, "recipes", "--stack", "go"); code != 0 || !strings.Contains(out, "R40: A fix from the pack") {
		t.Errorf("recipes: %d %s %s", code, out, errs)
	}
	if code, out, errs := run(t, env, "recipe", "R40"); code != 0 || !strings.Contains(out, "# R40: A fix from the pack") {
		t.Errorf("recipe: %d %s %s", code, out, errs)
	}
}

func TestRecipeNotFoundNamesThePacksNotOwned(t *testing.T) {
	env := signedIn(t, "sbt_test", `{"packs":[{"id":"gcp","status":"not_owned","account":"https://subaud.tools/account/"}]}`)
	if code, _, errs := run(t, env, "packs", "sync"); code != 0 {
		t.Fatal(errs)
	}
	code, _, errs := run(t, env, "recipe", "R99")
	if code != 2 || !strings.Contains(errs, "gcp") || !strings.Contains(errs, "https://subaud.tools/account/") {
		t.Errorf("%d %s", code, errs)
	}
}

func TestPacksSyncNeedsASignedInMachine(t *testing.T) {
	env := signedIn(t, "", `{"packs":[]}`)
	if err := os.MkdirAll(filepath.Join(os.Getenv("SUBAUD_HOME"), "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	code, _, errs := run(t, env, "packs", "sync")
	if code != 1 || !strings.Contains(errs, "muda login") {
		t.Errorf("%d %s", code, errs)
	}
}

const widgetToml = "id = \"widget\"\nmuda = \">=0.1.0\"\nstacks = [\"go\"]\n"
const widgetSkill = "---\nname: muda-widget\ndescription: The pack's own words.\n---\n\nFirst.\n"

func TestSkillsInstallBringsASyncedPacksSkillAndSyncRefreshesIt(t *testing.T) {
	env := signedIn(t, "sbt_test", `{"packs":[{"id":"widget","status":"changed","tree":"bbb","archive":"`+
		packArchive(t, map[string]string{"pack.toml": widgetToml, "skill/SKILL.md": widgetSkill})+`"}]}`)
	home := os.Getenv("HOME")
	p := filepath.Join(home, ".claude/skills/muda-widget/SKILL.md")
	if code, _, errs := run(t, env, "packs", "sync"); code != 0 {
		t.Fatal(errs)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("sync installed a new skill")
	}
	code, out, errs := run(t, env, "skills", "install", "--agent", "claude")
	if code != 0 || !strings.Contains(out, p) {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	if b, _ := os.ReadFile(p); string(b) != widgetSkill {
		t.Fatal(string(b))
	}

	newer := strings.ReplaceAll(widgetSkill, "First", "Second")
	env = resync(t, `{"packs":[{"id":"widget","status":"changed","tree":"ccc","archive":"`+
		packArchive(t, map[string]string{"pack.toml": widgetToml, "skill/SKILL.md": newer})+`"}]}`)
	code, out, errs = run(t, env, "packs", "sync")
	if code != 0 || !strings.Contains(out, p) {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	if b, _ := os.ReadFile(p); string(b) != newer {
		t.Fatal(string(b))
	}
	if _, err := os.Stat(filepath.Join(home, ".pi/agent/skills/muda-widget")); !os.IsNotExist(err) {
		t.Fatal("sync installed for an agent that had no copy")
	}
}

func TestSkillsInstallWithNoPackInstallsOnlyMudas(t *testing.T) {
	env := signedIn(t, "sbt_test", `{"packs":[]}`)
	code, out, errs := run(t, env, "skills", "install", "--agent", "pi")
	if code != 0 || strings.Count(out, "SKILL.md") != 6 {
		t.Fatalf("%d %s %s", code, out, errs)
	}
}

// resync points the access service at a new answer, keeping the cache and home.
func resync(t *testing.T, answer string) cli.Env {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, answer) }))
	t.Cleanup(srv.Close)
	env := cli.DefaultEnv()
	env.PacksAPI = srv.URL
	return env
}
