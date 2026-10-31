// Package store is muda's page database: threadstore's tables for threads,
// messages, drafts and Sends, and muda's own for projects, groups, jobs, stages
// and cards. It holds what agents post and never a record: `.muda/` and the
// other records stay the only record of decisions.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/msgstate"
	"github.com/subaud-tools/tools-common/sqlitedb"
	"github.com/subaud-tools/tools-common/threadstore"
)

// schema is muda's own step, after threadstore's first.
const schema = `
CREATE TABLE projects (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  title      TEXT NOT NULL UNIQUE,
  working    TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE TABLE project_groups (
  project  INTEGER NOT NULL REFERENCES projects(id),
  key      TEXT NOT NULL,
  kind     TEXT NOT NULL CHECK (kind IN ('repo', 'account')),
  label    TEXT NOT NULL DEFAULT '',
  added_by TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL,
  PRIMARY KEY (project, key)
);
CREATE TABLE jobs (
  project  INTEGER NOT NULL REFERENCES projects(id),
  id       TEXT NOT NULL,
  label    TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL,
  PRIMARY KEY (project, id)
);
CREATE TABLE stages (
  project  INTEGER NOT NULL,
  job      TEXT NOT NULL,
  id       TEXT NOT NULL,
  label    TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL,
  state    TEXT NOT NULL DEFAULT '',
  count    TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (project, job, id),
  FOREIGN KEY (project, job) REFERENCES jobs(project, id)
);
CREATE TABLE cards (
  uid            INTEGER PRIMARY KEY AUTOINCREMENT,
  project        INTEGER NOT NULL,
  job            TEXT NOT NULL,
  grp            TEXT NOT NULL,
  stage          TEXT NOT NULL,
  id             TEXT NOT NULL,
  title          TEXT NOT NULL,
  kind           TEXT NOT NULL DEFAULT '',
  class          TEXT NOT NULL DEFAULT '',
  basis          TEXT NOT NULL DEFAULT '',
  evidence       TEXT NOT NULL DEFAULT '[]',
  decided        TEXT NOT NULL DEFAULT '',
  recommendation TEXT NOT NULL DEFAULT '',
  answers        TEXT NOT NULL DEFAULT '',
  revision       TEXT NOT NULL DEFAULT '',
  posted_at      INTEGER NOT NULL,
  thread_id      INTEGER NOT NULL REFERENCES thread_threads(id),
  UNIQUE (project, job, grp, id),
  FOREIGN KEY (project, job, stage) REFERENCES stages(project, job, id),
  FOREIGN KEY (project, grp) REFERENCES project_groups(project, key)
);
`

// You is the author of what the user writes on the page.
const You = "you"

// The kinds a group can be.
const (
	Repo    = "repo"
	Account = "account"
)

// projectThread names the thread for what is about the project and no card.
const projectThread = "project"

// Project is a named set of groups. Owner is its scope's owner in threadstore,
// which a takeover there moves too, so it is kept nowhere else.
type Project struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Working   string    `json:"working"`
	Owner     string    `json:"owner"`
	Thread    int64     `json:"thread"`
	CreatedAt time.Time `json:"created_at"`
}

// Group is a repository (Key is its path) or an account (Key is its name).
type Group struct {
	Kind    string `json:"kind"`
	Key     string `json:"key"`
	Label   string `json:"label"`
	AddedBy string `json:"added_by"`
}

// Job is a line of work in a project, with its stages in order.
type Job struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Stages []Stage `json:"stages"`
}

// Stage is one step of a job. State is what the page's stage track takes: done,
// now, flagged or empty. Count is shown after the label, such as "3 of 5".
type Stage struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	State string `json:"state"`
	Count string `json:"count"`
}

// Evidence is one thing the agent read. Kind says what Ref names: run, job,
// file, resource or bill_line. URL, when it has one, makes it a link.
type Evidence struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

// Option is one thing to pick.
type Option struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// AnswerRow is a row taking one choice (kind rows), or a row to order (kind
// order, with no options).
type AnswerRow struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Options []Option `json:"options,omitempty"`
	Chosen  string   `json:"chosen,omitempty"`
}

// Field is one field to fill in. Type is text, date or long.
type Field struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Type  string `json:"type,omitempty"`
	Value string `json:"value,omitempty"`
}

// Answers is what a card takes, one of four kinds: buttons (Options), rows (a
// choice per row), order (drag the Rows) or fields (Fields). The skill supplies
// the words and the page knows only the kinds.
type Answers struct {
	Kind    string      `json:"kind"`
	Options []Option    `json:"options,omitempty"`
	Rows    []AnswerRow `json:"rows,omitempty"`
	Fields  []Field     `json:"fields,omitempty"`
}

