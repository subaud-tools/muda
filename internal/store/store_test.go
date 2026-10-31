package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/subaud-tools/tools-common/msgstate"
	"github.com/subaud-tools/tools-common/threadstore"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "muda.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestPathIsInMudasStateDir(t *testing.T) {
	t.Setenv("MUDA_HOME", "/tmp/muda-home")
	if got, want := Path(), "/tmp/muda-home/state/muda.db"; got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

// A project keeps its groups, and asking for its title again opens it.
func TestProjectKeepsItsGroupsAndOpensByTitle(t *testing.T) {
	st, ctx := open(t), context.Background()
	p, created, err := st.Ensure(ctx, "Delivery", []Group{{Kind: Repo, Key: "/w/a", Label: "a"}}, "you")
	if err != nil || !created {
		t.Fatalf("%v %v", created, err)
	}
	again, created, err := st.Ensure(ctx, "Delivery", []Group{{Kind: Account, Key: "prod", Label: "Production"}}, "s1")
	if err != nil || created || again.ID != p.ID {
		t.Fatalf("second open: %+v %v %v", again, created, err)
	}
	gs, err := st.Groups(ctx, p.ID)
	if err != nil || len(gs) != 2 {
		t.Fatalf("%+v %v", gs, err)
	}
	if gs[1] != (Group{Kind: Account, Key: "prod", Label: "Production", AddedBy: "s1"}) {
		t.Fatalf("%+v", gs[1])
	}
}

func TestAGroupIsARepoOrAnAccount(t *testing.T) {
	st := open(t)
	if _, _, err := st.Ensure(context.Background(), "P", []Group{{Kind: "cluster", Key: "x"}}, "you"); err == nil {
		t.Fatal("a third kind of group was stored")
	}
}

func post(t *testing.T, st *Store, id int64, c ...Card) {
	t.Helper()
	err := st.Post(context.Background(), id, Posted{
		Jobs: []Job{{ID: "deploy", Label: "Deployments", Stages: []Stage{
			{ID: "base", Label: "Baseline", State: "done"}, {ID: "find", Label: "Findings", State: "now", Count: "1 of 2"}}}},
		Groups: []Group{{Kind: Repo, Key: "/w/a", Label: "a"}}, Cards: c,
	}, "s1")
	if err != nil {
		t.Fatal(err)
	}
}

var finding = Card{Job: "deploy", Group: "/w/a", Stage: "find", ID: "F1", Title: "Full clones", Class: "finding", Basis: "measured",
	Evidence:       []Evidence{{Kind: "run", Ref: "run 123", Text: "checkout took 90 s", URL: "https://example.com/run/123"}},
	Recommendation: "Clone shallow.",
	Answers:        &Answers{Kind: "buttons", Options: []Option{{ID: "ok", Label: "Approve"}}}}

// A job keeps its stages in order with each one's state and count, and a card
// comes back as it was posted, with a thread of its project's.
func TestJobsStagesAndCardsRoundTrip(t *testing.T) {
	st, ctx := open(t), context.Background()
	p, _, _ := st.Ensure(ctx, "P", nil, "you")
	post(t, st, p.ID, finding)
	jobs, err := st.Jobs(ctx, p.ID)
	if err != nil || len(jobs) != 1 || len(jobs[0].Stages) != 2 || jobs[0].Stages[1] != (Stage{ID: "find", Label: "Findings", State: "now", Count: "1 of 2"}) {
		t.Fatalf("%+v %v", jobs, err)
	}
	cards, err := st.Cards(ctx, p.ID)
	if err != nil || len(cards) != 1 {
		t.Fatalf("%+v %v", cards, err)
	}
	c := cards[0]
	if c.Title != finding.Title || c.Basis != "measured" || c.Evidence[0] != finding.Evidence[0] || c.Answers.Options[0].Label != "Approve" || c.PostedAt.IsZero() {
		t.Fatalf("%+v", c)
	}
	th, err := st.Threads.ThreadByID(ctx, c.Thread)
	if err != nil || th.Scope != Scope(p.ID) {
		t.Fatalf("thread %+v %v", th, err)
	}
}

// A post can revise one card and leave the rest, in one transaction: a bad card
// undoes the whole post.
func TestPostIsOneTransaction(t *testing.T) {
	st, ctx := open(t), context.Background()
	p, _, _ := st.Ensure(ctx, "P", nil, "you")
	bad := finding
	bad.Stage = "nosuch"
	err := st.Post(ctx, p.ID, Posted{
		Jobs:   []Job{{ID: "deploy", Label: "Deployments", Stages: []Stage{{ID: "find", Label: "Findings"}}}},
		Groups: []Group{{Kind: Repo, Key: "/w/a"}}, Cards: []Card{bad}}, "s1")
	if err == nil {
		t.Fatal("a card at no stage was stored")
	}
	if jobs, _ := st.Jobs(ctx, p.ID); len(jobs) != 0 {
		t.Fatalf("the failed post left %+v", jobs)
	}
}

// A card with answers needs you until you send an answer; revising it asks
// again; posting it unchanged does not.
func TestACardNeedsYouUntilAnswered(t *testing.T) {
	st, ctx := open(t), context.Background()
	p, _, _ := st.Ensure(ctx, "P", nil, "you")
	post(t, st, p.ID, finding)
	card := func() Card {
		cs, err := st.Cards(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		return cs[0]
	}
	if !card().NeedsYou {
		t.Fatal("an unanswered card does not need you")
	}
	if _, err := st.Threads.Draft(ctx, card().Thread, threadstore.NewMessage{Author: You, Body: "Approve"}); err != nil {
		t.Fatal(err)
	}
	if !card().NeedsYou {
		t.Fatal("a draft answered the card")
	}
	batch, _, _ := st.Threads.DraftBatch(ctx, Scope(p.ID))
	if _, err := st.Threads.SendBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if card().NeedsYou {
		t.Fatal("a sent answer still needs you")
	}
	post(t, st, p.ID, finding)
	if card().NeedsYou {
		t.Fatal("posting it unchanged asked again")
	}
}

// Taking a project over moves its unanswered messages to the new session.
func TestTakingOverMovesUnansweredMessages(t *testing.T) {
	st, ctx := open(t), context.Background()
	p, _, _ := st.Ensure(ctx, "P", nil, "you")
	post(t, st, p.ID, finding)
	if err := st.Take(ctx, p.ID, "s1"); err != nil {
		t.Fatal(err)
	}
	cards, _ := st.Cards(ctx, p.ID)
	if _, err := st.Threads.Post(ctx, cards[0].Thread, threadstore.NewMessage{Author: You, Body: "why?"}); err != nil {
		t.Fatal(err)
	}
	present := []string{"s1", "s2"}
	d, err := st.Threads.Claim(ctx, "s1", []string{Scope(p.ID)}, present)
	if err != nil || d == nil {
		t.Fatalf("%v %v", d, err)
	}
	if err := st.Take(ctx, p.ID, "s2"); err != nil {
		t.Fatal(err)
	}
	got, err := st.Project(ctx, p.ID)
	if err != nil || got.Owner != "s2" {
		t.Fatalf("%+v %v", got, err)
	}
	d, err = st.Threads.Claim(ctx, "s2", []string{Scope(p.ID)}, present)
	if err != nil || d == nil || d.Messages[0].Body != "why?" || d.Messages[0].State != msgstate.Sent {
		t.Fatalf("the message did not move: %+v %v", d, err)
	}
}
