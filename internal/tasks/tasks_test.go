package tasks

import (
	"context"
	"errors"
	"testing"

	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/approot"
	"github.com/Zendevve/astradew/internal/store"
)

// openStore opens a real migrated database under a temp root. The store is
// real so reopen tests prove rows survive a process restart.
func openStore(t *testing.T, paths approot.Paths) *store.Store {
	t.Helper()
	db, err := store.Open(paths)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// reopen closes db and opens the same file again, proving the durable
// record outlives the handle that wrote it.
func reopen(t *testing.T, paths approot.Paths, db *store.Store) *store.Store {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return openStore(t, paths)
}

func taskNotFoundCode(t *testing.T, err error) {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error type = %T (%v), want *apperror.AppError", err, err)
	}
	if appErr.Code != apperror.CodeTaskNotFound {
		t.Fatalf("error code = %q, want %q", appErr.Code, apperror.CodeTaskNotFound)
	}
}

// The full lifecycle — create, progress, succeed — must re-read with the
// same status, outcome, and progress after the database is closed and
// reopened.
func TestLifecycleSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openStore(t, paths)
	created, err := New(db.DB()).Create(ctx, "startup")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create() returned an empty id")
	}
	if created.Status != StatusPending {
		t.Fatalf("Create() status = %q, want %q", created.Status, StatusPending)
	}
	if err := New(db.DB()).UpdateProgress(ctx, created.ID, 1, 2, "root resolved"); err != nil {
		t.Fatalf("UpdateProgress() error = %v", err)
	}
	if err := New(db.DB()).Succeed(ctx, created.ID, "startup complete at schema version 1"); err != nil {
		t.Fatalf("Succeed() error = %v", err)
	}

	got, err := New(reopen(t, paths, db).DB()).Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() after reopen error = %v", err)
	}
	if got.Operation != "startup" || got.Status != StatusSucceeded {
		t.Fatalf("Get() after reopen = %+v, want operation startup status succeeded", got)
	}
	if got.Current != 1 || got.Total != 2 || got.Message != "root resolved" {
		t.Fatalf("Get() after reopen progress = %d/%d %q, want 1/2 %q", got.Current, got.Total, got.Message, "root resolved")
	}
	if got.Outcome == nil || *got.Outcome != "startup complete at schema version 1" {
		t.Fatalf("Get() after reopen outcome = %v, want the succeed summary", got.Outcome)
	}
	if got.CreatedAt != created.CreatedAt {
		t.Fatalf("Get() after reopen createdAt = %q, want %q", got.CreatedAt, created.CreatedAt)
	}
}

// Fail must persist the typed error CODE in outcome, and the code must
// still be there after a reopen: the failure mode is data, not a
// transient event.
func TestFailRecordsCodeDurably(t *testing.T) {
	ctx := context.Background()
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openStore(t, paths)
	created, err := New(db.DB()).Create(ctx, "startup")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := New(db.DB()).Fail(ctx, created.ID, apperror.CodeProbeFailure, "probe blew up"); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}

	got, err := New(reopen(t, paths, db).DB()).Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() after reopen error = %v", err)
	}
	if got.Status != StatusFailed {
		t.Fatalf("Get() after reopen status = %q, want %q", got.Status, StatusFailed)
	}
	if got.Outcome == nil || *got.Outcome != string(apperror.CodeProbeFailure) {
		t.Fatalf("Get() after reopen outcome = %v, want code %q", got.Outcome, apperror.CodeProbeFailure)
	}
	if got.Message != "probe blew up" {
		t.Fatalf("Get() after reopen message = %q, want %q", got.Message, "probe blew up")
	}
}

// Reads and writes against an unknown id report TASK_NOT_FOUND, never a
// nil row or a bare sql error.
func TestUnknownIDReportsTaskNotFound(t *testing.T) {
	ctx := context.Background()
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	ts := New(openStore(t, paths).DB())

	if _, err := ts.Get(ctx, "task-does-not-exist"); err == nil {
		t.Fatal("Get() unknown id error = nil, want TASK_NOT_FOUND")
	} else {
		taskNotFoundCode(t, err)
	}
	if err := ts.UpdateProgress(ctx, "task-does-not-exist", 1, 1, "x"); err == nil {
		t.Fatal("UpdateProgress() unknown id error = nil, want TASK_NOT_FOUND")
	} else {
		taskNotFoundCode(t, err)
	}
	if err := ts.Succeed(ctx, "task-does-not-exist", "x"); err == nil {
		t.Fatal("Succeed() unknown id error = nil, want TASK_NOT_FOUND")
	} else {
		taskNotFoundCode(t, err)
	}
	if err := ts.Fail(ctx, "task-does-not-exist", apperror.CodeProbeFailure, "x"); err == nil {
		t.Fatal("Fail() unknown id error = nil, want TASK_NOT_FOUND")
	} else {
		taskNotFoundCode(t, err)
	}
}

// List returns records newest-first and honours the limit; a
// non-positive limit falls back to the sane default.
func TestListOrderingAndLimit(t *testing.T) {
	ctx := context.Background()
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	ts := New(openStore(t, paths).DB())
	for _, op := range []string{"op-a", "op-b", "op-c"} {
		if _, err := ts.Create(ctx, op); err != nil {
			t.Fatalf("Create(%q) error = %v", op, err)
		}
	}

	all, err := ts.List(ctx, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 3 || all[0].Operation != "op-c" || all[1].Operation != "op-b" || all[2].Operation != "op-a" {
		t.Fatalf("List() order = %v, want [op-c op-b op-a]", operations(all))
	}

	limited, err := ts.List(ctx, 2)
	if err != nil {
		t.Fatalf("List(2) error = %v", err)
	}
	if len(limited) != 2 || limited[0].Operation != "op-c" || limited[1].Operation != "op-b" {
		t.Fatalf("List(2) = %v, want [op-c op-b]", operations(limited))
	}
}

// An empty operation creates nothing: a record without a name is not a
// record.
func TestCreateRejectsEmptyOperation(t *testing.T) {
	ctx := context.Background()
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	if _, err := New(openStore(t, paths).DB()).Create(ctx, ""); err == nil {
		t.Fatal("Create() empty operation error = nil, want an error")
	}
}

func operations(tasks []Task) []string {
	out := make([]string, len(tasks))
	for i, task := range tasks {
		out[i] = task.Operation
	}
	return out
}
