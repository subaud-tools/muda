package channel_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/subaud-tools/muda/internal/channel"
	"github.com/subaud-tools/muda/internal/serve"
	"github.com/subaud-tools/muda/internal/store"
	"github.com/subaud-tools/tools-common/msgstate"
	"github.com/subaud-tools/tools-common/pageserve"
	"github.com/subaud-tools/tools-common/threadstore"
)

// rig is a real serve's API over a real store, and a channel per session.
type rig struct {
	t      *testing.T
	st     *store.Store
	client *pageserve.Client
	base   string
	opened []string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "muda.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := serve.New(st)
	s.PageURL = "http://page.test/?t=tok"
	h := httptest.NewServer(s.Handler())
	t.Cleanup(h.Close)
	c := &pageserve.Client{Tool: "muda", HTTP: h.Client(), Find: func() (pageserve.Entry, error) {
		return pageserve.Entry{Base: h.URL, Token: "tok"}, nil
	}}
	return &rig{t: t, st: st, client: c, base: h.URL}
}

// call runs one tool as session.
func (r *rig) call(session, tool, args string) (string, error) {
	r.t.Helper()
	spec := channel.Spec("v")
	ch := pageserve.New(spec, pageserve.Identity{Session: session, Harness: "pi", CWD: "/w/a"}, r.client)
	ch.Open = func(u string) error { r.opened = append(r.opened, u); return nil }
	return spec.Call(context.Background(), ch, tool, json.RawMessage(args))
}

func (r *rig) must(session, tool, args string) map[string]any {
	r.t.Helper()
	out, err := r.call(session, tool, args)
	if err != nil {
		r.t.Fatalf("%s: %v", tool, err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		r.t.Fatalf("%s: %v: %s", tool, err, out)
	}
	return got
}

const openArgs = `{"title":"Delivery","groups":[{"kind":"repo","key":"/w/a","label":"a"},{"kind":"repo","key":"/w/b","label":"b"}]}`

func (r *rig) project(session string) int64 {
	r.t.Helper()
	return int64(r.must(session, "muda_open", openArgs)["project"].(float64))
}

const job = `"jobs":[{"id":"deploy","label":"Deployments","stages":[{"id":"find","label":"Findings","state":"now"}]}]`

const goodEvidence = `[{"kind":"run","ref":"run 123","text":"checkout took 90 s"}]`

func cardArgs(project int64, card string) string {
	return `{"project":` + strconv.FormatInt(project, 10) + `,` + job + `,"cards":[` + card + `]}`
}

func finding(extra string) string {
	return `{"job":"deploy","group":"/w/a","stage":"find","id":"F1","title":"Full clones","kind":"finding","class":"W3"` + extra + `}`
}

func TestPostRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, card string
		want       []string // in the refusal
	}{
		{"a finding with no evidence", finding(`,"basis":"measured"`), []string{`"F1"`, "evidence"}},
		{"a finding with an empty evidence list", finding(`,"basis":"measured","evidence":[]`), []string{`"F1"`, "evidence"}},
		{"a finding with no basis", finding(`,"evidence":` + goodEvidence), []string{`"F1"`, "basis"}},
		{"a finding with another basis", finding(`,"basis":"probable","evidence":` + goodEvidence), []string{`"F1"`, "basis"}},
		{"evidence naming nothing", finding(`,"basis":"suspected","evidence":[{"kind":"run","ref":"run 1"},{"kind":"run","ref":" ","text":"read it"}]`), []string{`"F1"`, "evidence[1]"}},
		{"evidence of another kind", finding(`,"basis":"suspected","evidence":[{"kind":"hunch","ref":"x"}]`), []string{`"F1"`, "evidence[0]"}},
		{"evidence on another card names nothing", `{"job":"deploy","group":"/w/a","stage":"find","id":"P1","title":"Plan","kind":"plan","evidence":[{"text":"read it"}]}`, []string{`"P1"`, "evidence[0]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			id := r.project("s1")
			_, err := r.call("s1", "muda_post", cardArgs(id, tc.card))
			if err == nil {
				t.Fatal("the post was taken")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("the refusal %q does not name %s", err, w)
				}
			}
			if cards, _ := r.st.Cards(context.Background(), id); len(cards) != 0 {
				t.Errorf("the refused post stored %+v", cards)
			}
		})
	}
}

