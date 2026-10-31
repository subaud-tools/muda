package serve

// The agent side of serve: which sessions are here, which projects each may
// hear from, delivery of Send, and what the agent posts and replies.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/subaud-tools/muda/internal/store"
	"github.com/subaud-tools/tools-common/channelmcp"
	"github.com/subaud-tools/tools-common/localweb"
	"github.com/subaud-tools/tools-common/msgstate"
	"github.com/subaud-tools/tools-common/threadstore"
)

// maxWait caps one long poll, in seconds.
const maxWait = 60

type session struct {
	label string
	seen  time.Time
}

// agents is the in-memory half: sessions, and the wake-up for waiters. The zero
// value is ready to use.
type agents struct {
	mu       sync.Mutex
	sessions map[string]*session
	wake     chan struct{} // closed and replaced to wake every waiter
}

// touch registers or refreshes a session; an empty label keeps the one it has.
func (s *Server) touch(id, label string) {
	s.ag.mu.Lock()
	defer s.ag.mu.Unlock()
	if s.ag.sessions == nil {
		s.ag.sessions = map[string]*session{}
	}
	cur := s.ag.sessions[id]
	if cur == nil {
		cur = &session{}
		s.ag.sessions[id] = cur
	}
	if label != "" {
		cur.label = label
	}
	cur.seen = time.Now()
}

func (s *Server) present() []string {
	s.ag.mu.Lock()
	defer s.ag.mu.Unlock()
	out := []string{}
	for id, cur := range s.ag.sessions {
		if time.Since(cur.seen) <= threadstore.DefaultPresenceTTL {
			out = append(out, id)
		}
	}
	return out
}

func (s *Server) isPresent(id string) bool {
	for _, p := range s.present() {
		if p == id {
			return true
		}
	}
	return false
}

// labelOf is what the page calls a session.
func (s *Server) labelOf(id string) string {
	s.ag.mu.Lock()
	defer s.ag.mu.Unlock()
	if cur := s.ag.sessions[id]; cur != nil && cur.label != "" {
		return cur.label
	}
	return id
}

func (s *Server) waker() <-chan struct{} {
	s.ag.mu.Lock()
	defer s.ag.mu.Unlock()
	if s.ag.wake == nil {
		s.ag.wake = make(chan struct{})
	}
	return s.ag.wake
}

