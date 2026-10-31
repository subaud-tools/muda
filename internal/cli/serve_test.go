package cli

import (
	"bytes"
	"testing"

	"github.com/subaud-tools/muda/internal/channel"
	"github.com/subaud-tools/muda/internal/serve"
	tools "github.com/subaud-tools/tools-common"
)

// muda's page commands are the shared ones, each answering --help, alongside
// the skills command muda keeps as its own.
func TestPageCommandsAreRegistered(t *testing.T) {
	app := tools.New(tools.Config{Name: "muda", Domain: "muda.tools"})
	for _, c := range PageCommands(nil) {
		app.Register(c)
	}
	for _, name := range []string{"serve", "channel", "watch", "report"} {
		var out, errw bytes.Buffer
		if code := app.Dispatch([]string{name, "--help"}, &out, &errw); code != 0 {
			t.Errorf("%s --help: exit %d: %s", name, code, errw.String())
		}
	}
	var out, errw bytes.Buffer
	if code := app.Dispatch([]string{"channel", "--help"}, &out, &errw); code != 0 || !bytes.Contains(out.Bytes(), []byte("--scope")) {
		t.Errorf("channel --help names no --scope: %s", out.String())
	}
}

// --scope names the project's root: presence and wait carry the session's
// folder whatever folder that is, with no gate on the loop.
func TestChannelScopeNamesTheFolder(t *testing.T) {
	spec := channel.Spec("v")
	if spec.Scope == nil || spec.Scope("/work/a") != "/work/a" {
		t.Fatalf("Spec.Scope does not name the folder")
	}
	if spec.Root != nil {
		t.Fatal("Spec.Root would gate the loop on a project")
	}
	if serve.Live().Tool != "muda" {
		t.Fatalf("live entry for %q", serve.Live().Tool)
	}
}
