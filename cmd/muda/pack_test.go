package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePack writes a pack folder holding the test pack's R1 under the given
// stacks.
func writePack(t *testing.T, stacks string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ci")
	r1, err := os.ReadFile("../../internal/content/testdata/pack/recipes/R1.md")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"pack.toml":     "id = \"ci\"\nmuda = \">=0.1.0\"\nstacks = [" + stacks + "]\n",
		"recipes/R1.md": string(r1),
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPackCheckPassesAValidPack(t *testing.T) {
	dir := writePack(t, `"github-actions", "docker", "python", "node", "go"`)
	var out, errs bytes.Buffer
	if code := NewApp().Dispatch([]string{"pack", "check", dir}, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s %s", code, out.String(), errs.String())
	}
}

func TestPackCheckReportsEachProblem(t *testing.T) {
	dir := writePack(t, `"github-actions", "python", "node", "go"`)
	var out, errs bytes.Buffer
	if code := NewApp().Dispatch([]string{"pack", "check", dir}, &out, &errs); code != 1 {
		t.Fatalf("exit %d: %s %s", code, out.String(), errs.String())
	}
	if want := filepath.Join(dir, "recipes", "R1.md") + ": unknown stack: docker"; !strings.Contains(out.String(), want) {
		t.Errorf("want %q in %s", want, out.String())
	}
}
