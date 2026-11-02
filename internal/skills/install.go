package skills

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	commonskills "github.com/subaud-tools/tools-common/skills"
)

// UserTargets maps Muda's existing agent names to the shared user destinations.
func UserTargets() map[string]string {
	targets := commonskills.UserTargets()
	return map[string]string{"claude": targets["claude-code"], "pi": targets["pi"]}
}

// Target validates only the active scope's arguments; it never consults cwd or
// the process home. Project callers must supply an existing Git root.
func Target(agent, scope, home, projectRoot string) (string, string, error) {
	target, ok := UserTargets()[agent]
	if !ok {
		return "", "", fmt.Errorf("unsupported agent: %s", agent)
	}
	rel := strings.TrimPrefix(target, "~/")
	base := home
	switch scope {
	case "user":
	case "project":
		base = projectRoot
		if agent == "pi" {
			rel = ".pi/skills"
		}
	default:
		return "", "", fmt.Errorf("unsupported scope: %s", scope)
	}
	if base == "" || !filepath.IsAbs(base) {
		return "", "", fmt.Errorf("active %s root must be absolute", scope)
	}
	st, e := os.Lstat(base)
	if e != nil {
		return "", "", e
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("unsafe root: %s", base)
	}
	if scope == "project" {
		st, e = os.Lstat(filepath.Join(base, ".git"))
		if e != nil || st.Mode()&os.ModeSymlink != 0 || (!st.IsDir() && !st.Mode().IsRegular()) {
			return "", "", fmt.Errorf("not a Git root: %s", base)
		}
	}
	return filepath.Clean(base), rel, nil
}

// A pack skill's text is the pack's, so it can carry no muda-owned line: the
// marker file beside it says which pack's copy the directory holds.
const packMarker = ".muda-pack"

// Pack is the skill a pack in the cache carries: Skill holds its SKILL.md and
// supporting files at the top.
type Pack struct {
	ID    string
	Skill fs.FS
}

// source is one file to install. pack is empty for muda's own skills.
type source struct {
	rel, name, pack string
	data            []byte
}

// mark names the file that says who owns the skill's directory.
func (s source) mark() string {
	if s.pack != "" {
		return packMarker
	}
	return "SKILL.md"
}

// marks reports whether the mark file's text b says the directory is this
// source's to write.
func (s source) marks(b []byte) bool {
	if s.pack != "" {
		return string(b) == s.pack+"\n"
	}
	return owned(b, s.name)
}

func sources(fsys fs.FS) ([]source, error) {
	var files []source
	entries, e := fs.ReadDir(fsys, "skills")
	if e != nil {
		return nil, e
	}
	for _, d := range entries {
		if d.Name() == "README.md" && !d.IsDir() {
			continue
		}
		if !d.IsDir() || !knownName(d.Name()) || d.Type()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("invalid source skill: %s", d.Name())
		}
		n := d.Name()
		dir := "skills/" + n
		haveSkill := false
		e = fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !fs.ValidPath(p) || d.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("unsafe source: %s", p)
			}
			if d.IsDir() {
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("non-regular source: %s", p)
			}
			b, e := fs.ReadFile(fsys, p)
			if e != nil {
				return e
			}
			if !owned(b, n) {
				return fmt.Errorf("unowned source: %s", p)
			}
			if p == dir+"/SKILL.md" {
				haveSkill = true
			}
			files = append(files, source{rel: strings.TrimPrefix(p, "skills/"), name: n, data: b})
			return nil
		})
		if e != nil {
			return nil, e
		}
		if !haveSkill {
			return nil, fmt.Errorf("missing source: %s/SKILL.md", dir)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no bundled skills")
	}
	return files, nil
}

var packSkillName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// packSources reads each pack's skill under the name its front matter gives,
// with the marker first. Only the name is read: the text is the pack's, and
// muda's lint is not for it.
func packSources(packs []Pack) ([]source, error) {
	var files []source
	takenBy := map[string]string{}
	for _, p := range packs {
		b, e := fs.ReadFile(p.Skill, "SKILL.md")
		if e != nil {
			return nil, fmt.Errorf("pack %s: %w", p.ID, e)
		}
		m, _, e := frontmatter(b)
		if e != nil {
			return nil, fmt.Errorf("pack %s: skill/SKILL.md: %w", p.ID, e)
		}
		n := m.Name
		switch {
		case !packSkillName.MatchString(n):
			return nil, fmt.Errorf("pack %s: skill name %q is not a safe directory name; refused", p.ID, n)
		case knownName(n):
			return nil, fmt.Errorf("pack %s: skill name %q is one of muda's own skills; refused", p.ID, n)
		case takenBy[n] != "":
			return nil, fmt.Errorf("pack %s: skill name %q is also pack %s's; refused", p.ID, n, takenBy[n])
		}
		takenBy[n] = p.ID
		files = append(files, source{rel: n + "/" + packMarker, name: n, pack: p.ID, data: []byte(p.ID + "\n")})
		e = fs.WalkDir(p.Skill, ".", func(f string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("pack %s: unsafe source: %s", p.ID, f)
			}
			if d.IsDir() {
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() || f == packMarker {
				return fmt.Errorf("pack %s: refused source: %s", p.ID, f)
			}
			data, e := fs.ReadFile(p.Skill, f)
			if e != nil {
				return e
			}
			files = append(files, source{rel: n + "/" + f, name: n, pack: p.ID, data: data})
			return nil
		})
		if e != nil {
			return nil, e
		}
	}
	return files, nil
}

