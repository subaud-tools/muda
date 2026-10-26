package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/subaud-tools/muda"
	"github.com/subaud-tools/muda/internal/content"
	tools "github.com/subaud-tools/tools-common"
)

// parsePack is shared with the pure metadata command checker.
func parsePack(args []string) (string, error) {
	if len(args) != 2 || args[0] != "check" {
		return "", usage("expected pack check DIR")
	}
	return args[1], nil
}

// Pack checks a pack folder with the content check muda runs on its own
// content. checker gives the assembled app's command recognizer.
func Pack(checker func() (func([]string) bool, error)) tools.Command {
	return tools.Command{
		Name: "pack", Summary: "check a recipe pack folder", Synopsis: "check DIR", Subcommands: []string{"check"},
		Help: "Checks a pack folder (pack.toml, standard.md, recipes/, skill/) against this muda's standard, signals and commands and the pack's own stacks and classes. Prints one line per problem and exits 1 if there is any.",
		Run: func(args []string, out, _ io.Writer) error {
			dir, err := parsePack(args)
			if err != nil {
				return err
			}
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				return usage("not a pack folder: " + dir)
			}
			known, err := checker()
			if err != nil {
				return err
			}
			ps := content.CheckPack(muda.Content, os.DirFS(dir), known)
			for _, p := range ps {
				if _, err := fmt.Fprintf(out, "%s: %s\n", filepath.Join(dir, filepath.FromSlash(p.Path)), p.Message); err != nil {
					return err
				}
			}
			if len(ps) > 0 {
				return tools.Exitf(1, "pack check: %d problems in %s", len(ps), dir)
			}
			_, err = fmt.Fprintf(out, "%s: ok\n", dir)
			return err
		},
	}
}
