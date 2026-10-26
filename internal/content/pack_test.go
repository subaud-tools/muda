package content_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/subaud-tools/muda"
	"github.com/subaud-tools/muda/internal/content"
)

// packFixture is a pack folder with one catalog row per class and one
// minimal recipe per recipe ID.
func packFixture(id, muda string, classes []string, recipes ...string) fstest.MapFS {
	f := fstest.MapFS{"pack.toml": {Data: []byte("id = \"" + id + "\"\nmuda = \"" + muda + "\"\nstacks = [\"" + id + "-stack\"]\n")}}
	if len(classes) > 0 {
		var b strings.Builder
		b.WriteString("# The " + id + " standard\n\n## Catalog\n\n| Class | Waste |\n|---|---|\n")
		for _, c := range classes {
			b.WriteString("| " + c + " | **Something** |\n")
		}
		f["standard.md"] = &fstest.MapFile{Data: []byte(b.String())}
	}
	for _, r := range recipes {
		f["recipes/"+r+".md"] = &fstest.MapFile{Data: []byte("---\nid: " + r + "\ntitle: A fix\nwaste: [W1]\nsignals: [slow-step]\nstacks: [" + id + "-stack]\n---\n\n## Symptom\n\nSlow.\n")}
	}
	return f
}

func TestLoadKnowsAPacksClasses(t *testing.T) {
	lib, err := content.Load(muda.Content, []fs.FS{packFixture("aws", ">=0.1.0", []string{"A1", "A2"}, "R40")}, "0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"W1", "W10", "A1", "A2"} {
		if !slices.Contains(lib.Wastes, w) {
			t.Errorf("class %s not known: %v", w, lib.Wastes)
		}
	}
	if !slices.Contains(lib.Stacks, "aws-stack") {
		t.Errorf("pack stack not known: %v", lib.Stacks)
	}
	i := slices.IndexFunc(lib.Recipes, func(r content.Recipe) bool { return r.ID == "R40" })
	if i < 0 || lib.Recipes[i].Pack != "aws" {
		t.Errorf("pack recipe not listed by pack: %+v", lib.Recipes)
	}
}

func TestLoadRefusesTwoPacksClaimingOneClass(t *testing.T) {
	_, err := content.Load(muda.Content, []fs.FS{
		packFixture("aws", ">=0.1.0", []string{"A1"}),
		packFixture("gcp", ">=0.1.0", []string{"A1"}),
	}, "0.3.0")
	if err == nil || !strings.Contains(err.Error(), "A1") || !strings.Contains(err.Error(), "aws") || !strings.Contains(err.Error(), "gcp") {
		t.Fatalf("want a refusal naming A1, aws and gcp: %v", err)
	}
}

func TestLoadRefusesAPackClaimingMudasOwnID(t *testing.T) {
	_, err := content.Load(muda.Content, []fs.FS{packFixture("aws", ">=0.1.0", []string{"W3"})}, "0.3.0")
	if err == nil || !strings.Contains(err.Error(), "W3") || !strings.Contains(err.Error(), "muda") || !strings.Contains(err.Error(), "aws") {
		t.Fatalf("want a refusal naming W3, muda and aws: %v", err)
	}
}

func TestLoadRefusesTwoPacksClaimingOneRecipe(t *testing.T) {
	_, err := content.Load(muda.Content, []fs.FS{
		packFixture("aws", ">=0.1.0", nil, "R40"),
		packFixture("gcp", ">=0.1.0", nil, "R40"),
	}, "0.3.0")
	if err == nil || !strings.Contains(err.Error(), "R40") || !strings.Contains(err.Error(), "aws") || !strings.Contains(err.Error(), "gcp") {
		t.Fatalf("want a refusal naming R40, aws and gcp: %v", err)
	}
}

func TestLoadSkipsAPackOutsideItsMudaRange(t *testing.T) {
	lib, err := content.Load(muda.Content, []fs.FS{packFixture("aws", ">=0.9.0", []string{"A1"})}, "0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(lib.Wastes, "A1") {
		t.Error("a pack outside its range was loaded")
	}
	if len(lib.Warnings) != 1 || !strings.Contains(lib.Warnings[0], "aws") || !strings.Contains(lib.Warnings[0], ">=0.9.0") {
		t.Errorf("want one warning naming aws and its range: %q", lib.Warnings)
	}
}

func knownAll([]string) bool { return true }

func TestCheckPackPassesAValidPack(t *testing.T) {
	if ps := content.CheckPack(muda.Content, testPack(t), knownAll); len(ps) != 0 {
		t.Fatalf("%+v", ps)
	}
}

func TestCheckPackJudgesStacksByThePacksOwnList(t *testing.T) {
	f := testPack(t)
	f["pack.toml"] = &fstest.MapFile{Data: []byte("id = \"ci\"\nmuda = \">=0.1.0\"\nstacks = [\"github-actions\", \"cdk\", \"python\", \"node\", \"go\"]\n")}
	wantProblem(t, content.CheckPack(muda.Content, f, knownAll), "recipes/R1.md", "unknown stack: docker")
}

func TestCheckPackKnowsThePacksOwnClasses(t *testing.T) {
	f := testPack(t)
	f["standard.md"] = &fstest.MapFile{Data: []byte("# ci\n\n## Catalog\n\n| Class | Waste |\n|---|---|\n| C1 | **Something** |\n")}
	r1 := string(f["recipes/R1.md"].Data)
	f["recipes/R1.md"] = &fstest.MapFile{Data: []byte(strings.Replace(r1, "waste: [W2]", "waste: [W2, C1, C2]", 1))}
	ps := content.CheckPack(muda.Content, f, knownAll)
	wantProblem(t, ps, "recipes/R1.md", "unknown waste: C2")
	for _, p := range ps {
		if strings.Contains(p.Message, "C1") || strings.Contains(p.Message, "W2") {
			t.Errorf("a known class was flagged: %+v", p)
		}
	}
}

func TestCheckPackFlagsAClassMudaOwns(t *testing.T) {
	f := testPack(t)
	f["standard.md"] = &fstest.MapFile{Data: []byte("# ci\n\n## Catalog\n\n| Class | Waste |\n|---|---|\n| W4 | **Again** |\n")}
	wantProblem(t, content.CheckPack(muda.Content, f, knownAll), "standard.md", "class W4 is muda's own")
}

func TestCheckPackFlagsAnUnknownCommandInItsSkill(t *testing.T) {
	f := testPack(t)
	f["skill/SKILL.md"] = &fstest.MapFile{Data: []byte("---\nname: x\n---\n```sh\nmuda nonexistent\n```\n")}
	ps := content.CheckPack(muda.Content, f, func(a []string) bool { return a[0] != "nonexistent" })
	wantProblem(t, ps, "skill/SKILL.md", "unknown command: nonexistent")
}
