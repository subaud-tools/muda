package content

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Pack is one recipe pack: a small standard of its own. Its folder holds
// pack.toml (id, the muda range it was checked against, its stacks), an
// optional standard.md whose ## Catalog is in muda's shape, recipes/*.md in
// muda's recipe format, and an optional skill/SKILL.md.
type Pack struct {
	ID         string
	Muda       string
	Stacks     []string
	Classes    []string
	Candidates []string
	Recipes    []Recipe
	FS         fs.FS
}

type manifest struct {
	ID     string   `toml:"id"`
	Muda   string   `toml:"muda"`
	Stacks []string `toml:"stacks"`
}

var packID = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ReadPack reads a pack folder. Malformed metadata is an error; the content
// check (CheckPack) judges what the files say.
func ReadPack(fsys fs.FS) (Pack, error) {
	m, err := readManifest(fsys)
	if err != nil {
		return Pack{}, fmt.Errorf("pack.toml: %w", err)
	}
	p := Pack{ID: m.ID, Muda: m.Muda, Stacks: m.Stacks, FS: fsys}
	if b, err := fs.ReadFile(fsys, "standard.md"); err == nil {
		text := string(b)
		if p.Classes, err = wastes(text); err != nil {
			return p, fmt.Errorf("pack %s: standard.md: %w", p.ID, err)
		}
		if _, ok := section(text, "Candidate wastes"); ok {
			if p.Candidates, err = candidates(text); err != nil {
				return p, fmt.Errorf("pack %s: standard.md: %w", p.ID, err)
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return p, fmt.Errorf("pack %s: %w", p.ID, err)
	}
	entries, err := fs.ReadDir(fsys, "recipes")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return p, fmt.Errorf("pack %s: %w", p.ID, err)
	}
	if p.Recipes, err = readRecipes(fsys, entries); err != nil {
		return p, fmt.Errorf("pack %s: %w", p.ID, err)
	}
	for i := range p.Recipes {
		p.Recipes[i].Pack = p.ID
	}
	return p, nil
}

func readManifest(fsys fs.FS) (manifest, error) {
	var m manifest
	b, err := fs.ReadFile(fsys, "pack.toml")
	if err != nil {
		return m, err
	}
	md, err := toml.Decode(string(b), &m)
	if err != nil {
		return m, err
	}
	if extra := md.Undecoded(); len(extra) > 0 {
		return m, fmt.Errorf("unknown key %s", extra[0])
	}
	if !packID.MatchString(m.ID) {
		return m, fmt.Errorf("invalid id %q", m.ID)
	}
	if _, err := parseRange(m.Muda); err != nil {
		return m, fmt.Errorf("muda: %w", err)
	}
	return m, nil
}

// Library is muda's own standard merged with the packs it holds: classes,
// candidates and stacks from all, recipes by pack.
type Library struct {
	Wastes     []string
	Candidates []string
	Stacks     []string
	Recipes    []Recipe
	Packs      []Pack
	Warnings   []string
}

// Load merges base (muda's embedded content) with each pack, in pack ID
// order. A pack whose muda range excludes version is skipped with a warning;
// a dev build loads every pack. A class or recipe ID that two of them claim
// refuses the library, naming both.
func Load(base fs.FS, packs []fs.FS, version string) (Library, error) {
	var lib Library
	b, err := fs.ReadFile(base, "standard/standard.md")
	if err != nil {
		return lib, err
	}
	if lib.Wastes, err = wastes(string(b)); err != nil {
		return lib, err
	}
	if lib.Candidates, err = candidates(string(b)); err != nil {
		return lib, err
	}
	classOwner, recipeOwner := map[string]string{}, map[string]string{}
	for _, w := range lib.Wastes {
		classOwner[w] = "muda"
	}
	if _, err := fs.Stat(base, "recipes"); err == nil {
		if lib.Stacks, err = Stacks(base); err != nil {
			return lib, err
		}
		if lib.Recipes, err = Recipes(base); err != nil {
			return lib, err
		}
		for _, r := range lib.Recipes {
			recipeOwner[r.ID] = "muda"
		}
	}
	var ps []Pack
	for _, f := range packs {
		p, err := ReadPack(f)
		if err != nil {
			return lib, err
		}
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })
	for _, p := range ps {
		if ok, _ := inRange(p.Muda, version); !ok {
			lib.Warnings = append(lib.Warnings, fmt.Sprintf("pack %s skipped: it was checked against muda %s, and this is muda %s", p.ID, p.Muda, version))
			continue
		}
		claim := func(owner map[string]string, kind, id string) error {
			if o, ok := owner[id]; ok {
				return fmt.Errorf("%s %s is claimed by both %s and pack %s", kind, id, o, p.ID)
			}
			owner[id] = "pack " + p.ID
			return nil
		}
		for _, c := range p.Classes {
			if err := claim(classOwner, "class", c); err != nil {
				return lib, err
			}
		}
		for _, r := range p.Recipes {
			if err := claim(recipeOwner, "recipe", r.ID); err != nil {
				return lib, err
			}
		}
		lib.Wastes = append(lib.Wastes, p.Classes...)
		for _, c := range p.Candidates {
			if !slices.Contains(lib.Candidates, c) {
				lib.Candidates = append(lib.Candidates, c)
			}
		}
		for _, s := range p.Stacks {
			if !slices.Contains(lib.Stacks, s) {
				lib.Stacks = append(lib.Stacks, s)
			}
		}
		lib.Recipes = append(lib.Recipes, p.Recipes...)
		lib.Packs = append(lib.Packs, p)
	}
	return lib, nil
}

// A muda range is comma-separated bounds, each an operator (>=, >, <=, <, =)
// and a version: ">=0.4.0" or ">=0.4.0, <0.5.0".
type bound struct {
	op string
	v  [3]int
}

func parseRange(s string) ([]bound, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("missing version range")
	}
	var bs []bound
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		var b bound
		for _, op := range []string{">=", "<=", ">", "<", "="} {
			if rest, ok := strings.CutPrefix(part, op); ok {
				b.op, part = op, strings.TrimSpace(rest)
				break
			}
		}
		v, ok := parseVersion(part)
		if b.op == "" || !ok {
			return nil, fmt.Errorf("invalid version range %q", s)
		}
		b.v = v
		bs = append(bs, b)
	}
	return bs, nil
}

func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// inRange reports whether version satisfies the range. A version that is not
// a release (dev) satisfies every range.
func inRange(rng, version string) (bool, error) {
	bs, err := parseRange(rng)
	if err != nil {
		return false, err
	}
	v, ok := parseVersion(version)
	if !ok {
		return true, nil
	}
	for _, b := range bs {
		c := slices.Compare(v[:], b.v[:])
		if !map[string]bool{">=": c >= 0, ">": c > 0, "<=": c <= 0, "<": c < 0, "=": c == 0}[b.op] {
			return false, nil
		}
	}
	return true, nil
}
