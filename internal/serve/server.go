// Package serve is `muda serve`: the page's API, served by pageserve's one
// local server per machine.
package serve

import (
	"context"
	"embed"
	"io"
	"io/fs"

	"github.com/subaud-tools/tools-common/localweb/page"
	"github.com/subaud-tools/tools-common/pageserve"
)

//go:embed assets
var assets embed.FS

// Live is where muda serve's entry is: StateDir/live/serve.json.
func Live() pageserve.Live { return pageserve.LiveFor("muda") }

// Options configures Run.
type Options struct {
	Port    int // 0: remembered, else free
	Version string
	Log     io.Writer // one line per start and stop
}

// Run serves until ctx ends or /api/stop.
func Run(ctx context.Context, o Options) error {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		return err
	}
	return pageserve.Run(ctx, pageserve.Options{
		Live: Live(), Assets: page.With(sub), Port: o.Port, Version: o.Version, Log: o.Log,
	})
}
