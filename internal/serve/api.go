package serve

// The page's side of serve: what it reads of projects, and the threads, drafts
// and Sends it writes through threadstore. It writes to no record.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/subaud-tools/muda/internal/store"
	"github.com/subaud-tools/tools-common/msgstate"
	"github.com/subaud-tools/tools-common/threadstore"
)

func (s *Server) pageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.newProject)
	mux.HandleFunc("GET /api/projects/{id}", s.getProject)
	mux.HandleFunc("POST /api/projects/{id}/groups", s.addGroups)
	mux.HandleFunc("POST /api/projects/{id}/drafts", s.addDraft)
	mux.HandleFunc("PUT /api/projects/{id}/drafts/{draft}", s.editDraft)
	mux.HandleFunc("DELETE /api/projects/{id}/drafts/{draft}", s.removeDraft)
	mux.HandleFunc("DELETE /api/projects/{id}/drafts", s.discardDrafts)
	mux.HandleFunc("POST /api/projects/{id}/send", s.send)
	mux.HandleFunc("GET /api/scan", s.scan)
}

// JobStanding is where one job has reached.
type JobStanding struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Stage string `json:"stage"`
	Count string `json:"count"`
}

// ProjectRow is a project on the front page.
type ProjectRow struct {
	Standing
	Groups int           `json:"groups"`
	Jobs   []JobStanding `json:"jobs"`
}

// ProjectList is GET /api/projects. Cursor is where the page's event stream
// starts, and Home lets the page show paths under it as ~/….
type ProjectList struct {
	Cursor   string       `json:"cursor"`
	Home     string       `json:"home"`
	Projects []ProjectRow `json:"projects"`
}

// ProjectView is GET /api/projects/{id}: a project whole. Thread is its own
// thread, for a note on no card. Messages are every thread's, in order, each
// naming its thread; Drafts are what goes on the next Send.
type ProjectView struct {
	Standing
	Cursor   string             `json:"cursor"`
	Thread   int64              `json:"thread"`
	Jobs     []store.Job        `json:"jobs"`
	Groups   []store.Group      `json:"groups"`
	Cards    []store.Card       `json:"cards"`
	Messages []msgstate.Message `json:"messages"`
	Drafts   []DraftView        `json:"drafts"`
}

// DraftView is one draft: a note, or a pick with its Answer.
type DraftView struct {
	ID     int64         `json:"id"`
	Thread int64         `json:"thread"`
	Text   string        `json:"text"`
	Answer *store.Answer `json:"answer"`
}

// DraftBody is what the page writes of a draft.
type DraftBody struct {
	Thread int64         `json:"thread"`
	Text   string        `json:"text"`
	Answer *store.Answer `json:"answer"`
}

// ProjectBody is New project: a title with the groups ticked, or more groups.
type ProjectBody struct {
	Title  string        `json:"title"`
	Groups []store.Group `json:"groups"`
}

// SendResult is POST /api/projects/{id}/send: how many messages went.
type SendResult struct {
	Sent int `json:"sent"`
}

// projectID reads the path's {id}; a bad one answers as a missing project.
func projectID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		writeErr(w, http.StatusNotFound, "no "+name+" "+r.PathValue(name))
		return 0, false
	}
	return id, true
}