func TestPostTakesWellFormedCards(t *testing.T) {
	r := newRig(t)
	id := r.project("s1")
	r.must("s1", "muda_post", cardArgs(id, finding(`,"basis":"measured","evidence":`+goodEvidence)))
	// A card that is no finding needs neither: a plan to approve.
	r.must("s1", "muda_post", cardArgs(id, `{"job":"deploy","group":"/w/a","stage":"find","id":"P1","title":"Plan","kind":"plan"}`))
	cards, err := r.st.Cards(context.Background(), id)
	if err != nil || len(cards) != 2 {
		t.Fatalf("%+v %v", cards, err)
	}
}

// A card's answers are one of four kinds.
func TestAnswersAreOneOfFourKinds(t *testing.T) {
	r := newRig(t)
	id := r.project("s1")
	for _, a := range []string{
		`{"kind":"buttons","options":[{"id":"ok","label":"Approve"}]}`,
		`{"kind":"rows","rows":[{"id":"ci.yml","label":"ci.yml","options":[{"id":"gate","label":"Gate"}]}]}`,
		`{"kind":"order","rows":[{"id":"F1","label":"First"},{"id":"F2","label":"Second"}]}`,
		`{"kind":"fields","fields":[{"id":"reason","label":"Reason"},{"id":"review","label":"Review date","type":"date"}]}`,
	} {
		r.must("s1", "muda_post", cardArgs(id, `{"job":"deploy","group":"/w/a","stage":"find","id":"A","title":"Pick","kind":"plan","answers":`+a+`}`))
	}
	_, err := r.call("s1", "muda_post", cardArgs(id, `{"job":"deploy","group":"/w/a","stage":"find","id":"B","title":"Pick","kind":"plan","answers":{"kind":"slider"}}`))
	if err == nil || !strings.Contains(err.Error(), `"B"`) || !strings.Contains(err.Error(), "answers") {
		t.Fatalf("a fifth kind: %v", err)
	}
}

// muda_open creates the project, makes the session its owner and opens its page.
func TestOpenCreatesAProject(t *testing.T) {
	r := newRig(t)
	id := r.project("s1")
	p, err := r.st.Project(context.Background(), id)
	if err != nil || p.Title != "Delivery" || p.Owner != "s1" {
		t.Fatalf("%+v %v", p, err)
	}
	if gs, _ := r.st.Groups(context.Background(), id); len(gs) != 2 || gs[0].AddedBy != "s1" {
		t.Fatalf("%+v", gs)
	}
	if len(r.opened) != 1 || r.opened[0] != "http://page.test/?t=tok#/p/"+strconv.FormatInt(id, 10) {
		t.Fatalf("opened %v", r.opened)
	}
}

func TestOpenNeedsASession(t *testing.T) {
	r := newRig(t)
	if _, err := r.call("", "muda_open", openArgs); err == nil {
		t.Fatal("no session, and the open was taken")
	}
}

// send puts one message of yours on the project's thread and sends it.
func (r *rig) send(id int64, body string) threadstore.Message {
	r.t.Helper()
	p, err := r.st.Project(context.Background(), id)
	if err != nil {
		r.t.Fatal(err)
	}
	m, err := r.st.Threads.Post(context.Background(), p.Thread, threadstore.NewMessage{Author: store.You, Body: body})
	if err != nil {
		r.t.Fatal(err)
	}
	return m
}

// wait is one long poll as session, as the channel's loop makes it.
func (r *rig) wait(session, root string) map[string]any {
	r.t.Helper()
	var got map[string]any
	code, err := r.client.Do(context.Background(), http.MethodGet, "/api/agent/wait?"+pageserve.Query("session", session, "root", root, "timeout", "1"), nil, &got)
	if err != nil {
		r.t.Fatal(err)
	}
	if code == http.StatusNoContent {
		return nil
	}
	return got
}

// A later session opening a project takes its unanswered messages.
func TestALaterSessionTakesOver(t *testing.T) {
	r := newRig(t)
	id := r.project("s1")
	r.send(id, "why this order?")
	if got := r.wait("s1", "/w/a"); got == nil {
		t.Fatal("the owner got nothing")
	}
	r.project("s2")
	got := r.wait("s2", "/elsewhere")
	if got == nil || !strings.Contains(got["text"].(string), "why this order?") {
		t.Fatalf("the new owner got %v", got)
	}
}

