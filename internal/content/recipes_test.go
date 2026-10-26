package content_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/subaud-tools/muda"
	"github.com/subaud-tools/muda/internal/content"
)

func TestRecipesParse(t *testing.T) {
	p, err := content.ReadPack(testPack(t))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range p.Recipes {
		ids = append(ids, r.ID)
		if r.Pack != "ci" || r.Title == "" || len(r.Sections) != 7 || len(r.StackSections) != len(r.Stacks) {
			t.Fatalf("bad recipe %+v", r)
		}
	}
	if fmt.Sprint(ids) != "[R1 R5 R6 R11 R13]" {
		t.Fatalf("recipes not in numeric ID order: %v", ids)
	}
}
func checkFixture(t *testing.T, name, want string) {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".md")
	if err != nil {
		t.Fatal(err)
	}
	f := testPack(t)
	f["recipes/R1.md"] = &fstest.MapFile{Data: b}
	// This callback isolates checker diagnostics; assembled registration is
	// tested separately by cmd/muda.TestPackCheck*.
	ps := content.CheckPack(muda.Content, f, func(a []string) bool { return len(a) > 0 && a[0] != "nonexistent" })
	for _, p := range ps {
		if strings.Contains(p.Message, want) {
			return
		}
	}
	t.Fatalf("missing %q in %+v", want, ps)
}
func TestCheckFlagsMissingSection(t *testing.T) {
	checkFixture(t, "missing-section", "missing section: Evidence")
}
func TestCheckFlagsUnknownSignal(t *testing.T) {
	checkFixture(t, "unknown-signal", "unknown signal: made-up")
}
func TestCheckFlagsUnknownStack(t *testing.T) {
	checkFixture(t, "unknown-stack", "unknown stack: made-up")
}
func TestCheckFlagsUnknownWaste(t *testing.T) { checkFixture(t, "unknown-waste", "unknown waste: W99") }
func TestCheckFlagsBadCommand(t *testing.T) {
	checkFixture(t, "bad-command", "unknown command: nonexistent")
}

// fakeFacts swaps the shipped project-fact set for one fake word, so tests
// exercise the mechanism without naming a real project fact.
func fakeFacts(t *testing.T) {
	t.Helper()
	sum := sha256.Sum256([]byte("widgetco"))
	shipped := content.ProjectFacts
	content.ProjectFacts = content.NewFactGuard(hex.EncodeToString(sum[:]))
	t.Cleanup(func() { content.ProjectFacts = shipped })
}
func TestCheckFlagsProjectFacts(t *testing.T) {
	fakeFacts(t)
	checkFixture(t, "project-facts", "project facts are forbidden")
}

