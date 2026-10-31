package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/subaud-tools/tools-common/pageserve"
)

func pretty(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

// call runs one tool: open, post and reply go to serve with the session added;
// status is serve's answer as it stands.
func call(ctx context.Context, ch *pageserve.Channel, name string, args json.RawMessage) (string, error) {
	text, err := run(ctx, ch, name, args)
	if err != nil {
		return "", errors.New(strings.Join(strings.Fields(err.Error()), " "))
	}
	return text, nil
}

func run(ctx context.Context, ch *pageserve.Channel, name string, args json.RawMessage) (string, error) {
	switch name {
	case "muda_open":
		return open(ctx, ch, args)
	case "muda_post":
		return forward(ctx, ch, "/api/agent/post", args)
	case "muda_reply":
		return forward(ctx, ch, "/api/agent/reply", args)
	case "muda_status":
		return status(ctx, ch)
	}
	return "", fmt.Errorf("unknown tool %q", name)
}

// withSession adds the session to a tool's arguments.
func withSession(ch *pageserve.Channel, args json.RawMessage) (map[string]any, error) {
	if ch.ID.Session == "" {
		return nil, errors.New("this muda channel has no session id (not running under Claude Code or pi), so the user's decisions could not reach it")
	}
	body := map[string]any{}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &body); err != nil {
			return nil, fmt.Errorf("bad arguments: %w", err)
		}
	}
	body["session"], body["label"] = ch.ID.Session, ch.ID.Label
	return body, nil
}

func forward(ctx context.Context, ch *pageserve.Channel, path string, args json.RawMessage) (string, error) {
	body, err := withSession(ch, args)
	if err != nil {
		return "", err
	}
	var out map[string]any
	if _, err := ch.Client.Do(ctx, http.MethodPost, path, body, &out); err != nil {
		return "", plain(err)
	}
	return pretty(out), nil
}

// plain is serve's refusal in its own words.
func plain(err error) error {
	var se *pageserve.StatusError
	if errors.As(err, &se) && se.Code < http.StatusInternalServerError {
		return errors.New(se.Msg)
	}
	return err
}

// open makes this session the project's owner and shows its page.
func open(ctx context.Context, ch *pageserve.Channel, args json.RawMessage) (string, error) {
	body, err := withSession(ch, args)
	if err != nil {
		return "", err
	}
	var out map[string]any
	if _, err := ch.Client.Do(ctx, http.MethodPost, "/api/agent/open", body, &out); err != nil {
		return "", plain(err)
	}
	if out["tab"] == "opened" {
		if url, _ := out["url"].(string); url != "" {
			if err := ch.Open(url); err != nil {
				out["open_error"] = err.Error()
			}
		}
	}
	return pretty(out), nil
}

func status(ctx context.Context, ch *pageserve.Channel) (string, error) {
	var out map[string]any
	if _, err := ch.Client.Do(ctx, http.MethodGet, "/api/agent/status", nil, &out); err != nil {
		return "", plain(err)
	}
	if ch.ServeOnly {
		out["events"] = "This is a Claude Code session: the user's decisions reach it through `muda watch`, which the muda plugin starts, not through this process."
	}
	return pretty(out), nil
}
