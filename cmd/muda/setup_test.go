package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/subaud-tools/muda"
	"github.com/subaud-tools/tools-common/setupbundle"
)

func TestSetupBundleStockOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var previous []byte
	for i := range 2 {
		dest := filepath.Join(t.TempDir(), "setup.tar.gz")
		var output bytes.Buffer
		if code := NewApp().Dispatch([]string{"setup-bundle", "--version", "0.9.0-rc.1", "--out", dest}, &output, &output); code != 0 {
			t.Fatalf("exit %d: %s", code, &output)
		}
		data, err := os.ReadFile(dest)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && !bytes.Equal(previous, data) {
			t.Fatal("nondeterministic archive")
		}
		previous = data
		b, err := setupbundle.Read(data)
		if err != nil {
			t.Fatal(err)
		}
		if b.Tool != "muda" || b.Version != "0.9.0-rc.1" || len(b.Files) != 6 {
			t.Fatalf("wrong identity/inventory: %+v", b)
		}
		for p, body := range b.Files {
			if !strings.HasSuffix(p, "/SKILL.md") {
				t.Fatal(p)
			}
			want, err := fs.ReadFile(muda.Content, "skills/"+p)
			if err != nil || !bytes.Equal(body, want) {
				t.Fatalf("non-stock file %s: %v", p, err)
			}
		}
		var manifest struct {
			Kempt    struct{ Spec int }
			Packages map[string]struct {
				Files []struct {
					From, To, Mode string
					Markers        map[string]string `toml:"adopt-markers"`
				} `toml:"managed-file"`
				Merges []struct {
					File  string
					Merge struct {
						ExtraKnownMarketplaces map[string]struct {
							Source     struct{ Source, Repo string }
							AutoUpdate bool
						}
						EnabledPlugins map[string]bool
						ChannelServers map[string]struct {
							Command string
							Args    []string
						}
					}
				} `toml:"json-merge"`
			}
		}
		md, err := toml.Decode(string(b.Manifest), &manifest)
		if err != nil || len(md.Undecoded()) != 0 {
			t.Fatalf("unexpected manifest: %v %v", err, md.Undecoded())
		}
		pkg := manifest.Packages["integration"]
		if manifest.Kempt.Spec != 2 || len(manifest.Packages) != 1 || len(pkg.Files) != 12 || len(pkg.Merges) != 2 {
			t.Fatalf("wrong scope: %+v", manifest)
		}
		claude, pi := pkg.Merges[0], pkg.Merges[1]
		marketplace := claude.Merge.ExtraKnownMarketplaces["subaud-tools"]
		channel := pi.Merge.ChannelServers["muda"]
		if claude.File != "~/.claude/settings.json" || !claude.Merge.EnabledPlugins["muda@subaud-tools"] || marketplace.Source.Source != "github" || marketplace.Source.Repo != "subaud-tools/plugins" || !marketplace.AutoUpdate || pi.File != "~/.pi/agent/channels.json" || channel.Command != "muda" || strings.Join(channel.Args, " ") != "channel" {
			t.Fatal("lost existing registrations")
		}
		for _, f := range pkg.Files {
			name := strings.TrimSuffix(f.From, "/SKILL.md")
			if b.Files[f.From] == nil || f.Mode != "0644" || len(f.Markers) != 1 || f.Markers["muda-owned"] != name {
				t.Fatalf("wrong ownership: %+v", f)
			}
			if f.To != "~/.claude/skills/"+f.From && f.To != "~/.pi/agent/skills/"+f.From {
				t.Fatalf("new harness: %s", f.To)
			}
		}
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("generation touched HOME: %v %v", entries, err)
	}
}