// The shipped set names no word in source: every entry is a SHA-256 digest.
func TestShippedProjectFactsAreHashes(t *testing.T) {
	hashes := content.ShippedFactHashes()
	if len(hashes) == 0 {
		t.Fatal("shipped project-fact set is empty")
	}
	hexHash := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, h := range hashes {
		if !hexHash.MatchString(h) {
			t.Errorf("not a 64-hex SHA-256 digest: %q", h)
		}
	}
}
func TestNoProjectFacts(t *testing.T) {
	for _, dir := range []string{"standard", "skills"} {
		err := fs.WalkDir(muda.Content, dir, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			b, e := fs.ReadFile(muda.Content, p)
			if e != nil {
				return e
			}
			// The same word-bounded pattern Check uses (single source).
			if m := content.ProjectFacts.Find(string(b)); m != "" {
				t.Errorf("%s: %s", p, m)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Final review M7: project names are caught as words, not as substrings of
// ordinary words. Fake words stand in for the real set: "nf" plays a short
// word that ordinary words contain, "widgetco" a name with a plural.
func TestProjectFactsWordBoundaries(t *testing.T) {
	var hashes []string
	for _, w := range []string{"nf", "widgetco"} {
		sum := sha256.Sum256([]byte(w))
		hashes = append(hashes, hex.EncodeToString(sum[:]))
	}
	g := content.NewFactGuard(hashes...)
	for _, text := range []string{"conflict", "Conflicting edits", "influence", "inflate", "inflated", "unconflicted", "widgetcorp", "awidgetco", "nfx", ""} {
		if m := g.Find(text); m != "" {
			t.Errorf("ordinary word flagged: %q (%q)", text, m)
		}
	}
	for text, want := range map[string]string{
		"NF": "NF", "the nf season": "nf", "NF stats": "NF", "nf": "nf",
		"widgetco": "widgetco", "Widgetcos": "Widgetcos", "WIDGETCO": "WIDGETCO",
		"github.com/widgetco/muda": "widgetco", "see (Widgetco).": "Widgetco",
	} {
		if m := g.Find(text); m != want {
			t.Errorf("Find(%q) = %q, want %q", text, m, want)
		}
	}
}

func TestCheckSectionOrder(t *testing.T) {
	f := testPack(t)
	text := strings.ReplaceAll(string(f["recipes/R1.md"].Data), "## Symptom", "## TEMP")
	text = strings.ReplaceAll(text, "## Detect", "## Symptom")
	text = strings.ReplaceAll(text, "## TEMP", "## Detect")
	f["recipes/R1.md"] = &fstest.MapFile{Data: []byte(text)}
	if len(content.CheckPack(muda.Content, f, func([]string) bool { return true })) == 0 {
		t.Fatal("out-of-order sections accepted")
	}
}

// Skills may hand evidence off with one `muda … > file.json` redirect (its
// target is validated by skills.Lint); recipes and the standard may not.
func TestCheckRedirectOnlyInSkills(t *testing.T) {
	f := fstest.MapFS{}
	err := fs.WalkDir(muda.Content, ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil || d.IsDir() {
			return e
		}
		b, e := fs.ReadFile(muda.Content, p)
		f[p] = &fstest.MapFile{Data: b}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	block := []byte("```sh\nmuda measure > measure.json\n```\n")
	f["skills/muda-measure/SKILL.md"] = &fstest.MapFile{Data: block}
	f["standard/redirect.md"] = &fstest.MapFile{Data: block}
	// Only a direct skills/<name>/SKILL.md is linted by skills.Lint; any other
	// Markdown under skills/ stays strict.
	f["skills/muda-measure/notes.md"] = &fstest.MapFile{Data: []byte("```sh\nmuda record show > .muda/baseline.json\n```\n")}
	f["skills/muda-measure/ref/SKILL.md"] = &fstest.MapFile{Data: block}
	f["skills/SKILL.md"] = &fstest.MapFile{Data: block}
	flagged := map[string]bool{}
	for _, p := range content.Check(f, func(a []string) bool { return len(a) >= 1 && (a[0] == "measure" || a[0] == "record") }) {
		flagged[p.Path] = true
	}
	for p, want := range map[string]bool{"skills/muda-measure/SKILL.md": false, "standard/redirect.md": true,
		"skills/muda-measure/notes.md": true, "skills/muda-measure/ref/SKILL.md": true, "skills/SKILL.md": true} {
		if flagged[p] != want {
			t.Errorf("%s flagged=%v, want %v", p, flagged[p], want)
		}
	}
}

// muda's own repository is where recipe submissions go, so shipped content may
// name it; every other use of its owner stays a project fact.
func TestProjectFactsAllowMudaHome(t *testing.T) {
	for _, s := range []string{"gh issue create -R " + content.Home + " --label x", "https://github.com/" + content.Home + "/issues/new"} {
		if m := content.ProjectFacts.Find(s); m != "" {
			t.Errorf("%q flagged %q", s, m)
		}
	}
	owner, _, _ := strings.Cut(content.Home, "/")
	for _, s := range []string{owner + "/tackle", content.Home + "-private", content.Home + "x", "by " + owner} {
		if content.ProjectFacts.Find(s) == "" {
			t.Errorf("%q not flagged", s)
		}
	}
}
