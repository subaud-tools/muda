package skills_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/subaud-tools/muda/internal/skills"
	commonskills "github.com/subaud-tools/tools-common/skills"
)

func TestBuiltinOwnershipMatchesSharedMarkers(t *testing.T) {
	for _, body := range []string{
		"---\nname: muda\nmuda-owned: muda\n---\nstock\n",
		"---\r\nname: muda\r\nmuda-owned: muda\r\n---\r\nstock\r\n",
		"---\nname: muda\n---\npersonal\n",
		"---\nname: muda\nmuda-owned: true\n---\n",
		"---\nname: muda\nmuda-owned: muda-fix\n---\n",
		"---\nname: muda-fix\nmuda-owned: muda\n---\n",
	} {
		want := commonskills.OwnsWithMarkers("muda", []byte(body), map[string]string{"muda-owned": "muda"})
		if got := skills.OwnsBuiltin("muda", []byte(body)); got != want {
			t.Fatalf("ownership %q = %v, shared = %v", body, got, want)
		}
	}
	if skills.OwnsBuiltin("private", []byte("---\nname: private\nmuda-owned: private\n---\n")) {
		t.Fatal("unknown stock name accepted")
	}
}

func TestBuiltinInstallPreservesUnmarkedAndUpgradesMarked(t *testing.T) {
	for _, agent := range []string{"claude", "pi"} {
		for _, marked := range []bool{false, true} {
			home := t.TempDir()
			_, rel, err := skills.Target(agent, "user", home, "")
			if err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(home, rel, "muda", "SKILL.md")
			old := "---\nname: muda\n"
			if marked {
				old += "muda-owned: muda\n"
			}
			old += "---\nold content\n"
			if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dest, []byte(old), 0600); err != nil {
				t.Fatal(err)
			}
			f := fixture()
			_, err = skills.Install(f, agent, "user", home, "")
			got, readErr := os.ReadFile(dest)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !marked {
				if err == nil || string(got) != old {
					t.Fatalf("unmarked file changed: %v %q", err, got)
				}
			} else if err != nil || !bytes.Equal(got, f["skills/muda/SKILL.md"].Data) {
				t.Fatalf("marked upgrade: %v %q", err, got)
			}
		}
	}
	for _, body := range []string{"---\nname: muda\n---\n", "---\nname: muda\nmuda-owned: true\n---\n"} {
		f := fixture()
		f["skills/muda/SKILL.md"].Data = []byte(body)
		if _, err := skills.Install(f, "pi", "user", t.TempDir(), ""); err == nil || !strings.Contains(err.Error(), "unowned source") {
			t.Fatalf("unowned desired content accepted: %v", err)
		}
	}
}
