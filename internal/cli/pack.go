package cli

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/subaud-tools/muda"
	"github.com/subaud-tools/muda/internal/content"
	"github.com/subaud-tools/muda/internal/version"
	tools "github.com/subaud-tools/tools-common"
)

// parsePack is shared with the pure metadata command checker.
func parsePack(args []string) ([]string, error) {
	if len(args) < 2 || args[0] != "check" {
		return nil, usage("expected pack check DIR...")
	}
	return args[1:], nil
}

// Pack checks a pack folder with the content check muda runs on its own
// content. checker gives the assembled app's command recognizer.
func Pack(checker func() (func([]string) bool, error)) tools.Command {
	return tools.Command{
		Name: "pack", Summary: "check a recipe pack folder", Synopsis: "check DIR...", Subcommands: []string{"check"},
		Help: "Checks a pack folder (pack.toml, standard.md, recipes/, skill/) against this muda's standard, signals and commands and the pack's own stacks and classes. Given several folders, it also loads them together, so two packs that claim the same class or recipe id fail as they would for a buyer who owns both. Prints one line per problem and exits 1 if there is any.",
		Run: func(args []string, out, _ io.Writer) error {
			dirs, err := parsePack(args)
			if err != nil {
				return err
			}
			for _, dir := range dirs {
				if info, err := os.Stat(dir); err != nil || !info.IsDir() {
					return usage("not a pack folder: " + dir)
				}
			}
			known, err := checker()
			if err != nil {
				return err
			}
			problems := 0
			var fss []fs.FS
			for _, dir := range dirs {
				ps := content.CheckPack(muda.Content, os.DirFS(dir), known)
				for _, p := range ps {
					if _, err := fmt.Fprintf(out, "%s: %s\n", filepath.Join(dir, filepath.FromSlash(p.Path)), p.Message); err != nil {
						return err
					}
				}
				problems += len(ps)
				fss = append(fss, os.DirFS(dir))
			}
			if len(dirs) > 1 && problems == 0 {
				if _, err := content.Load(muda.Content, fss, version.Version().Number); err != nil {
					if _, err := fmt.Fprintf(out, "packs together: %s\n", err); err != nil {
						return err
					}
					problems++
				}
			}
			if problems > 0 {
				return tools.Exitf(1, "pack check: %d problems", problems)
			}
			for _, dir := range dirs {
				if _, err := fmt.Fprintf(out, "%s: ok\n", dir); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
