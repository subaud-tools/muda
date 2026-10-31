package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

// evidenceKinds are what an evidence entry can name.
var evidenceKinds = []string{"run", "job", "file", "resource", "bill_line"}

// answerKinds are the four kinds of answers a card takes.
var answerKinds = []string{"buttons", "rows", "order", "fields"}

func cardRefused(c Card, field, why string) error {
	return Refused{fmt.Sprintf("card %q: %s: %s", c.ID, field, why)}
}

// checkCard refuses a card muda_post must not take, naming the card and field: a
// finding needs evidence and a measured or suspected basis, and every evidence
// entry names what was read.
func checkCard(c Card) error {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Title) == "" {
		return cardRefused(c, "id and title", "a card needs both")
	}
	if strings.EqualFold(strings.TrimSpace(c.Class), "finding") {
		if len(c.Evidence) == 0 {
			return cardRefused(c, "evidence", "a finding needs at least one evidence entry")
		}
		if c.Basis != "measured" && c.Basis != "suspected" {
			return cardRefused(c, "basis", fmt.Sprintf("a finding needs measured or suspected, got %q", c.Basis))
		}
	}
	for i, e := range c.Evidence {
		if !slices.Contains(evidenceKinds, e.Kind) || strings.TrimSpace(e.Ref) == "" {
			return cardRefused(c, fmt.Sprintf("evidence[%d]", i), "names no run, job, file, resource or bill_line")
		}
	}
	if c.Answers != nil && !slices.Contains(answerKinds, c.Answers.Kind) {
		return cardRefused(c, "answers.kind", fmt.Sprintf("%q: want buttons, rows, order or fields", c.Answers.Kind))
	}
	return nil
}

// checkPlace refuses a card at a job, stage or group its project doesn't have.
func checkPlace(ctx context.Context, tx *sql.Tx, project int64, c Card) error {
	for _, q := range []struct {
		field, query string
		args         []any
	}{
		{"group", "SELECT COUNT(*) FROM project_groups WHERE project = ? AND key = ?", []any{project, c.Group}},
		{"job", "SELECT COUNT(*) FROM jobs WHERE project = ? AND id = ?", []any{project, c.Job}},
		{"stage", "SELECT COUNT(*) FROM stages WHERE project = ? AND job = ? AND id = ?", []any{project, c.Job, c.Stage}},
	} {
		var n int
		if err := tx.QueryRowContext(ctx, q.query, q.args...).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return cardRefused(c, q.field, "this project has none by that name; post it first")
		}
	}
	return nil
}
