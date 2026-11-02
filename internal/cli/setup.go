package cli

import (
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/subaud-tools/muda"
	"github.com/subaud-tools/muda/internal/skills"
	"github.com/subaud-tools/muda/internal/version"
	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/setupbundle"
)

//go:embed setup.toml
var setupManifest string

// SetupBundle builds only the embedded stock integration; it never reads packs
// or installs into the user's harnesses.
func SetupBundle() tools.Command {
	source, err := setupSource()
	cmd := setupbundle.BundleCommand("muda", version.Version().Number, source)
	if err != nil {
		cmd.Run = func(_ []string, _, _ io.Writer) error { return err }
	}
	return cmd
}

func setupSource() (setupbundle.Source, error) {
	files := muda.BuiltinSkills()
	paths, err := fs.Glob(files, "*/SKILL.md")
	if err != nil {
		return setupbundle.Source{}, err
	}
	var manifest strings.Builder
	manifest.WriteString(setupManifest)
	targets := skills.UserTargets()
	for _, p := range paths {
		name := strings.TrimSuffix(p, "/SKILL.md")
		body, err := fs.ReadFile(files, p)
		if err != nil {
			return setupbundle.Source{}, err
		}
		if !skills.OwnsBuiltin(name, body) {
			return setupbundle.Source{}, fmt.Errorf("unowned stock skill: %s", p)
		}
		for _, agent := range slices.Sorted(maps.Keys(targets)) {
			_, _ = fmt.Fprintf(&manifest, "\n[[packages.integration.managed-file]]\nfrom = %q\nto = %q\nmode = \"0644\"\nadopt-markers = { muda-owned = %q }\n", p, targets[agent]+"/"+p, name)
		}
	}
	return setupbundle.Source{Manifest: []byte(manifest.String()), Files: files}, nil
}