// sortSources gives a deterministic order with each skill's mark first:
// supporting files are only written beneath an already-owned directory. A
// pack's marker comes before its SKILL.md, which is not its mark.
func sortSources(files []source) {
	rank := func(s source) int {
		switch s.rel {
		case s.name + "/" + s.mark():
			return 0
		case s.name + "/SKILL.md":
			return 1
		}
		return 2
	}
	sort.Slice(files, func(i, j int) bool {
		a, b := files[i], files[j]
		if a.name != b.name {
			return a.name+"/" < b.name+"/" // same order as the paths
		}
		if am, bm := rank(a), rank(b); am != bm {
			return am < bm
		}
		return a.rel < b.rel
	})
}

// safePath rejects symlinks even when their target stays inside the root.
func safePath(r *os.Root, p string) error {
	parts := strings.Split(p, "/")
	for i := range parts {
		q := strings.Join(parts[:i+1], "/")
		st, e := r.Lstat(q)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !st.IsDir()) {
			return fmt.Errorf("unsafe destination: %s", q)
		}
	}
	return nil
}

// destination allows p to be replaced. Inside a pack's copy only the marker's
// text is checked: the marker owns the directory, and the other files hold no
// mark of their own.
func destination(r *os.Root, p string, f source) error {
	if e := safePath(r, p); e != nil {
		return e
	}
	st, e := r.Lstat(p)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("foreign destination: %s", p)
	}
	if f.pack != "" && path.Base(p) != packMarker {
		return nil
	}
	b, e := r.ReadFile(p)
	if e != nil {
		return e
	}
	if !f.marks(b) {
		return fmt.Errorf("foreign destination: %s", p)
	}
	return nil
}

// skillsRoot resolves the agent's skills root once: the root itself may be a
// symlink (e.g. into a dotfiles folder), but must resolve to a
// directory. A missing root is created beneath base without following any
// symlink. Symlinks beneath the resolved root stay refused by safePath.
func skillsRoot(base, rel string) (string, error) {
	p := filepath.Join(base, filepath.FromSlash(rel))
	if _, e := os.Lstat(p); os.IsNotExist(e) {
		r, e := os.OpenRoot(base)
		if e != nil {
			return "", e
		}
		e = safePath(r, rel)
		if e == nil {
			e = r.MkdirAll(rel, 0755)
		}
		if ce := r.Close(); e == nil {
			e = ce
		}
		if e != nil {
			return "", fmt.Errorf("%s: %w", p, e)
		}
	} else if e != nil {
		return "", e
	}
	resolved, e := filepath.EvalSymlinks(p)
	if e != nil {
		return "", e
	}
	st, e := os.Lstat(resolved)
	if e != nil {
		return "", e
	}
	if !st.IsDir() {
		return "", fmt.Errorf("skills root is not a directory: %s", p)
	}
	return resolved, nil
}

// Install is an intentional write-boundary exception to the normal read-only
// CLI. It installs only owned files, preserving unrelated supporting files.
// All static destinations are validated before mkdir/write; each file is then
// atomically renamed through a pinned os.Root. It is not a batch transaction.
// Each pack's skill installs beside muda's, under its own name; a pack that
// cannot be installed refuses the whole install before anything is written.
func Install(fsys fs.FS, agent, scope, home, projectRoot string, packs ...Pack) ([]string, error) {
	base, rel, e := Target(agent, scope, home, projectRoot)
	if e != nil {
		return nil, e
	}
	files, e := sources(fsys)
	if e != nil {
		return nil, e
	}
	fromPacks, e := packSources(packs)
	if e != nil {
		return nil, e
	}
	return write(base, rel, append(files, fromPacks...))
}

