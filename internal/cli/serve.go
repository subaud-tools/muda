package cli

import (
	"context"
	"io"

	"github.com/subaud-tools/muda/internal/channel"
	"github.com/subaud-tools/muda/internal/serve"
	"github.com/subaud-tools/muda/internal/version"
	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/pageserve"
)

// shell is muda's serve, channel, watch and report commands: pageserve's, with
// muda's channel and serve. muda's own skills command stays as it is.
func shell() pageserve.Commands {
	return pageserve.Commands{
		Spec:    channel.Spec(version.Version().Number),
		Process: pageserve.Process{Live: serve.Live(), Args: []string{"serve", "--foreground", "--no-open"}},
		Serve:   serveForeground,
	}
}

func serveForeground(ctx context.Context, port int, errw io.Writer) error {
	return serve.Run(ctx, serve.Options{Port: port, Version: version.Version().Number, Log: errw})
}

// PageCommands are muda's commands for its page and an agent session's channel.
func PageCommands(stdin io.Reader) []tools.Command {
	s := shell()
	return []tools.Command{
		s.ServeCommand(pageserve.Help{
			Synopsis: "[--no-open] [--port N] [--foreground] [--stop]",
			Summary:  "open muda's page (starts the local server if needed)",
			Long:     "Starts the page's server in the background (one per machine) and opens the page.\nWhen it is already running, opens the running one. Needs no network.",
		}),
		s.ChannelCommand(stdin, pageserve.Help{
			Synopsis: "[--scope DIR]",
			Summary:  "MCP server for an agent session: open, post, reply, status, and Send as events",
			Long: "Runs muda's MCP server on stdin/stdout for an agent session (Claude Code or pi starts it).\n" +
				"It starts muda serve if needed. --scope names the project's root when it is not the\n" +
				"session's folder. Under pi it also registers the session and delivers the user's\n" +
				"decisions as events; under Claude Code `muda watch` does that.\n" +
				"stdout carries MCP frames only; logs go to stderr.",
		}),
		s.WatchCommand(pageserve.Help{
			Summary: "print the user's Sends as JSON lines: the Claude Code plugin's feed",
			Long: "Registers the session with muda serve and prints each of the user's Sends for it as one\n" +
				"JSON line on stdout. The muda plugin's mod runs it for a Claude Code session.\n" +
				"Logs go to stderr. Runs until signalled.",
		}),
		s.ReportCommand(),
	}
}
