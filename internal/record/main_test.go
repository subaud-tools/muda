package record

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain caches one pack (class A1, recipe R40) in a temp MUDA_HOME, so the
// default vocabulary never reads the developer's own cache.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "muda-record-test-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("MUDA_HOME", home)
	dir := filepath.Join(home, "data", "packs")
	for name, data := range map[string]string{
		"packs.json":         `{"packs":[{"id":"aws","status":"owned","tree":"aaa"}]}`,
		"aws/pack.toml":      "id = \"aws\"\nmuda = \">=0.1.0\"\nstacks = [\"cdk\"]\n",
		"aws/standard.md":    "# aws\n\n## Catalog\n\n| Class | Waste |\n|---|---|\n| A1 | **Piles** |\n",
		"aws/recipes/R40.md": "---\nid: R40\ntitle: A fix\nwaste: [A1]\nsignals: [slow-step]\nstacks: [cdk]\n---\n\n## Symptom\n\nSlow.\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o700); err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
