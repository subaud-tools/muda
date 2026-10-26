package cli

import (
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/subaud-tools/muda/internal/content"
	"github.com/subaud-tools/muda/internal/packs"
	"github.com/subaud-tools/muda/internal/signal"
	tools "github.com/subaud-tools/tools-common"
)

func recipesFlags() *flag.FlagSet {
	fs := flag.NewFlagSet("recipes", flag.ContinueOnError)
	fs.String("waste", "", "filter by W-number")
	fs.String("signal", "", "filter by signal ID")
	fs.String("stack", "", "filter by stack")
	return fs
}
func recipeFlags() *flag.FlagSet {
	fs := flag.NewFlagSet("recipe", flag.ContinueOnError)
	fs.String("stack", "", "select one stack, keeping the core sections")
	return fs
}
func flagString(fs *flag.FlagSet, name string) string { return fs.Lookup(name).Value.String() }
func usage(msg string) error                          { return tools.UsageError{Msg: msg} }

// Recipes lists matching recipes of the cached packs, by pack and in numeric
// ID order. Filters intersect.
func Recipes() tools.Command {
	return tools.Command{
		Name: "recipes", Summary: "list delivery-waste recipes", Synopsis: "[--waste W] [--signal ID] [--stack S]", NewFlags: recipesFlags,
		Run: func(args []string, out, errw io.Writer) error {
			fs := recipesFlags()
			if err := tools.ParseFlags(fs, args, out); err != nil {
				return err
			}
			if fs.NArg() != 0 {
				return usage("recipes takes no positional arguments")
			}
			w, s, stack := flagString(fs, "waste"), flagString(fs, "signal"), flagString(fs, "stack")
			lib, err := library(errw)
			if err != nil {
				return err
			}
			if len(lib.Recipes) == 0 && len(lib.Packs) > 0 {
				ids := make([]string, len(lib.Packs))
				for i, p := range lib.Packs {
					ids[i] = p.ID
				}
				return tools.Exitf(1, "the cached packs (%s) hold no recipes yet", strings.Join(ids, ", ")).WithHint("run `muda packs sync` for new recipes; more packs are sold at " + packs.AccountPage)
			}
			if len(lib.Recipes) == 0 {
				return tools.Exitf(1, "no recipe packs are cached").WithHint("sign this machine in with `muda login`, then run `muda packs sync`; packs are sold at " + packs.AccountPage)
			}
			if w != "" && !slices.Contains(lib.Wastes, w) {
				return usage("unknown waste: " + w)
			}
			if s != "" && !signal.Known(s) {
				return usage("unknown signal: " + s)
			}
			if stack != "" && !slices.Contains(lib.Stacks, stack) {
				return usage("unknown stack: " + stack)
			}
			for _, r := range lib.Recipes {
				if (w == "" || slices.Contains(r.Waste, w)) && (s == "" || slices.Contains(r.Signals, s)) && (stack == "" || slices.Contains(r.Stacks, stack)) {
					if _, err := fmt.Fprintf(out, "%s: %s\n", r.ID, r.Title); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
}

// Recipe prints all sections, or the core sections and one declared stack.
// A recipe not in a cached pack names the packs the account doesn't own.
func Recipe() tools.Command {
	return tools.Command{
		Name: "recipe", Summary: "read a delivery-waste recipe", Synopsis: "ID [--stack S]", NewFlags: recipeFlags,
		Run: func(args []string, out, errw io.Writer) error {
			if len(args) == 0 || strings.HasPrefix(args[0], "-") {
				return usage("expected recipe ID")
			}
			id := args[0]
			fs := recipeFlags()
			if err := tools.ParseFlags(fs, args[1:], out); err != nil {
				return err
			}
			if fs.NArg() != 0 {
				return usage("unexpected recipe arguments")
			}
			lib, err := library(errw)
			if err != nil {
				return err
			}
			for _, r := range lib.Recipes {
				if r.ID != id {
					continue
				}
				stack := flagString(fs, "stack")
				if stack != "" && !slices.Contains(r.Stacks, stack) {
					return usage("recipe has no stack: " + stack)
				}
				var b strings.Builder
				fmt.Fprintf(&b, "# %s: %s\n\n", r.ID, r.Title)
				if stack == "" {
					b.WriteString(r.Body)
				} else {
					for _, name := range content.SectionNames {
						fmt.Fprintf(&b, "## %s\n\n", name)
						section := r.Sections[name]
						if name == "Stacks" {
							section = r.StackSections[stack]
						}
						b.WriteString(strings.TrimSpace(section))
						b.WriteString("\n\n")
					}
				}
				_, err := io.WriteString(out, b.String())
				return err
			}
			return noRecipes("unknown recipe: " + id)
		},
	}
}
