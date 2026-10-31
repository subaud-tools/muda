// Package channel is `muda channel`: the MCP server each agent session starts
// (Claude Code through the muda plugin, pi through channels.tools). The channel,
// its client and its loop are tools-common's pageserve; this package is muda's
// part of it: its tools, its instructions and how it reads serve's answers.
package channel

import (
	"encoding/json"

	"github.com/subaud-tools/tools-common/pageserve"
)

// Spec is muda's channel. Its scope is the session's folder (`muda channel
// --scope` names it): serve matches it against each project's groups.
func Spec(version string) pageserve.Spec {
	return pageserve.Spec{
		Tool: "muda", Version: version, Answers: "the user's decisions",
		Scope: func(cwd string) string { return cwd },
		Wake:  wake,
	}
}

func wake(json.RawMessage) (string, map[string]string, bool) { return "", nil, false }
