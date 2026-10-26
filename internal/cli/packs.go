package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/subaud-tools/muda/internal/content"
	"github.com/subaud-tools/muda/internal/packs"
	"github.com/subaud-tools/muda/internal/version"
	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/auth"
)

// parsePacks is shared with the pure metadata command checker.
func parsePacks(args []string) (sync bool, err error) {
	switch {
	case len(args) == 0:
		return false, nil
	case len(args) == 1 && args[0] == "sync":
		return true, nil
	}
	return false, usage("expected packs or packs sync")
}

// Packs lists the packs the account owned at the last sync, the version cached
// and its stacks, or syncs them with the access service.
func Packs(env Env) tools.Command {
	return tools.Command{
		Name: "packs", Summary: "list or sync the recipe packs this account owns", Synopsis: "[sync]", Subcommands: []string{"sync"},
		Help: "muda packs lists what the last sync said of each pack: owned and the version cached, or not owned. muda packs sync sends the cached versions to the access service with this machine's subaud.tools token (muda login) and replaces each pack that changed.",
		Run: func(args []string, out, _ io.Writer) error {
			sync, err := parsePacks(args)
			if err != nil {
				return err
			}
			cache := packs.Default()
			if sync {
				return syncPacks(env, cache, out)
			}
			s, err := cache.State()
			if err != nil {
				return err
			}
			if len(s.Packs) == 0 {
				_, err = fmt.Fprintln(out, "No packs synced yet: sign this machine in with `muda login`, then run `muda packs sync`.")
				return err
			}
			var b strings.Builder
			for _, e := range s.Packs {
				switch {
				case e.Status == packs.NotOwned:
					fmt.Fprintf(&b, "%s: not owned; buy it at %s\n", e.ID, account(e.Account))
				case e.Status == packs.Unavailable:
					fmt.Fprintf(&b, "%s: owned, not published yet\n", e.ID)
				case e.Tree == "":
					fmt.Fprintf(&b, "%s: owned, not cached; run `muda packs sync`\n", e.ID)
				default:
					p, err := content.ReadPack(os.DirFS(filepath.Join(cache.Dir, e.ID)))
					if err != nil {
						fmt.Fprintf(&b, "%s: owned, cached %s, unreadable (%v); run `muda packs sync`\n", e.ID, e.Tree, err)
						continue
					}
					fmt.Fprintf(&b, "%s: owned, cached %s; stacks %s\n", e.ID, e.Tree, strings.Join(p.Stacks, ", "))
				}
			}
			fmt.Fprintf(&b, "Cache: %s (synced %s)\n", cache.Dir, s.Synced.Format("2006-01-02 15:04 MST"))
			_, err = io.WriteString(out, b.String())
			return err
		},
	}
}

func syncPacks(env Env, cache packs.Cache, out io.Writer) error {
	token, err := auth.Token()
	if errors.Is(err, auth.ErrNotSignedIn) {
		return tools.Exitf(1, "this machine is not signed in to subaud.tools").WithHint("run `muda login`, then `muda packs sync`")
	}
	if err != nil {
		return err
	}
	rs, syncErr := cache.Sync(context.Background(), env.PacksAPI, env.HTTP, token)
	var b strings.Builder
	for _, r := range rs {
		switch r.Status {
		case "changed":
			fmt.Fprintf(&b, "%s: updated to %s\n", r.ID, r.Tree)
		case "unchanged":
			fmt.Fprintf(&b, "%s: up to date\n", r.ID)
		case "not_owned":
			fmt.Fprintf(&b, "%s: not owned; buy it at %s\n", r.ID, account(r.Account))
		case "unavailable":
			fmt.Fprintf(&b, "%s: owned, not published yet\n", r.ID)
		default:
			fmt.Fprintf(&b, "%s: %s\n", r.ID, r.Status)
		}
	}
	if _, err := io.WriteString(out, b.String()); err != nil {
		return err
	}
	return syncErr
}

func account(page string) string {
	if page == "" {
		return packs.AccountPage
	}
	return page
}

// library is muda's standard with the cached packs; its warnings go to errw.
func library(errw io.Writer) (content.Library, error) {
	lib, err := packs.Default().Library(version.Version().Number)
	for _, w := range lib.Warnings {
		_, _ = fmt.Fprintln(errw, "muda: "+w)
	}
	return lib, err
}

// noRecipes says why a recipe isn't here: no pack cached, or packs the
// account doesn't own.
func noRecipes(what string) error {
	s, err := packs.Default().State()
	if err != nil {
		return err
	}
	var notOwned []string
	page := packs.AccountPage
	for _, e := range s.Packs {
		if e.Status == packs.NotOwned {
			notOwned = append(notOwned, e.ID)
			page = account(e.Account)
		}
	}
	switch {
	case len(s.Packs) == 0:
		return usage(what + "; no packs synced yet: run `muda login`, then `muda packs sync`")
	case len(notOwned) == 0:
		return usage(what + "; no pack this account owns has it")
	}
	return usage(fmt.Sprintf("%s; it may be in a pack this account doesn't own (%s): see %s", what, strings.Join(notOwned, ", "), page))
}
