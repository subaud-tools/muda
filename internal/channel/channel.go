// Package channel is `muda channel`: the MCP server each agent session starts
// (Claude Code through the muda plugin, pi through channels.tools). The channel,
// its client and its loop are tools-common's pageserve; this package is muda's
// part of it: its tools, its instructions and how it reads serve's answers.
package channel

import (
	"encoding/json"
	"strconv"

	"github.com/subaud-tools/tools-common/channelmcp"
	"github.com/subaud-tools/tools-common/pageserve"
)

// Instructions are the standing guidance the agent gets with the channel.
const Instructions = `muda's page is where the user reviews a project: a named set of groups (repositories or accounts), each job's stages, and the cards you post for them. Every review decision is made on the page, not in chat.
- muda_open creates a project by title with its groups, or opens one, and makes this session its owner; any later session may open it and take over, bringing the user's unanswered messages with it.
- muda_post posts or revises jobs, stages, groups and cards in one transaction. Give every card a title. Set kind "finding" on every finding, and its waste class in class: a finding needs at least one evidence entry and a basis of measured or suspected, and each evidence entry names its run, job, file, resource or bill line. Post what the record or tool printed; never restate a gate result in your own words.
- A card's answers are one of four kinds: buttons (options), rows (a choice per row), order (rows to drag into order) or fields. You supply the words and the page knows only the kinds.
- The user's choices and notes reach you together when they press Send, as an event from muda: <channel source="muda" project="…" send_id="…"> in pi, or a turn whose first line is muda event project=… send_id=… in Claude Code. On a Send that takes more than a moment, first call muda_reply with its send id, state working and one line on what you are doing. Settle every Send: answered, declined or failed, with why.
- The page writes to no record. Record decisions only through the record CLI or the record's pull requests, never in muda's page; then post again after every record write, so the page shows what the record now says.
- muda_status says what waits on whom and which Sends are not settled.`

const groupsSchema = `"groups":{"type":"array","description":"groups to add; those it has stay","items":{"type":"object","properties":{` +
	`"kind":{"type":"string","enum":["repo","account"]},` +
	`"key":{"type":"string","description":"a repository's absolute path, or an account's name"},` +
	`"label":{"type":"string"}},"required":["kind","key"]}}`

const optionSchema = `{"type":"object","properties":{"id":{"type":"string"},"label":{"type":"string"},"detail":{"type":"string"}},"required":["id","label"]}`

const answersSchema = `{"type":"object","description":"what the card takes; leave out on a card that asks nothing","properties":{` +
	`"kind":{"type":"string","enum":["buttons","rows","order","fields"]},` +
	`"options":{"type":"array","description":"buttons: one each","items":` + optionSchema + `},` +
	`"rows":{"type":"array","description":"rows: a row each with its options; order: the rows to drag","items":{"type":"object","properties":{"id":{"type":"string"},"label":{"type":"string"},"chosen":{"type":"string"},"options":{"type":"array","items":` + optionSchema + `}},"required":["id","label"]}},` +
	`"fields":{"type":"array","description":"fields: one each","items":{"type":"object","properties":{"id":{"type":"string"},"label":{"type":"string"},"type":{"type":"string","enum":["text","long","date"]},"value":{"type":"string"}},"required":["id","label"]}}},` +
	`"required":["kind"]}`

const cardSchema = `{"type":"object","properties":{` +
	`"job":{"type":"string"},"group":{"type":"string","description":"a group's key"},"stage":{"type":"string"},` +
	`"id":{"type":"string","description":"with job and group, names the card: post it again to revise it"},` +
	`"title":{"type":"string"},` +
	`"kind":{"type":"string","description":"what the card is: finding, baseline, plan, verdict, call, later, step"},"class":{"type":"string","description":"a finding's waste class, such as W3"},` +
	`"basis":{"type":"string","enum":["measured","suspected"],"description":"required on a finding"},` +
	`"evidence":{"type":"array","description":"what you read; at least one entry on a finding","items":{"type":"object","properties":{` +
	`"kind":{"type":"string","enum":["run","job","file","resource","bill_line"]},` +
	`"ref":{"type":"string","description":"the run, job, file, resource or bill line it names"},` +
	`"text":{"type":"string","description":"what it shows"},` +
	`"url":{"type":"string"}},"required":["kind","ref"]}},` +
	`"decided":{"type":"string","description":"what the user already decided on it, as recorded"},` +
	`"recommendation":{"type":"string"},` +
	`"answers":` + answersSchema + `,` +
	`"revision":{"type":"string","description":"one line on what changed, when revising"}},` +
	`"required":["job","group","stage","id","title"]}`

