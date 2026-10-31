// Package serve is `muda serve`: the page's API over the store, served by
// pageserve's one local server per machine.
package serve

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"

	"github.com/subaud-tools/muda/internal/store"
	"github.com/subaud-tools/tools-common/localweb"
	"github.com/subaud-tools/tools-common/localweb/page"
	"github.com/subaud-tools/tools-common/pageserve"
)

//go:embed assets
var assets embed.FS

// Live is where muda serve's entry is: StateDir/live/serve.json.
func Live() pageserve.Live { return pageserve.LiveFor("muda") }

// Server is muda's API over the store.
type Server struct {
	// PageURL is the page's address with its token, for agents.
	PageURL string

	st     *store.Store
	events *pageserve.Events
	tabs   *localweb.Tabs
	ag     agents
}

// New returns a Server over st.
func New(st *store.Store) *Server { return &Server{st: st, events: pageserve.NewEvents()} }

// Handler is muda's own /api/ routes; pageserve adds events, poll and stop.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/agent/presence", s.agentPresence)
	mux.HandleFunc("GET /api/agent/wait", s.agentWait)
	mux.HandleFunc("POST /api/agent/report", s.agentReport)
	mux.HandleFunc("POST /api/agent/open", s.agentOpen)
	mux.HandleFunc("POST /api/agent/post", s.agentPost)
	mux.HandleFunc("POST /api/agent/reply", s.agentReply)
	mux.HandleFunc("GET /api/agent/status", s.agentStatus)
	return mux
}

// Options configures Run.
type Options struct {
	Port    int // 0: remembered, else free
	Version string
	Log     io.Writer // one line per start and stop
}

// Run serves st until ctx ends or /api/stop. Deliveries the last serve left in
// flight go back to their scopes' owners once it holds the machine's one serve.
func Run(ctx context.Context, st *store.Store, o Options) error {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		return err
	}
	s := New(st)
	return pageserve.Run(ctx, pageserve.Options{
		Live: Live(), Assets: page.With(sub), API: s.Handler(), Events: s.events, Port: o.Port, Version: o.Version, Log: o.Log,
		Tabs: func(t *localweb.Tabs) { s.tabs = t },
		OnStart: func(ctx context.Context, url string) {
			s.PageURL = url
			// After pageserve's check that no other serve runs: this one's
			// deliveries are not the last serve's to end.
			if _, err := st.Threads.Interrupt(ctx); err != nil && o.Log != nil {
				_, _ = fmt.Fprintf(o.Log, "muda serve: deliveries left in flight were not sent again: %v\n", err)
			}
		},
	})
}