// Refresh rewrites the copies of pack skills already installed for this agent
// and scope whose files differ from the pack's. It installs nothing new: a
// pack with no copy here, or a skill of that name muda did not install, is
// left alone. One pack's failure doesn't stop the others'.
func Refresh(agent, scope, home, projectRoot string, packs []Pack) ([]string, error) {
	base, rel, e := Target(agent, scope, home, projectRoot)
	if e != nil {
		return nil, e
	}
	dir := filepath.Join(base, filepath.FromSlash(rel))
	var written []string
	var errs []error
	for _, p := range packs {
		files, e := packSources([]Pack{p})
		if e != nil {
			errs = append(errs, e)
			continue
		}
		installed := func(f source) ([]byte, error) { return os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.rel))) }
		if b, e := installed(files[0]); e != nil || !files[0].marks(b) {
			continue
		}
		changed := slices.ContainsFunc(files, func(f source) bool {
			b, e := installed(f)
			return e != nil || !bytes.Equal(b, f.data)
		})
		if !changed {
			continue
		}
		paths, e := write(base, rel, files)
		written = append(written, paths...)
		if e != nil {
			errs = append(errs, e)
		}
	}
	return written, errors.Join(errs...)
}

func write(base, rel string, files []source) ([]string, error) {
	sortSources(files)
	shown := filepath.Join(base, filepath.FromSlash(rel))
	resolved, e := skillsRoot(base, rel)
	if e != nil {
		return nil, e
	}
	r, e := os.OpenRoot(resolved)
	if e != nil {
		return nil, e
	}
	defer func() { _ = r.Close() }()
	opened, e := r.Stat(".")
	current, ce := os.Lstat(resolved)
	named, ne := os.Stat(shown)
	if e != nil || ce != nil || ne != nil || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, current) || !os.SameFile(opened, named) {
		return nil, fmt.Errorf("root boundary changed: %s", shown)
	}
	fail := func(p string, e error) error { return fmt.Errorf("%s: %w", filepath.Join(shown, p), e) }
	for _, f := range files {
		if e = destination(r, f.rel, f); e != nil {
			return nil, fail(f.rel, e)
		}
		if e = safePath(r, f.name); e != nil {
			return nil, fail(f.name, e)
		}
		if _, e = r.Lstat(f.name); e == nil {
			b, err := r.ReadFile(path.Join(f.name, f.mark()))
			if err != nil || !f.marks(b) {
				return nil, fail(f.name, fmt.Errorf("foreign skill directory"))
			}
		} else if !os.IsNotExist(e) {
			return nil, fail(f.name, e)
		}
	}
	var written []string
	for _, f := range files {
		if e = safePath(r, f.rel); e != nil {
			return written, e
		}
		if e = r.MkdirAll(path.Dir(f.rel), 0755); e != nil {
			return written, e
		}
		// Pin the own-directory boundary too; root confinement alone would permit
		// an internal symlink redirecting one skill into another.
		own := f.name
		before, e := r.Lstat(own)
		if e != nil {
			return written, e
		}
		ownRoot, e := r.OpenRoot(own)
		if e != nil {
			return written, e
		}
		sub := strings.TrimPrefix(f.rel, f.name+"/")
		e = writeOwned(r, ownRoot, own, sub, f, before)
		closeErr := ownRoot.Close()
		if e != nil {
			return written, e
		}
		if closeErr != nil {
			return written, closeErr
		}
		if f.pack == "" || sub != packMarker {
			written = append(written, filepath.Join(shown, filepath.FromSlash(f.rel)))
		}
	}
	return written, nil
}
func writeOwned(parent, r *os.Root, own, sub string, f source, before os.FileInfo) error {
	boundary := func() error {
		now, e := parent.Lstat(own)
		opened, oe := r.Stat(".")
		if e != nil || oe != nil || now.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, now) || !os.SameFile(before, opened) {
			return fmt.Errorf("skill boundary changed: %s", own)
		}
		if sub != f.mark() {
			if e := safePath(r, f.mark()); e != nil {
				return e
			}
			b, e := r.ReadFile(f.mark())
			if e != nil || !f.marks(b) {
				return fmt.Errorf("skill ownership changed: %s", own)
			}
		}
		return nil
	}
	if e := boundary(); e != nil {
		return e
	}
	if e := destination(r, sub, f); e != nil {
		return e
	}
	// Exclusive creation, confined under the own skill root.
	var tmp string
	var w *os.File
	var e error
	for i := 0; i < 100; i++ {
		tmp = path.Join(path.Dir(sub), fmt.Sprintf(".muda-install-%d-%d", os.Getpid(), i))
		w, e = r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if !os.IsExist(e) {
			break
		}
	}
	if e != nil {
		return e
	}
	defer func() { _ = r.Remove(tmp) }()
	_, e = w.Write(f.data)
	if e == nil {
		// Created 0600 for exclusivity; installed files are ordinary 0644 files.
		e = w.Chmod(0o644)
	}
	if e == nil {
		e = w.Sync()
	}
	ce := w.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = boundary(); e != nil {
		return e
	}
	if e = destination(r, sub, f); e != nil {
		return e
	}
	return r.Rename(tmp, sub)
}
