// Package packs keeps the recipe packs the account owns in muda's cache and
// brings them up to date from the access service.
package packs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/subaud-tools/muda"
	"github.com/subaud-tools/muda/internal/content"
	"github.com/subaud-tools/muda/internal/skills"
	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/auth"
)

// AccountPage is where a buyer sees and buys packs.
const AccountPage = "https://subaud.tools/account/"

// Status is what the last sync said of a pack.
type Status string

const (
	Owned       Status = "owned"       // the account owns it; Tree is the cached copy's
	NotOwned    Status = "not_owned"   // no grant covers it; nothing is cached
	Unavailable Status = "unavailable" // owned, but not in the pack repo yet
)

// Entry is one pack in the cache's state.
type Entry struct {
	ID      string `json:"id"`
	Status  Status `json:"status"`
	Tree    string `json:"tree,omitempty"`
	Account string `json:"account,omitempty"`
}

// State is the last sync's answer, kept beside the cached packs.
type State struct {
	Synced time.Time `json:"synced"`
	Packs  []Entry   `json:"packs"`
}

// Result is one pack's line of a sync: the service's status for it, or
// "refused" when a changed pack's archive was not a valid pack.
type Result struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Tree    string `json:"tree,omitempty"`
	Account string `json:"account,omitempty"`
	Archive string `json:"archive,omitempty"`
}

// Cache is muda's pack cache: one folder per pack, named by its id, and
// packs.json.
type Cache struct{ Dir string }

// Default is the cache under muda's XDG data dir.
func Default() Cache { return Cache{Dir: filepath.Join(tools.DataDir("muda"), "packs")} }

const stateFile = "packs.json"

var packID = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// State reads the last sync's answer; before the first sync it is empty.
func (c Cache) State() (State, error) {
	var s State
	b, err := os.ReadFile(filepath.Join(c.Dir, stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s: %w", filepath.Join(c.Dir, stateFile), err)
	}
	return s, nil
}

func (c Cache) writeState(s State) error {
	if err := tools.EnsureDir(c.Dir); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return tools.WriteFileAtomic(filepath.Join(c.Dir, stateFile), append(b, '\n'), 0o600)
}

// Library is muda's own content merged with each owned pack in the cache.
func (c Cache) Library(version string) (content.Library, error) {
	s, err := c.State()
	if err != nil {
		return content.Library{}, err
	}
	var fss []fs.FS
	for _, e := range s.Packs {
		if e.cached() {
			fss = append(fss, os.DirFS(filepath.Join(c.Dir, e.ID)))
		}
	}
	return content.Load(muda.Content, fss, version)
}

// cached reports whether the entry is an owned pack with a copy in the cache.
func (e Entry) cached() bool {
	return e.Status == Owned && e.Tree != "" && packID.MatchString(e.ID)
}

// Skills is the skill of each owned cached pack that has one.
func (c Cache) Skills() ([]skills.Pack, error) {
	s, err := c.State()
	if err != nil {
		return nil, err
	}
	var out []skills.Pack
	for _, e := range s.Packs {
		if !e.cached() {
			continue
		}
		dir := filepath.Join(c.Dir, e.ID, "skill")
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		out = append(out, skills.Pack{ID: e.ID, Skill: os.DirFS(dir)})
	}
	return out, nil
}

// Sync sends the tree hash of each cached pack to the access service at
// api, with the machine's token, and replaces each pack that changed. A pack
// the account no longer owns leaves the cache. A changed pack whose archive
// is not a valid pack is refused and its cached copy kept.
func (c Cache) Sync(ctx context.Context, api string, client *http.Client, token string) ([]Result, error) {
	if api == "" {
		api = auth.DefaultAPI
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	old, err := c.State()
	if err != nil {
		return nil, err
	}
	// A pack whose folder is gone sends no hash, so it comes back whole.
	have := map[string]string{}
	for _, e := range old.Packs {
		if _, err := os.Stat(filepath.Join(c.Dir, e.ID, "pack.toml")); err == nil && e.Status == Owned && e.Tree != "" && packID.MatchString(e.ID) {
			have[e.ID] = e.Tree
		}
	}
	body, err := json.Marshal(map[string]any{"have": have})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(api, "/")+"/packs/sync", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching the access service: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("reading the access service's answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &e)
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("the access service answered %d %s: sign this machine in with `muda login --wait`", resp.StatusCode, e.Error)
		}
		return nil, fmt.Errorf("the access service answered %d %s", resp.StatusCode, e.Error)
	}
	var answer struct {
		Packs []Result `json:"packs"`
	}
	if err := json.Unmarshal(b, &answer); err != nil {
		return nil, fmt.Errorf("reading the access service's answer: %w", err)
	}
	if err := tools.EnsureDir(c.Dir); err != nil {
		return nil, err
	}
	next := State{Synced: time.Now().UTC()}
	var errs []error
	for i, r := range answer.Packs {
		answer.Packs[i].Archive = ""
		if !packID.MatchString(r.ID) {
			errs = append(errs, fmt.Errorf("the access service named an invalid pack %q", r.ID))
			continue
		}
		e := Entry{ID: r.ID, Status: Status(r.Status), Tree: have[r.ID], Account: r.Account}
		switch r.Status {
		case "changed":
			e.Status = Owned
			if err := c.replace(r); err != nil {
				errs = append(errs, fmt.Errorf("pack %s: %w", r.ID, err))
				answer.Packs[i].Status = "refused"
			} else {
				e.Tree = r.Tree
			}
		case "unchanged":
			e.Status = Owned
		case string(Unavailable):
		case string(NotOwned):
			e.Tree = ""
			if err := os.RemoveAll(filepath.Join(c.Dir, r.ID)); err != nil {
				errs = append(errs, err)
			}
		default:
			errs = append(errs, fmt.Errorf("pack %s: unknown status %q", r.ID, r.Status))
			continue
		}
		next.Packs = append(next.Packs, e)
	}
	if err := c.writeState(next); err != nil {
		errs = append(errs, err)
	}
	return answer.Packs, errors.Join(errs...)
}

// maxPack bounds an unpacked pack; packs are Markdown, far below it.
const maxPack = 32 << 20

// replace unpacks a changed pack beside the cache, checks it reads as the
// pack it claims to be, and only then puts it in place of the cached copy.
func (c Cache) replace(r Result) error {
	tmp, err := os.MkdirTemp(c.Dir, ".sync-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	raw, err := base64.StdEncoding.DecodeString(r.Archive)
	if err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	if err := untar(raw, tmp); err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	p, err := content.ReadPack(os.DirFS(tmp))
	if err != nil {
		return err
	}
	if p.ID != r.ID {
		return fmt.Errorf("archive holds pack %s", p.ID)
	}
	dst := filepath.Join(c.Dir, r.ID)
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// untar writes a gzip tar's regular files under dir. A path that leaves dir,
// or an entry that is not a file or directory, refuses the whole archive.
func untar(raw []byte, dir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(h.Name)
		if !fs.ValidPath(name) || name == "." {
			return fmt.Errorf("path outside the pack: %s", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
		default:
			return fmt.Errorf("not a regular file: %s", h.Name)
		}
		if total += h.Size; total > maxPack {
			return fmt.Errorf("larger than %d bytes", maxPack)
		}
		dst := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		b, err := io.ReadAll(io.LimitReader(tr, h.Size))
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o600); err != nil {
			return err
		}
	}
}
