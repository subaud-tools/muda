package serve

// New project's folder scan. It lists GitHub repositories, and of a record it
// checks only whether .muda/muda.toml exists: it reads no finding, gate or window.

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/subaud-tools/muda/internal/store"
)

// ScannedRepo is one repository found. Name is its GitHub OWNER/NAME.
type ScannedRepo struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	HasRecord bool   `json:"has_record"`
}

// ScanResult is GET /api/scan: the folder looked in and what it holds.
type ScanResult struct {
	Folder string        `json:"folder"`
	Repos  []ScannedRepo `json:"repos"`
}

var ownerName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// githubName is the OWNER/NAME of a GitHub remote, "" for any other.
func githubName(remote string) string {
	path, ok := strings.CutPrefix(remote, "git@github.com:")
	if !ok {
		u, err := url.Parse(remote)
		if err != nil || u.Host != "github.com" || (u.Scheme != "https" && u.Scheme != "ssh") {
			return ""
		}
		path = strings.TrimPrefix(u.Path, "/")
	}
	path = strings.TrimSuffix(strings.TrimRight(path, "/"), ".git")
	if !ownerName.MatchString(path) {
		return ""
	}
	return path
}

// origin is a repository's origin remote, "" when it has none.
func origin(ctx context.Context, dir string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "git", "-C", dir, "config", "--get", "remote.origin.url") //nolint:gosec // G204: dir is a folder on this machine, not a command
	c.Env = gitEnv()
	out, err := c.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// scanFolder lists the GitHub repositories under folder to two levels. It goes
// past no repository and into no hidden folder.
func scanFolder(ctx context.Context, folder string, depth int) []ScannedRepo {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil
	}
	var out []ScannedRepo
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(folder, e.Name())
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err != nil {
			if depth > 1 {
				out = append(out, scanFolder(ctx, dir, depth-1)...)
			}
			continue
		}
		name := githubName(origin(ctx, dir))
		if name == "" {
			continue
		}
		_, err := os.Stat(filepath.Join(dir, ".muda", "muda.toml"))
		out = append(out, ScannedRepo{Path: dir, Name: name, HasRecord: err == nil})
	}
	return out
}

func (s *Server) scan(w http.ResponseWriter, r *http.Request) {
	folder := r.URL.Query().Get("folder")
	if rest, ok := strings.CutPrefix(folder, "~"); ok && (rest == "" || rest[0] == '/') {
		if home, err := os.UserHomeDir(); err == nil {
			folder = home + rest
		}
	}
	if st, err := os.Stat(folder); folder == "" || err != nil || !st.IsDir() {
		fail(w, store.Refused{Reason: "not a folder: " + r.URL.Query().Get("folder")})
		return
	}
	repos := scanFolder(r.Context(), folder, 2)
	sort.Slice(repos, func(i, j int) bool { return repos[i].Path < repos[j].Path })
	if repos == nil {
		repos = []ScannedRepo{}
	}
	writeJSON(w, http.StatusOK, ScanResult{Folder: folder, Repos: repos})
}

// gitEnv is this process's environment without the GIT_* variables a hook
// sets, so a git run with -C reads the repository at that path and no other.
func gitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	return env
}