// Card is what the agent posts for the user to read and answer. Its job, group
// and id name it within its project. Decided is what the user already decided
// on it, as the agent recorded it.
type Card struct {
	Job   string `json:"job"`
	Group string `json:"group"`
	Stage string `json:"stage"`
	ID    string `json:"id"`
	Title string `json:"title"`
	// Kind is what the card is: finding, baseline, plan, verdict, call, later
	// or step. Class is a finding's waste class, such as W3.
	Kind           string     `json:"kind"`
	Class          string     `json:"class"`
	Basis          string     `json:"basis"`
	Evidence       []Evidence `json:"evidence"`
	Decided        string     `json:"decided"`
	Recommendation string     `json:"recommendation"`
	Answers        *Answers   `json:"answers"`
	Revision       string     `json:"revision"`
	PostedAt       time.Time  `json:"posted_at"`
	// Thread is the card's threadstore thread; NeedsYou is whether it takes an
	// answer you have not sent since it was last posted.
	Thread   int64 `json:"thread"`
	NeedsYou bool  `json:"needs_you"`
}

// Posted is one post: every part optional, each revising what has its id.
// Working, when set, replaces the project's working line.
type Posted struct {
	Working *string `json:"working"`
	Jobs    []Job   `json:"jobs"`
	Groups  []Group `json:"groups"`
	Cards   []Card  `json:"cards"`
}

// Store is muda's database.
type Store struct {
	DB      *sqlitedb.DB
	Threads *threadstore.Store
	Now     func() time.Time
}

// Path is where the database is.
func Path() string { return filepath.Join(tools.StateDir("muda"), "muda.db") }

// Open opens and migrates the database at path. threadstore's steps are taken
// by position, as it asks: a later one goes after muda's steps of that time.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sqlitedb.Open(ctx, path, sqlitedb.Options{Migrations: []sqlitedb.Step{
		threadstore.Migrations[0],
		sqlitedb.SQL(schema),
	}})
	if err != nil {
		return nil, err
	}
	return &Store{DB: db, Threads: threadstore.New(db), Now: time.Now}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

// Scope is a project's scope in threadstore.
func Scope(project int64) string { return "project:" + strconv.FormatInt(project, 10) }

// ScopeProject is the project a scope names, 0 for another scope.
func ScopeProject(scope string) int64 {
	v, ok := strings.CutPrefix(scope, "project:")
	if !ok {
		return 0
	}
	id, _ := strconv.ParseInt(v, 10, 64)
	return id
}

// tx runs fn in a transaction opening with a write, as threadstore asks.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE projects SET title = title WHERE id = 0"); err != nil {
			return err
		}
		return fn(tx)
	})
}

// Ensure opens the project with this title, creating it if there is none, and
// adds groups it doesn't have yet.
func (s *Store) Ensure(ctx context.Context, title string, groups []Group, by string) (Project, bool, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Project{}, false, Refused{"a project needs a title"}
	}
	var id int64
	created := false
	err := s.tx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, "SELECT id FROM projects WHERE title = ?", title).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			created = true
			err = tx.QueryRowContext(ctx, "INSERT INTO projects(title, created_at) VALUES (?, ?) RETURNING id", title, s.Now().UnixMilli()).Scan(&id)
			if err == nil {
				_, err = s.Threads.ThreadTx(ctx, tx, Scope(id), projectThread)
			}
		}
		if err != nil {
			return err
		}
		return addGroups(ctx, tx, id, groups, by)
	})
	if err != nil {
		return Project{}, false, err
	}
	p, err := s.Project(ctx, id)
	return p, created, err
}

// AddGroups adds groups to a project, leaving those it has.
func (s *Store) AddGroups(ctx context.Context, project int64, groups []Group, by string) error {
	return s.tx(ctx, func(tx *sql.Tx) error { return addGroups(ctx, tx, project, groups, by) })
}