func (s *Server) wakeAll() {
	s.ag.mu.Lock()
	defer s.ag.mu.Unlock()
	if s.ag.wake != nil {
		close(s.ag.wake)
	}
	s.ag.wake = make(chan struct{})
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return fmt.Errorf("bad body: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// fail answers err with the status it deserves.
func fail(w http.ResponseWriter, err error) {
	var no store.Refused
	var tno threadstore.Refused
	switch {
	case errors.As(err, &no):
		writeErr(w, http.StatusUnprocessableEntity, no.Reason)
	case errors.As(err, &tno):
		writeErr(w, http.StatusConflict, tno.Reason)
	case errors.Is(err, store.ErrNoProject), errors.Is(err, threadstore.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

// changed tells open pages a project changed.
func (s *Server) changed(project int64) {
	s.events.Emit("project", map[string]int64{"id": project})
}

func (s *Server) agentPresence(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Session string `json:"session"`
		Label   string `json:"label"`
	}
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if b.Session == "" {
		writeErr(w, http.StatusBadRequest, "session is required")
		return
	}
	s.touch(b.Session, b.Label)
	s.wakeAll() // a present session may now be able to claim
	w.WriteHeader(http.StatusNoContent)
}

// within says whether path is dir or under it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// covers says whether a session in root works on g: the repository holds root,
// or root holds the repository (a workspace).
func covers(root string, g store.Group) bool {
	return root != "" && g.Kind == store.Repo && (within(root, g.Key) || within(g.Key, root))
}

// scopesFor is the projects a session may claim from: those it owns and those
// with a repository it works in. threadstore decides among them, owners first.
func (s *Server) scopesFor(ctx context.Context, session, root string) ([]string, error) {
	projects, err := s.st.Projects(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, p := range projects {
		mine := p.Owner == session
		if !mine {
			groups, err := s.st.Groups(ctx, p.ID)
			if err != nil {
				return nil, err
			}
			for _, g := range groups {
				mine = mine || covers(root, g)
			}
		}
		if mine {
			out = append(out, store.Scope(p.ID))
		}
	}
	return out, nil
}

func (s *Server) agentWait(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("session")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "session is required")
		return
	}
	secs := 30
	if v, err := strconv.Atoi(q.Get("timeout")); err == nil && v > 0 {
		secs = min(v, maxWait)
	}
	deadline := time.NewTimer(time.Duration(secs) * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(2 * time.Second) // presence expires with no event to wake us
	defer tick.Stop()
	for {
		wake := s.waker()
		s.touch(id, "") // a connected long poll is a live session
		scopes, err := s.scopesFor(r.Context(), id, q.Get("root"))
		var d *threadstore.Delivery
		if err == nil {
			d, err = s.st.Threads.Claim(r.Context(), id, scopes, s.present())
		}
		if err != nil {
			if r.Context().Err() != nil {
				return
			}
			fail(w, err)
			return
		}
		if d != nil {
			project := store.ScopeProject(d.Scope)
			s.changed(project)
			writeJSON(w, http.StatusOK, delivered{ID: d.BatchID, Project: project, Text: s.sendText(r.Context(), project, d), Report: d.Token})
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			w.WriteHeader(http.StatusNoContent)
			return
		case <-wake:
		case <-tick.C:
		}
	}
}

// delivered is one answer of GET /api/agent/wait.
type delivered struct {
	ID      int64  `json:"id"`
	Project int64  `json:"project"`
	Text    string `json:"text"`
	Report  string `json:"report"`
}

// sendText is what an agent reads when the user presses Send: one line per
// message, with the id that settles it and the card it answers.
func (s *Server) sendText(ctx context.Context, project int64, d *threadstore.Delivery) string {
	title := strconv.FormatInt(project, 10)
	if p, err := s.st.Project(ctx, project); err == nil {
		title = p.Title
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The user sent %d message(s) for muda project %q (project %d, send %d):\n", len(d.Messages), title, project, d.BatchID)
	for _, m := range d.Messages {
		fmt.Fprintf(&b, "- message %d", m.ID)
		if c, err := s.st.CardOfThread(ctx, m.ThreadID); err == nil {
			fmt.Fprintf(&b, " on card %q %q (job %s, group %s, stage %s)", c.ID, c.Title, c.Job, c.Group, c.Stage)
		}
		fmt.Fprintf(&b, ": %s\n", m.Body)
		if len(m.Meta) > 0 {
			fmt.Fprintf(&b, "  answer: %s\n", m.Meta)
		}
	}
	b.WriteString("Write any decision with the record CLI or the record's pull requests, post the cards again with muda_post, and settle this with muda_reply.")
	return b.String()
}

// agentReport is POST /api/agent/report: the harness says a Send it was handed
// is held or landed, or that the session's turn ended.
func (s *Server) agentReport(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Session string                 `json:"session"`
		Event   channelmcp.ReportEvent `json:"event"`
		Token   string                 `json:"token"`
	}
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if b.Session == "" {
		writeErr(w, http.StatusBadRequest, "session is required")
		return
	}
	var err error
	switch b.Event {
	case channelmcp.ReportHeld:
		_, err = s.st.Threads.Held(r.Context(), b.Session, b.Token)
	case channelmcp.ReportLanded:
		_, err = s.st.Threads.Landed(r.Context(), b.Session, b.Token)
	case channelmcp.ReportTurnEnded:
		_, err = s.st.Threads.TurnEnded(r.Context(), b.Session)
	default:
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("event %q: want held, landed or turn_ended", b.Event))
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	s.changed(0)
	w.WriteHeader(http.StatusNoContent)
}

// agentOpen is POST /api/agent/open: it creates the project by title or finds it
// by title or id, adds groups, and makes the session its owner, which brings the
// session what the project has not answered.
func (s *Server) agentOpen(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Session string        `json:"session"`
		Label   string        `json:"label"`
		Project int64         `json:"project"`
		Title   string        `json:"title"`
		Groups  []store.Group `json:"groups"`
	}
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if b.Session == "" {
		writeErr(w, http.StatusBadRequest, "session is required")
		return
	}
	ctx := r.Context()
	var p store.Project
	var err error
	created := false
	if b.Project != 0 {
		if p, err = s.st.Project(ctx, b.Project); err == nil {
			err = s.st.AddGroups(ctx, p.ID, b.Groups, b.Session)
		}
	} else {
		p, created, err = s.st.Ensure(ctx, b.Title, b.Groups, b.Session)
	}
	if err == nil {
		err = s.st.Take(ctx, p.ID, b.Session)
	}
	if err != nil {
		fail(w, err)
		return
	}
	s.touch(b.Session, b.Label)
	s.wakeAll()
	s.changed(p.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"project": p.ID, "title": p.Title, "created": created,
		"url": s.PageURL + "#/p/" + strconv.FormatInt(p.ID, 10), "tab": s.showIn(p.ID),
	})
}

// showIn puts the project in a page tab when one is open, and says which
// happened: "reused" (a tab shows it, moved to it, or only tabs on other projects
// are open, which list it in their bar) or "opened" (no tab is open).
func (s *Server) showIn(project int64) string {
	if s.tabs == nil {
		return "opened"
	}
	page := strconv.FormatInt(project, 10)
	c := s.tabs.Pick(page)
	switch c.Kind {
	case localweb.TabNone:
		return "opened"
	case localweb.TabMove:
		s.tabs.Moved(c.Tab, page)
		s.events.Emit("show", map[string]string{"tab": c.Tab, "page": page})
	}
	return "reused"
}

// agentPost is POST /api/agent/post: jobs, stages, groups and cards in one
// transaction, refused whole with the card and field that broke a rule.
func (s *Server) agentPost(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Session string `json:"session"`
		Project int64  `json:"project"`
		store.Posted
	}
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.st.Post(r.Context(), b.Project, b.Posted, b.Session); err != nil {
		fail(w, err)
		return
	}
	s.changed(b.Project)
	st, err := s.standing(r.Context(), b.Project)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// agentReply is POST /api/agent/reply: the agent settles whole Sends, messages
// one by one, or both, all or nothing.
func (s *Server) agentReply(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Session  string         `json:"session"`
		Sends    []int64        `json:"sends"`
		Messages []int64        `json:"messages"`
		State    msgstate.State `json:"state"`
		Text     string         `json:"text"`
	}
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	ids := append([]int64{}, b.Messages...)
	projects := map[int64]bool{}
	for _, id := range b.Sends {
		batch, err := s.st.Threads.Batch(ctx, id)
		if err != nil {
			fail(w, err)
			return
		}
		projects[store.ScopeProject(batch.Scope)] = true
		for _, m := range batch.Messages {
			if !msgstate.Final(m.State) {
				ids = append(ids, m.ID)
			}
		}
	}
	if _, err := s.st.Threads.Reply(ctx, b.Session, ids, b.State, b.Text, nil); err != nil {
		fail(w, err)
		return
	}
	for _, id := range b.Messages {
		if m, err := s.st.Threads.Message(ctx, id); err == nil {
			if th, err := s.st.Threads.ThreadByID(ctx, m.ThreadID); err == nil {
				projects[store.ScopeProject(th.Scope)] = true
			}
		}
	}
	for p := range projects {
		s.changed(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": ids, "state": b.State})
}

// Standing is where a project stands: who owns it, and how much waits on whom.
// OnYou counts cards that take an answer you have not sent; OnAgent counts your
// Sends the agent has not settled.
type Standing struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Owner        string `json:"owner"`
	OwnerLabel   string `json:"owner_label"`
	OwnerPresent bool   `json:"owner_present"`
	Working      string `json:"working"`
	OnYou        int    `json:"on_you"`
	OnAgent      int    `json:"on_agent"`
}

func (s *Server) standing(ctx context.Context, id int64) (Standing, error) {
	p, err := s.st.Project(ctx, id)
	if err != nil {
		return Standing{}, err
	}
	cards, err := s.st.Cards(ctx, id)
	if err != nil {
		return Standing{}, err
	}
	sends, err := s.st.Threads.Unsettled(ctx, []string{store.Scope(id)})
	if err != nil {
		return Standing{}, err
	}
	out := Standing{ID: p.ID, Title: p.Title, Owner: p.Owner, Working: p.Working, OnAgent: len(sends)}
	if p.Owner != "" {
		out.OwnerLabel, out.OwnerPresent = s.labelOf(p.Owner), s.isPresent(p.Owner)
	}
	for _, c := range cards {
		if c.NeedsYou {
			out.OnYou++
		}
	}
	return out, nil
}

// AgentStatus is GET /api/agent/status: every project's standing, and every Send
// not settled.
type AgentStatus struct {
	Projects  []Standing      `json:"projects"`
	Unsettled []UnsettledSend `json:"unsettled"`
}

// UnsettledSend is a Send the agent has not settled, with whom it is with ("":
// waiting to be claimed).
type UnsettledSend struct {
	ID       int64              `json:"id"`
	Project  int64              `json:"project"`
	With     string             `json:"with"`
	Held     bool               `json:"held"`
	At       time.Time          `json:"at"`
	Messages []UnsettledMessage `json:"messages"`
}

// UnsettledMessage is one message of an unsettled Send.
type UnsettledMessage struct {
	ID    int64          `json:"id"`
	State msgstate.State `json:"state"`
	Text  string         `json:"text"`
	Line  string         `json:"line"`
}

func (s *Server) agentStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projects, err := s.st.Projects(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	out := AgentStatus{Projects: []Standing{}, Unsettled: []UnsettledSend{}}
	scopes := make([]string, 0, len(projects))
	for _, p := range projects {
		st, err := s.standing(ctx, p.ID)
		if err != nil {
			fail(w, err)
			return
		}
		out.Projects = append(out.Projects, st)
		scopes = append(scopes, store.Scope(p.ID))
	}
	sends, err := s.st.Threads.Unsettled(ctx, scopes)
	if err != nil {
		fail(w, err)
		return
	}
	for _, b := range sends {
		u := UnsettledSend{ID: b.ID, Project: store.ScopeProject(b.Scope), With: b.ClaimedBy, Held: b.Held, At: b.SentAt, Messages: []UnsettledMessage{}}
		for _, m := range b.Messages {
			u.Messages = append(u.Messages, UnsettledMessage{ID: m.ID, State: m.State, Text: m.Body, Line: m.Line})
		}
		out.Unsettled = append(out.Unsettled, u)
	}
	writeJSON(w, http.StatusOK, out)
}