// Tools is muda's tool list. None of them decides anything or presses Send:
// those are the user's, on the page.
func Tools() []channelmcp.Tool {
	return []channelmcp.Tool{
		{Name: "muda_open", Description: "Create a project by title with its groups, or open one by title or id, and make this session its owner, which brings it the user's unanswered messages. Opens the project's page, reusing an open tab. Returns the project's id and URL.",
			InputSchema: schema(`{"type":"object","properties":{"title":{"type":"string"},"project":{"type":"integer","description":"an existing project's id, instead of its title"},` + groupsSchema + `}}`)},
		{Name: "muda_post", Description: "Post or revise jobs, stages, groups and cards, all in one transaction. Refused whole, naming the card and field, for a finding with no evidence entry or no measured or suspected basis, or an evidence entry that names no run, job, file, resource or bill line. Post again after every record write. Returns where the project stands.",
			InputSchema: schema(`{"type":"object","properties":{"project":{"type":"integer"},` +
				`"working":{"type":"string","description":"one line on what you are doing, shown on the page; empty clears it"},` +
				`"jobs":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"label":{"type":"string"},"stages":{"type":"array","description":"in order","items":{"type":"object","properties":{"id":{"type":"string"},"label":{"type":"string"},"state":{"type":"string","enum":["","now","done","flagged"]},"count":{"type":"string","description":"shown after the label: 3 of 5"}},"required":["id","label"]}}},"required":["id","label"]}},` +
				groupsSchema + `,"cards":{"type":"array","items":` + cardSchema + `}},"required":["project"]}`)},
		{Name: "muda_reply", Description: "Settle the user's Sends, all or nothing: working with one line on what you are doing, then answered with your reply, or declined or failed with why. Name whole Sends (each event's send_id) in sends, or single messages in messages to answer cards one by one. Only the session a Send was delivered to may settle it.",
			InputSchema: schema(`{"type":"object","properties":{"sends":{"type":"array","items":{"type":"integer"}},"messages":{"type":"array","items":{"type":"integer"}},` +
				`"state":{"type":"string","enum":["working","answered","declined","failed"]},"text":{"type":"string"}},"required":["state","text"]}`)},
		{Name: "muda_status", Description: "What waits, and on whom: each project's owner, the cards that wait on the user and the Sends that wait on an agent, and every Send not settled with its messages' states.",
			InputSchema: schema(`{"type":"object","properties":{}}`)},
	}
}

// Spec is muda's channel. Its scope is the session's folder (`muda channel
// --scope` names it): serve matches it against each project's groups.
func Spec(version string) pageserve.Spec {
	return pageserve.Spec{
		Tool: "muda", Version: version, Instructions: Instructions, Tools: Tools(), Call: call,
		Answers: "the user's decisions",
		Scope:   func(cwd string) string { return cwd },
		Wake:    wake,
	}
}

// wake reads one answer of GET /api/agent/wait into the event pushed to the agent.
func wake(body json.RawMessage) (string, map[string]string, bool) {
	var got struct {
		ID      int64  `json:"id"`
		Project int64  `json:"project"`
		Text    string `json:"text"`
		Report  string `json:"report"`
	}
	if json.Unmarshal(body, &got) != nil || got.ID == 0 {
		return "", nil, false
	}
	return got.Text, map[string]string{"source": "muda", "project": strconv.FormatInt(got.Project, 10),
		"send_id": strconv.FormatInt(got.ID, 10), channelmcp.MetaReport: got.Report}, true
}

func schema(s string) json.RawMessage { return json.RawMessage(s) }
