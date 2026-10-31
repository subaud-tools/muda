package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// muda's page wears muda's colours from the family palette
// (tools-ops sites/PALETTE.md: olive, #b9c06d dark and #6e7522 light) and the
// muda.tools mark as its favicon, not a colour of its own.
func TestPageWearsMudasPalette(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "web", "muda.css"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--tool-signal-light: #6e7522;", "--tool-signal-dark: #b9c06d;"} {
		if !strings.Contains(string(css), want) {
			t.Errorf("muda.css lacks %q", want)
		}
	}
	icon, err := os.ReadFile(filepath.Join("assets", "favicon.svg"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`fill="#14161d"`, `stroke="#b9c06d"`, `stroke-dasharray="3 5"`} {
		if !strings.Contains(string(icon), want) {
			t.Errorf("favicon.svg is not the muda.tools mark: lacks %s", want)
		}
	}
}