// A Send reaches a session working in one of the project's repositories, and no
// session working elsewhere, and wake reads it into the agent's event.
func TestASendWakesASessionInItsProjectOnly(t *testing.T) {
	r := newRig(t)
	p, _, err := r.st.Ensure(context.Background(), "Unowned", []store.Group{{Kind: store.Repo, Key: "/w/a"}}, store.You)
	if err != nil {
		t.Fatal(err)
	}
	r.send(p.ID, "look at this")
	if got := r.wait("s9", "/elsewhere"); got != nil {
		t.Fatalf("a session elsewhere got %v", got)
	}
	var body json.RawMessage
	if _, err := r.client.Do(context.Background(), http.MethodGet, "/api/agent/wait?"+pageserve.Query("session", "s1", "root", "/w", "timeout", "1"), nil, &body); err != nil {
		t.Fatal(err)
	}
	text, meta, ok := channel.Spec("v").Wake(body)
	if !ok || !strings.Contains(text, "look at this") || meta["source"] != "muda" || meta["send_id"] == "" || meta["report"] == "" || meta["project"] != strconv.FormatInt(p.ID, 10) {
		t.Fatalf("%q %v %v", text, meta, ok)
	}
}

func TestStatusSaysWhatWaitsOnWhom(t *testing.T) {
	r := newRig(t)
	id := r.project("s1")
	r.must("s1", "muda_post", cardArgs(id, finding(`,"basis":"measured","evidence":`+goodEvidence+`,"answers":{"kind":"buttons","options":[{"id":"ok","label":"Approve"}]}`)))
	m := r.send(id, "and this?")
	got := r.must("s1", "muda_status", `{}`)
	p := got["projects"].([]any)[0].(map[string]any)
	if p["on_you"].(float64) != 1 || p["on_agent"].(float64) != 1 || p["owner"] != "s1" || p["title"] != "Delivery" {
		t.Fatalf("%+v", p)
	}
	u := got["unsettled"].([]any)
	if len(u) != 1 {
		t.Fatalf("%+v", u)
	}
	msg := u[0].(map[string]any)["messages"].([]any)[0].(map[string]any)
	if int64(msg["id"].(float64)) != m.ID || msg["state"] != string(msgstate.Sent) {
		t.Fatalf("%+v", msg)
	}
}

// muda_reply settles a whole Send by its id, or its messages one by one.
func TestReplySettlesASend(t *testing.T) {
	r := newRig(t)
	id := r.project("s1")
	m := r.send(id, "why?")
	got := r.wait("s1", "/w/a")
	if got == nil {
		t.Fatal("nothing delivered")
	}
	send := strconv.FormatInt(int64(got["id"].(float64)), 10)
	r.must("s1", "muda_reply", `{"sends":[`+send+`],"state":"working","text":"reading the run"}`)
	read, _ := r.st.Threads.Message(context.Background(), m.ID)
	if read.State != msgstate.Working || read.Line != "reading the run" {
		t.Fatalf("%+v", read)
	}
	if _, err := r.call("s2", "muda_reply", `{"sends":[`+send+`],"state":"answered","text":"not mine"}`); err == nil {
		t.Fatal("another session settled it")
	}
	r.must("s1", "muda_reply", `{"messages":[`+strconv.FormatInt(m.ID, 10)+`],"state":"answered","text":"because of the cache"}`)
	read, _ = r.st.Threads.Message(context.Background(), m.ID)
	if read.State != msgstate.Answered {
		t.Fatalf("%+v", read)
	}
	if st := r.must("s1", "muda_status", `{}`); len(st["unsettled"].([]any)) != 0 {
		t.Fatalf("still unsettled: %+v", st)
	}
}

func TestInstructionsKeepTheRecordsOwnPath(t *testing.T) {
	spec := channel.Spec("v")
	for _, w := range []string{"post again after every record write", "only through the record CLI or the record's pull requests"} {
		if !strings.Contains(spec.Instructions, w) {
			t.Errorf("the instructions don't say %q", w)
		}
	}
	names := map[string]bool{}
	for _, tl := range spec.Tools {
		names[tl.Name] = true
	}
	for _, n := range []string{"muda_open", "muda_post", "muda_reply", "muda_status"} {
		if !names[n] {
			t.Errorf("no tool %s", n)
		}
	}
}

// A finding is marked by its kind; its class is the waste class, so a card
// whose class is a waste class but whose kind is finding is still checked.
func TestFindingIsMarkedByKindNotClass(t *testing.T) {
	r := newRig(t)
	id := r.project("s1")
	if _, err := r.call("s1", "muda_post", cardArgs(id, `{"job":"deploy","group":"/w/a","stage":"find","id":"F2","title":"Cold cache","kind":"finding","class":"W4","basis":"measured"}`)); err == nil {
		t.Fatal("a finding of class W4 with no evidence was taken")
	}
}