func jobStandings(jobs []store.Job) []JobStanding {
	out := make([]JobStanding, 0, len(jobs))
	for _, j := range jobs {
		st := JobStanding{ID: j.ID, Label: j.Label}
		var at *store.Stage
		for i := range j.Stages {
			g := &j.Stages[i]
			if g.State == "now" {
				at = g
				break
			}
			if at == nil && g.State != "done" {
				at = g
			}
		}
		if at == nil && len(j.Stages) > 0 {
			at = &j.Stages[len(j.Stages)-1]
		}
		if at != nil {
			st.Stage, st.Count = at.Label, at.Count
		}
		out = append(out, st)
	}
	return out
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	home, _ := os.UserHomeDir()
	out := ProjectList{Cursor: s.events.Cursor(), Home: home, Projects: []ProjectRow{}}
	projects, err := s.st.Projects(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	for _, p := range projects {
		st, err := s.standing(ctx, p.ID)
		if err != nil {
			fail(w, err)
			return
		}
		groups, err := s.st.Groups(ctx, p.ID)
		if err != nil {
			fail(w, err)
			return
		}
		jobs, err := s.st.Jobs(ctx, p.ID)
		if err != nil {
			fail(w, err)
			return
		}
		out.Projects = append(out.Projects, ProjectRow{Standing: st, Groups: len(groups), Jobs: jobStandings(jobs)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) newProject(w http.ResponseWriter, r *http.Request) {
	var b ProjectBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p, _, err := s.st.Ensure(r.Context(), b.Title, b.Groups, store.You)
	if err != nil {
		fail(w, err)
		return
	}
	s.changed(p.ID)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) addGroups(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r, "id")
	if !ok {
		return
	}
	var b ProjectBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.st.Project(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	if err := s.st.AddGroups(r.Context(), id, b.Groups, store.You); err != nil {
		fail(w, err)
		return
	}
	s.changed(id)
	w.WriteHeader(http.StatusNoContent)
}

// who is what the page calls a message's author.
func (s *Server) who(author string) string {
	if author == store.You {
		return store.You
	}
	return s.labelOf(author)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	// Read the cursor first: an event racing the reads is delivered again, never
	// skipped.
	v := ProjectView{Cursor: s.events.Cursor(), Messages: []msgstate.Message{}, Drafts: []DraftView{}}
	err := func() (err error) {
		if v.Standing, err = s.standing(ctx, id); err != nil {
			return err
		}
		p, err := s.st.Project(ctx, id)
		if err != nil {
			return err
		}
		v.Thread = p.Thread
		if v.Jobs, err = s.st.Jobs(ctx, id); err != nil {
			return err
		}
		if v.Groups, err = s.st.Groups(ctx, id); err != nil {
			return err
		}
		if v.Cards, err = s.st.Cards(ctx, id); err != nil {
			return err
		}
		if v.Messages, err = s.messages(ctx, id); err != nil {
			return err
		}
		v.Drafts, err = s.drafts(ctx, id)
		return err
	}()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// messages are a project's messages on the wire, leaving out its drafts.
func (s *Server) messages(ctx context.Context, project int64) ([]msgstate.Message, error) {
	threads, err := s.st.Threads.Threads(ctx, store.Scope(project))
	if err != nil {
		return nil, err
	}
	out := []msgstate.Message{}
	for _, th := range threads {
		msgs, err := s.st.Threads.Messages(ctx, th.ID)
		if err != nil {
			return nil, err
		}
		for _, m := range msgs {
			if m.State == threadstore.Draft {
				continue
			}
			out = append(out, msgstate.Message{ID: m.ID, Thread: strconv.FormatInt(m.ThreadID, 10), Who: s.who(m.Author),
				Text: m.Body, When: m.CreatedAt, State: m.State, ReplyTo: m.ReplyTo, Line: m.Line})
		}
	}
	return out, nil
}

func (s *Server) drafts(ctx context.Context, project int64) ([]DraftView, error) {
	_, msgs, err := s.st.Threads.DraftBatch(ctx, store.Scope(project))
	if err != nil {
		return nil, err
	}
	out := make([]DraftView, 0, len(msgs))
	for _, m := range msgs {
		d := DraftView{ID: m.ID, Thread: m.ThreadID, Text: m.Body}
		if len(m.Meta) > 0 {
			d.Answer = &store.Answer{}
			if err := json.Unmarshal(m.Meta, d.Answer); err != nil {
				return nil, err
			}
		}
		out = append(out, d)
	}
	return out, nil
}

func (s *Server) addDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r, "id")
	if !ok {
		return
	}
	var b DraftBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(b.Text) == "" {
		fail(w, store.Refused{Reason: "a draft needs text"})
		return
	}
	ctx := r.Context()
	th, err := s.st.Threads.ThreadByID(ctx, b.Thread)
	if err == nil && th.Scope != store.Scope(id) {
		err = threadstore.ErrNotFound
	}
	if err != nil {
		fail(w, err)
		return
	}
	var meta json.RawMessage
	if b.Answer != nil {
		if meta, err = json.Marshal(b.Answer); err != nil {
			fail(w, err)
			return
		}
	}
	m, err := s.st.Threads.Draft(ctx, b.Thread, threadstore.NewMessage{Author: store.You, Body: b.Text, Meta: meta})
	if err != nil {
		fail(w, err)
		return
	}
	s.changed(id)
	writeJSON(w, http.StatusOK, DraftView{ID: m.ID, Thread: m.ThreadID, Text: m.Body, Answer: b.Answer})
}

func (s *Server) editDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r, "id")
	if !ok {
		return
	}
	draft, ok := projectID(w, r, "draft")
	if !ok {
		return
	}
	var b DraftBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.st.Threads.EditDraft(r.Context(), draft, b.Text); err != nil {
		fail(w, err)
		return
	}
	s.changed(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r, "id")
	if !ok {
		return
	}
	draft, ok := projectID(w, r, "draft")
	if !ok {
		return
	}
	if err := s.st.Threads.RemoveDraft(r.Context(), draft); err != nil {
		fail(w, err)
		return
	}
	s.changed(id)
	w.WriteHeader(http.StatusNoContent)
}

// discardDrafts is the send bar's Discard: every draft of the project goes.
func (s *Server) discardDrafts(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	_, msgs, err := s.st.Threads.DraftBatch(ctx, store.Scope(id))
	if err != nil {
		fail(w, err)
		return
	}
	for _, m := range msgs {
		if err := s.st.Threads.RemoveDraft(ctx, m.ID); err != nil && !errors.Is(err, threadstore.ErrNotFound) {
			fail(w, err)
			return
		}
	}
	s.changed(id)
	w.WriteHeader(http.StatusNoContent)
}

// send is Send to the agent: the project's drafts go out as one Send and wake
// its session.
func (s *Server) send(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	batch, msgs, err := s.st.Threads.DraftBatch(ctx, store.Scope(id))
	if err != nil {
		fail(w, err)
		return
	}
	if len(msgs) == 0 {
		writeErr(w, http.StatusConflict, "nothing to send")
		return
	}
	n, err := s.st.Threads.SendBatch(ctx, batch)
	if err != nil {
		fail(w, err)
		return
	}
	s.wakeAll()
	s.changed(id)
	writeJSON(w, http.StatusOK, SendResult{Sent: n})
}
