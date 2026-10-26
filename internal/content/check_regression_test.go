package content_test

import (
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/subaud-tools/muda"
	"github.com/subaud-tools/muda/internal/content"
)

// libraryFixture is muda's own standard.
func libraryFixture(t *testing.T) fstest.MapFS {
	t.Helper()
	b, err := fs.ReadFile(muda.Content, "standard/standard.md")
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{"standard/standard.md": {Data: b}}
}

// testPack is the test pack in testdata/pack: synthetic recipes, not the ci
// pack's.
func testPack(t *testing.T) fstest.MapFS {
	t.Helper()
	f := fstest.MapFS{}
	src := os.DirFS("testdata/pack")
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil || d.IsDir() {
			return e
		}
		b, e := fs.ReadFile(src, p)
		f[p] = &fstest.MapFile{Data: b}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func wantProblem(t *testing.T, ps []content.Problem, path, message string) {
	t.Helper()
	for _, p := range ps {
		if p.Path == path && strings.Contains(p.Message, message) {
			return
		}
	}
	t.Fatalf("missing %s: %s in %+v", path, message, ps)
}

type unreadableFS struct {
	fs.FS
	operation string
}

func (f unreadableFS) ReadDir(p string) ([]fs.DirEntry, error) {
	if p == "recipes" && f.operation == "readdir" {
		return nil, &fs.PathError{Op: "readdir", Path: p, Err: fs.ErrPermission}
	}
	return fs.ReadDir(f.FS, p)
}
func (f unreadableFS) Stat(p string) (fs.FileInfo, error) {
	if p == "recipes" && f.operation == "stat" {
		return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrPermission}
	}
	return fs.Stat(f.FS, p)
}
func TestCheckRequiredContent(t *testing.T) {
	t.Run("missing standard", func(t *testing.T) {
		f := libraryFixture(t)
		delete(f, "standard/standard.md")
		wantProblem(t, content.Check(f, func([]string) bool { return true }), "standard/standard.md", "read required standard")
	})
	for _, op := range []string{"stat", "readdir"} {
		t.Run(op, func(t *testing.T) {
			wantProblem(t, content.CheckPack(muda.Content, unreadableFS{testPack(t), op}, func([]string) bool { return true }), "recipes", "permission denied")
		})
	}
}

// The aws pack ships its standard and skill before any recipe.
func TestCheckPackAllowsNoRecipes(t *testing.T) {
	f := fstest.MapFS{"pack.toml": testPack(t)["pack.toml"]}
	if ps := content.CheckPack(muda.Content, f, func([]string) bool { return true }); len(ps) != 0 {
		t.Fatal(ps)
	}
}
func TestCheckDetectVerifyExamples(t *testing.T) {
	for _, section := range []string{"Detect", "Verify"} {
		for _, language := range []string{"bash", "", "prose"} {
			t.Run(section+"/"+language, func(t *testing.T) {
				f := testPack(t)
				text := string(f["recipes/R1.md"].Data)
				start := strings.Index(text, "## "+section+"\n")
				end := strings.Index(text[start+4:], "\n## ") + start + 4
				body := text[start:end]
				if language == "prose" {
					body = "## " + section + "\n\nRun diagnostics.\n"
				} else {
					body = strings.ReplaceAll(body, "```sh", "```"+language)
					body = strings.ReplaceAll(body, "muda scan", "muda nonexistent")
					body = strings.ReplaceAll(body, "muda compare", "muda nonexistent")
				}
				f["recipes/R1.md"].Data = []byte(text[:start] + body + text[end:])
				ps := content.CheckPack(muda.Content, f, func(a []string) bool { return a[0] != "nonexistent" })
				wantProblem(t, ps, "recipes/R1.md", "missing sh command example: "+section)
				if language != "prose" {
					wantProblem(t, ps, "recipes/R1.md", "unsupported example fence language")
					wantProblem(t, ps, "recipes/R1.md", "unknown command: nonexistent")
				}
			})
		}
	}
}
func TestCheckMalformedRecipeStillChecksRaw(t *testing.T) {
	f := testPack(t)
	text := strings.Replace(string(f["recipes/R1.md"].Data), "id: R1", "unexpected: value", 1)
	text = strings.Replace(text, "muda scan", "muda nonexistent", 1) + "\nwidgetco\n"
	f["recipes/R1.md"].Data = []byte(text)
	fakeFacts(t)
	ps := content.CheckPack(muda.Content, f, func(a []string) bool { return a[0] != "nonexistent" })
	wantProblem(t, ps, "recipes/R1.md", "unknown command: nonexistent")
	wantProblem(t, ps, "recipes/R1.md", "project facts are forbidden")
}
func TestCheckAllowsExternalBootstrapOutsideRecipeExamples(t *testing.T) {
	f := libraryFixture(t)
	for _, p := range []string{"skills/README.md", "standard/bootstrap.md"} {
		f[p] = &fstest.MapFile{Data: []byte("```sh\ncurl https://example.com/install.sh\n```\n")}
	}
	if ps := content.Check(f, func([]string) bool { return true }); len(ps) != 0 {
		t.Fatal(ps)
	}
}
