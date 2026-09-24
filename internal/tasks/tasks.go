// Package tasks keeps Astradew's durable task records in SQLite.
//
// A Task row is the source of truth for a long operation (ADR 0006):
// Wails events are live hints that are never replayed and do not survive
// a frontend reload or an application restart, so every state change lands
// here and the frontend re-reads rows when it mounts. This package emits
// no events at all — and if any caller ever does emit one, no correctness
// path may depend on it; the re-read from SQLite is always the authority.
//
// Statuses are typed constants, never bare strings at call sites:
// pending → running → succeeded | failed. Fail persists the typed error
// CODE in outcome so the failure mode survives restarts as data.
package tasks

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/Zendevve/astradew/internal/apperror"
)

// Status is the lifecycle state of a Task record.
type Status string

const (
	// StatusPending marks a record that was created but never progressed.
	StatusPending Status = "pending"
	// StatusRunning marks a record with reported progress that has not
	// finished yet.
	StatusRunning Status = "running"
	// StatusSucceeded marks a finished record carrying a result summary
	// in outcome.
	StatusSucceeded Status = "succeeded"
	// StatusFailed marks a finished record carrying the apperror code in
	// outcome.
	StatusFailed Status = "failed"
)

// DefaultListLimit bounds List when the caller passes no positive limit.
const DefaultListLimit = 20

// Task is one durable operation record. The JSON keys are the contract
// the frontend binds to. Outcome is nil until the task finishes: then it
// holds the result summary (succeeded) or the apperror code (failed).
type Task struct {
	ID        string  `json:"id"`
	Operation string  `json:"operation"`
	Status    Status  `json:"status"`
	Current   int64   `json:"current"`
	Total     int64   `json:"total"`
	Message   string  `json:"message"`
	Outcome   *string `json:"outcome"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

// Store reads and writes task rows on db. It never opens or migrates:
// the caller passes the handle store.Open already migrated.
type Store struct {
	db *sql.DB
}

// New returns a Store over db.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// columns lists the tasks table columns in scan order. It matches
// migrations/0001_init.sql exactly; no new migration is needed.
const columns = "id, operation, status, current, total, message, outcome, created_at, updated_at"

// Create inserts a pending record for operation and returns it. The id is
// a UTC timestamp plus crypto/rand hex, so concurrent creators never
// collide without any new dependency.
func (s *Store) Create(ctx context.Context, operation string) (Task, error) {
	if operation == "" {
		return Task{}, fmt.Errorf("tasks: operation must not be empty")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	task := Task{
		ID:        generateID(),
		Operation: operation,
		Status:    StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	const query = `INSERT INTO tasks (id, operation, status, current, total, message, outcome, created_at, updated_at)
		VALUES (?, ?, ?, 0, 0, '', NULL, ?, ?)`
	if _, err := s.db.ExecContext(ctx, query, task.ID, task.Operation, string(task.Status), task.CreatedAt, task.UpdatedAt); err != nil {
		return Task{}, fmt.Errorf("tasks: create %q: %w", operation, err)
	}
	return task, nil
}

// UpdateProgress records current/total/message and moves the record to
// running. It reports TASK_NOT_FOUND for an unknown id.
func (s *Store) UpdateProgress(ctx context.Context, id string, current, total int64, message string) error {
	const query = `UPDATE tasks SET status = ?, current = ?, total = ?, message = ?, updated_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, string(StatusRunning), current, total, message, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("tasks: update progress %q: %w", id, err)
	}
	return checkUpdated(res, id, "update progress")
}

// Succeed finishes the record with a result summary in outcome. An empty
// outcome stores NULL, keeping "no summary" distinct from an empty one.
// It reports TASK_NOT_FOUND for an unknown id.
func (s *Store) Succeed(ctx context.Context, id string, outcome string) error {
	const query = `UPDATE tasks SET status = ?, outcome = ?, updated_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, string(StatusSucceeded), nullIfEmpty(outcome), time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("tasks: succeed %q: %w", id, err)
	}
	return checkUpdated(res, id, "succeed")
}

// Fail finishes the record with the typed error CODE durably in outcome
// and the human message in message. The code is data a later process
// re-reads — never a transient event. It reports TASK_NOT_FOUND for an
// unknown id.
func (s *Store) Fail(ctx context.Context, id string, code apperror.Code, message string) error {
	const query = `UPDATE tasks SET status = ?, outcome = ?, message = ?, updated_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, string(StatusFailed), string(code), message, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("tasks: fail %q: %w", id, err)
	}
	return checkUpdated(res, id, "fail")
}

// Get re-reads the record for id. An unknown id reports TASK_NOT_FOUND.
func (s *Store) Get(ctx context.Context, id string) (Task, error) {
	task, err := scanTask(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM tasks WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return Task{}, notFound(id)
	}
	if err != nil {
		return Task{}, fmt.Errorf("tasks: get %q: %w", id, err)
	}
	return task, nil
}

// List returns records newest-first. A non-positive limit falls back to
// DefaultListLimit. Ties on created_at break on rowid, so insertion order
// decides when two records share a timestamp.
func (s *Store) List(ctx context.Context, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM tasks ORDER BY created_at DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("tasks: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("tasks: list: %w", err)
		}
		out = append(out, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tasks: list: %w", err)
	}
	return out, nil
}

// scanner covers *sql.Row and *sql.Rows, the two sources rows come from.
type scanner interface {
	Scan(dest ...any) error
}

// scanTask reads one row in columns order.
func scanTask(row scanner) (Task, error) {
	var task Task
	var status string
	var outcome sql.NullString
	if err := row.Scan(&task.ID, &task.Operation, &status, &task.Current, &task.Total, &task.Message, &outcome, &task.CreatedAt, &task.UpdatedAt); err != nil {
		return Task{}, err
	}
	task.Status = Status(status)
	if outcome.Valid {
		task.Outcome = &outcome.String
	}
	return task, nil
}

// checkUpdated maps an UPDATE that touched no row to TASK_NOT_FOUND.
func checkUpdated(res sql.Result, id, op string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("tasks: %s %q: %w", op, id, err)
	}
	if n == 0 {
		return notFound(id)
	}
	return nil
}

// notFound reports an unknown task id. Callers branch on the code, never
// on this text.
func notFound(id string) error {
	return apperror.New(apperror.CodeTaskNotFound, fmt.Sprintf("task not found: %q", id))
}

// nullIfEmpty stores an empty outcome as NULL.
func nullIfEmpty(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// generateID builds a timestamp plus 8 crypto/rand bytes as hex: ordered
// enough for humans, unique enough for concurrent creators. A rand failure
// (practically impossible) falls back to nanotime instead of panicking.
func generateID() string {
	now := time.Now().UTC()
	var entropy [8]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return fmt.Sprintf("task-%s-%d", now.Format("20060102-150405.000"), now.UnixNano())
	}
	return fmt.Sprintf("task-%s-%s", now.Format("20060102-150405.000"), hex.EncodeToString(entropy[:]))
}