func addGroups(ctx context.Context, tx *sql.Tx, project int64, groups []Group, by string) error {
	for _, g := range groups {
		if g.Kind != Repo && g.Kind != Account {
			return Refused{fmt.Sprintf("group %q: kind %q: want repo or account", g.Key, g.Kind)}
		}
		if g.Key == "" {
			return Refused{"a group needs a key"}
		}
		if g.AddedBy == "" {
			g.AddedBy = by
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_groups(project, key, kind, label, added_by, position)
			VALUES (?1, ?2, ?3, ?4, ?5, (SELECT COUNT(*) FROM project_groups WHERE project = ?1))
			ON CONFLICT(project, key) DO NOTHING`, project, g.Key, g.Kind, g.Label, g.AddedBy); err != nil {
			return err
		}
	}
	return nil
}

// Take makes session the project's owner, bringing it what the project has not
// answered.
func (s *Store) Take(ctx context.Context, project int64, session string) error {
	_, err := s.Threads.SetOwner(ctx, Scope(project), session)
	return err
}

// ErrNoProject is a project id that doesn't exist.
var ErrNoProject = errors.New("no such project")

// Project reads one project.
func (s *Store) Project(ctx context.Context, id int64) (Project, error) {
	var p Project
	var at int64
	err := s.DB.QueryRowContext(ctx, "SELECT id, title, working, created_at FROM projects WHERE id = ?", id).Scan(&p.ID, &p.Title, &p.Working, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("%w: %d", ErrNoProject, id)
	}
	if err != nil {
		return p, err
	}
	p.CreatedAt = time.UnixMilli(at).UTC()
	ths, err := s.Threads.Threads(ctx, Scope(id))
	if err != nil {
		return p, err
	}
	for _, th := range ths {
		p.Owner = th.Owner
		if th.Name == projectThread {
			p.Thread = th.ID
		}
	}
	return p, nil
}

// Projects lists every project, oldest first.
func (s *Store) Projects(ctx context.Context) ([]Project, error) {
	ids, err := s.ids(ctx, "SELECT id FROM projects ORDER BY id")
	if err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(ids))
	for _, id := range ids {
		p, err := s.Project(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *Store) ids(ctx context.Context, q string, args ...any) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Groups lists a project's groups in the order they were added.
func (s *Store) Groups(ctx context.Context, project int64) ([]Group, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT kind, key, label, added_by FROM project_groups WHERE project = ? ORDER BY position", project)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.Kind, &g.Key, &g.Label, &g.AddedBy); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Jobs lists a project's jobs with their stages, each in order.
func (s *Store) Jobs(ctx context.Context, project int64) ([]Job, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id, label FROM jobs WHERE project = ? ORDER BY position", project)
	if err != nil {
		return nil, err
	}
	out := []Job{}
	for rows.Next() {
		j := Job{Stages: []Stage{}}
		if err := rows.Scan(&j.ID, &j.Label); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	for i := range out {
		st, err := s.DB.QueryContext(ctx, "SELECT id, label, state, count FROM stages WHERE project = ? AND job = ? ORDER BY position", project, out[i].ID)
		if err != nil {
			return nil, err
		}
		for st.Next() {
			var g Stage
			if err := st.Scan(&g.ID, &g.Label, &g.State, &g.Count); err != nil {
				_ = st.Close()
				return nil, err
			}
			out[i].Stages = append(out[i].Stages, g)
		}
		if err := st.Err(); err != nil {
			_ = st.Close()
			return nil, err
		}
		_ = st.Close()
	}
	return out, nil
}

// Cards lists a project's cards in the order they were first posted.
func (s *Store) Cards(ctx context.Context, project int64) ([]Card, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT job, grp, stage, id, title, kind, class, basis, evidence, decided, recommendation, answers, revision, posted_at, thread_id
		FROM cards WHERE project = ? ORDER BY uid`, project)
	if err != nil {
		return nil, err
	}
	out := []Card{}
	for rows.Next() {
		var c Card
		var evidence, answers string
		var at int64
		if err := rows.Scan(&c.Job, &c.Group, &c.Stage, &c.ID, &c.Title, &c.Kind, &c.Class, &c.Basis, &evidence, &c.Decided, &c.Recommendation, &answers, &c.Revision, &at, &c.Thread); err != nil {
			_ = rows.Close()
			return nil, err
		}
		c.PostedAt = time.UnixMilli(at).UTC()
		if err := json.Unmarshal([]byte(evidence), &c.Evidence); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if answers != "" {
			c.Answers = &Answers{}
			if err := json.Unmarshal([]byte(answers), c.Answers); err != nil {
				_ = rows.Close()
				return nil, err
			}
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	for i := range out {
		if out[i].NeedsYou, err = s.needsYou(ctx, out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// needsYou: the card takes an answer and no message of yours went since it was
// last posted.
func (s *Store) needsYou(ctx context.Context, c Card) (bool, error) {
	if c.Answers == nil {
		return false, nil
	}
	msgs, err := s.Threads.Messages(ctx, c.Thread)
	if err != nil {
		return false, err
	}
	for _, m := range msgs {
		if m.Author == You && m.State != threadstore.Draft && m.State != msgstate.State("") && !m.QueuedAt.Before(c.PostedAt) {
			return false, nil
		}
	}
	return true, nil
}

// Post stores one post in one transaction: a refused or failed part leaves the
// project as it was.
func (s *Store) Post(ctx context.Context, project int64, p Posted, by string) error {
	if _, err := s.Project(ctx, project); err != nil {
		return err
	}
	for _, c := range p.Cards {
		if err := checkCard(c); err != nil {
			return err
		}
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if p.Working != nil {
			if _, err := tx.ExecContext(ctx, "UPDATE projects SET working = ? WHERE id = ?", *p.Working, project); err != nil {
				return err
			}
		}
		if err := addGroups(ctx, tx, project, p.Groups, by); err != nil {
			return err
		}
		for _, j := range p.Jobs {
			if err := postJob(ctx, tx, project, j); err != nil {
				return err
			}
		}
		now := s.Now().UnixMilli()
		for _, c := range p.Cards {
			if err := checkPlace(ctx, tx, project, c); err != nil {
				return err
			}
			if err := s.postCard(ctx, tx, project, c, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func postJob(ctx context.Context, tx *sql.Tx, project int64, j Job) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs(project, id, label, position)
		VALUES (?1, ?2, ?3, (SELECT COUNT(*) FROM jobs WHERE project = ?1))
		ON CONFLICT(project, id) DO UPDATE SET label = excluded.label`, project, j.ID, j.Label); err != nil {
		return err
	}
	for i, g := range j.Stages {
		if _, err := tx.ExecContext(ctx, `INSERT INTO stages(project, job, id, label, position, state, count) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(project, job, id) DO UPDATE SET label = excluded.label, position = excluded.position, state = excluded.state, count = excluded.count`,
			project, j.ID, g.ID, g.Label, i, g.State, g.Count); err != nil {
			return err
		}
	}
	return nil
}

// postCard stores a card with its thread. A card posted again unchanged keeps
// its posted time, so it does not ask again.
func (s *Store) postCard(ctx context.Context, tx *sql.Tx, project int64, c Card, now int64) error {
	th, err := s.Threads.ThreadTx(ctx, tx, Scope(project), "card/"+c.Job+"/"+c.Group+"/"+c.ID)
	if err != nil {
		return err
	}
	if c.Evidence == nil {
		c.Evidence = []Evidence{}
	}
	evidence, err := json.Marshal(c.Evidence)
	if err != nil {
		return err
	}
	answers := ""
	if c.Answers != nil {
		b, err := json.Marshal(c.Answers)
		if err != nil {
			return err
		}
		answers = string(b)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO cards(project, job, grp, stage, id, title, kind, class, basis, evidence, decided, recommendation, answers, revision, posted_at, thread_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project, job, grp, id) DO UPDATE SET stage = excluded.stage, title = excluded.title, kind = excluded.kind, class = excluded.class, basis = excluded.basis,
			evidence = excluded.evidence, decided = excluded.decided, recommendation = excluded.recommendation, answers = excluded.answers,
			revision = excluded.revision, posted_at = excluded.posted_at
		WHERE (stage, title, kind, class, basis, evidence, decided, recommendation, answers, revision) IS NOT
			(excluded.stage, excluded.title, excluded.kind, excluded.class, excluded.basis, excluded.evidence, excluded.decided, excluded.recommendation, excluded.answers, excluded.revision)`,
		project, c.Job, c.Group, c.Stage, c.ID, c.Title, c.Kind, c.Class, c.Basis, string(evidence), c.Decided, c.Recommendation, answers, c.Revision, now, th.ID)
	return err
}

// Refused is a post or an open the store won't take, with the reason.
type Refused struct{ Reason string }

func (r Refused) Error() string { return r.Reason }

// CardOfThread is the card a thread belongs to, ErrNoCard for a project's own.
func (s *Store) CardOfThread(ctx context.Context, thread int64) (Card, error) {
	var c Card
	err := s.DB.QueryRowContext(ctx, "SELECT job, grp, stage, id, title FROM cards WHERE thread_id = ?", thread).Scan(&c.Job, &c.Group, &c.Stage, &c.ID, &c.Title)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNoCard
	}
	return c, err
}

// ErrNoCard is a thread with no card.
var ErrNoCard = errors.New("no such card")

// Answer is what the user picked on a card, kept with the message that carries
// it: Choice for buttons, Rows for a choice per row, Order for an order, and
// Fields for fields.
type Answer struct {
	Kind   string            `json:"kind"`
	Choice string            `json:"choice,omitempty"`
	Rows   map[string]string `json:"rows,omitempty"`
	Order  []string          `json:"order,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}
