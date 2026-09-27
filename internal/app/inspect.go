// This file owns the bound archive-inspection operations. InspectArchive
// records a durable task row and returns it before any work runs; the work
// itself — limits from settings, staged extraction, package inspection,
// success, failure, or cancellation — happens in a goroutine that writes
// every state change to the row first and emits a Wails event hint second.
//
// The row is the truth (ADR 0006) and the event is a hint with no replay: a
// frontend that reloads mid-run re-reads Inspection and sees the same state
// the last event carried, and a frontend that never subscribed still sees
// every state the row holds. Nothing about a preview is persisted this
// phase: previews and failure payloads live in a small in-memory FIFO, so an
// inspection from a previous app run is gone and Inspection says so with
// INSPECTION_NOT_FOUND.
//
// One inspection runs at a time. The busy refusal and the live record are
// decided under the same lock that adds the record, so two callers can never
// both start a run, and a refusal leaves no task row behind.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/archive"
	"github.com/Zendevve/astradew/internal/inspect"
	"github.com/Zendevve/astradew/internal/settings"
	"github.com/Zendevve/astradew/internal/tasks"
)

// OperationInspectArchive names the task row, and the operation field of
// every event payload, for one archive inspection.
const OperationInspectArchive = "archive.inspect"

// The task event names. Each is emitted as a live hint through the injected
// EventSink; the durable task row carries the same state and is what the
// interface re-reads.
const (
	EventTaskStarted   = "task.started"
	EventTaskProgress  = "task.progress"
	EventTaskCompleted = "task.completed"
	EventTaskFailed    = "task.failed"
	EventTaskCancelled = "task.cancelled"
)

// maxInspectionRecords bounds the in-memory inspection FIFO: the four most
// recent inspections keep their preview or failure payload for Inspection.
// Live records are never evicted, so a fifth inspection evicts the oldest
// finished one only.
const maxInspectionRecords = 4

// The inspection view status vocabulary. A live run — and a row that is
// still pending or running — reads as "running"; the finished states reuse
// the task status strings so the row and the view can never disagree about
// spelling.
const (
	viewStatusRunning   = "running"
	viewStatusSucceeded = string(tasks.StatusSucceeded)
	viewStatusFailed    = string(tasks.StatusFailed)
	viewStatusCancelled = string(tasks.StatusCancelled)
)

// The messages the lifecycle writes to the row and carries in its events.
// They are the status line the interface shows, so they are fixed here
// rather than composed at each call site.
const (
	startMessage  = "starting the inspection"
	scanMessage   = "scanning the package"
	cancelMessage = "cancelled by the user"
)

// The six settings keys that fill archive.Limits, spelled exactly as
// internal/settings' registry declares them. The registry stays the one
// definition of a key; these aliases exist so the service names the budgets
// it reads. The registry's guard test pins the defaults to
// archive.DefaultLimits, and the service test proves the key-to-field mapping
// that guard cannot see.
const (
	settingArchiveBytes  = "archive-limit-archive-bytes"
	settingExpandedBytes = "archive-limit-expanded-bytes"
	settingEntries       = "archive-limit-entries"
	settingRatio         = "archive-limit-ratio"
	settingPathDepth     = "archive-limit-path-depth"
	settingPathBytes     = "archive-limit-path-bytes"
)

// EventSink receives task event hints. *application.EventManager satisfies
// it, and a nil sink (tests, headless runs) is valid by design: every
// emission is best-effort and no correctness path depends on one (ADR 0006).
type EventSink interface {
	Emit(name string, data ...any) bool
}

// TaskEvent is the payload every task event carries, in the repository's
// camelCase wire style. Current and Total are the progress numbers of the
// phase that emitted the event; the scanning phase reports 0/0, the
// indeterminate shape a reloaded frontend renders from the row too.
type TaskEvent struct {
	TaskID    string `json:"taskId"`
	Operation string `json:"operation"`
	Current   int64  `json:"current"`
	Total     int64  `json:"total"`
	Message   string `json:"message"`
}

// Preview is the wire form of one finished inspection: the archive's
// identity, its digests, and everything internal/inspect derived from the
// staged tree. The embedded inspect.Preview inlines its own JSON keys
// (units, findings, duplicates, installable).
type Preview struct {
	SourcePath    string `json:"sourcePath"`
	OriginalName  string `json:"originalName"`
	SizeBytes     int64  `json:"sizeBytes"`
	ArchiveSHA256 string `json:"archiveSha256"`
	PackageSHA256 string `json:"packageSha256"`
	inspect.Preview
}

// InspectionFailure is the typed failure of one inspection. Code is the
// apperror code the row's outcome carries; Message is its stable sentence
// and Details names the offending entry, path, or reason.
type InspectionFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details"`
}

// InspectionView is the live state of one inspection: the durable row, the
// view status, the preview when it succeeded, and the failure when it
// failed. Both payload fields are nil unless their status applies.
type InspectionView struct {
	Task    tasks.Task         `json:"task"`
	Status  string             `json:"status"`
	Preview *Preview           `json:"preview"`
	Failure *InspectionFailure `json:"failure"`
}

// inspectionRecord is one in-memory inspection: the cancel handle and
// context of a live run, plus the preview or failure payload once it ends.
// Every field is read and written under ApplicationService.mu.
type inspectionRecord struct {
	taskID string
	ctx    context.Context
	cancel context.CancelFunc
	// done closes when the run goroutine has written its terminal state,
	// emitted its final event, and removed its stage: CancelTask waits on it
	// so cancellation returns only after the cleanup finished.
	done chan struct{}
	// live is true from the moment the row exists until the run writes its
	// terminal state. CancelTask refuses a record that is not live.
	live bool
	// cancelRequested is set by CancelTask under the lock; the run decides
	// its terminal state by this flag, so a cancellation that arrives while
	// the work is finishing still lands as cancelled rather than racing the
	// success write.
	cancelRequested bool
	preview         *Preview
	failure         *InspectionFailure
}

// SetEventSink injects the sink task events are emitted through. main.go
// calls it once the Wails application exists; a service that never gets one
// runs identically, minus the hints.
func (s *ApplicationService) SetEventSink(sink EventSink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sink = sink
}

// InspectArchive records a task row for the chosen archive, starts the
// inspection, and returns the row immediately. Only one inspection runs at a
// time: a second call while one is live refuses with INSPECTION_BUSY and
// leaves no row behind, as does a service whose store or data layout is
// unusable. The row starts pending; the run moves it to running and then to
// succeeded, failed, or cancelled.
func (s *ApplicationService) InspectArchive(path string) (tasks.Task, error) {
	if s.db == nil {
		return tasks.Task{}, apperror.New(apperror.CodeStoreOpenFailed, "task store unavailable: no open database")
	}
	if s.paths.Temp == "" {
		return tasks.Task{}, apperror.NewRecoverable(apperror.CodeInspectionFailed,
			"the staging area is unavailable",
			"the application data layout has no temp root, so an inspection has nowhere to stage its extraction")
	}

	s.mu.Lock()
	if live := s.liveInspectionLocked(); live != nil {
		s.mu.Unlock()
		return tasks.Task{}, apperror.NewRecoverable(apperror.CodeInspectionBusy,
			"an inspection is already running",
			fmt.Sprintf("task %s is running; wait for it to finish or cancel it before inspecting another archive", live.taskID))
	}
	row, err := tasks.New(s.db.DB()).Create(context.Background(), OperationInspectArchive)
	if err != nil {
		// Nothing was created, so the refusal leaves no row behind.
		s.mu.Unlock()
		return tasks.Task{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	record := &inspectionRecord{taskID: row.ID, ctx: ctx, cancel: cancel, done: make(chan struct{}), live: true}
	s.addRecordLocked(record)
	s.mu.Unlock()

	go s.runInspection(record, path)
	return row, nil
}

// Inspection returns the live state of one inspection. The status comes from
// the durable row — the truth a reloaded frontend re-reads — and the preview
// or failure payload comes from the in-memory record, because neither is
// persisted this phase. A task id this process holds no record for reports
// INSPECTION_NOT_FOUND: an unknown id, a non-inspection task, an evicted
// record, and a row left by a previous app run are all the same answer, and
// the frontend renders it as the expired state.
func (s *ApplicationService) Inspection(taskID string) (InspectionView, error) {
	if s.db == nil {
		return InspectionView{}, apperror.New(apperror.CodeStoreOpenFailed, "task store unavailable: no open database")
	}
	store := tasks.New(s.db.DB())
	s.mu.Lock()
	record := s.findRecordLocked(taskID)
	if record == nil {
		s.mu.Unlock()
		return InspectionView{}, s.missingInspection(store, taskID)
	}
	// The row read shares the lock with the record state so the view can
	// never mix a finished payload with a pre-terminal row, or the reverse.
	row, err := store.Get(context.Background(), taskID)
	if err != nil {
		s.mu.Unlock()
		return InspectionView{}, err
	}
	view := InspectionView{Task: row, Status: viewStatus(row.Status)}
	switch row.Status {
	case tasks.StatusSucceeded:
		view.Preview = record.preview
	case tasks.StatusFailed:
		view.Failure = record.failure
	}
	s.mu.Unlock()
	return view, nil
}

// CancelTask cancels a live inspection: it cancels the run's context, waits
// for the goroutine to stop its work, mark the row cancelled, and remove its
// stage, and then returns the row. An unknown id reports TASK_NOT_FOUND; a
// row that is not live in this process — finished, already cancelled, or a
// stale running row from a previous app run — reports TASK_NOT_CANCELLABLE,
// because there is nothing here to stop.
func (s *ApplicationService) CancelTask(taskID string) (tasks.Task, error) {
	if s.db == nil {
		return tasks.Task{}, apperror.New(apperror.CodeStoreOpenFailed, "task store unavailable: no open database")
	}
	store := tasks.New(s.db.DB())

	s.mu.Lock()
	record := s.findRecordLocked(taskID)
	if record == nil || !record.live {
		s.mu.Unlock()
		return tasks.Task{}, notCancellable(store, taskID)
	}
	record.cancelRequested = true
	cancel := record.cancel
	done := record.done
	s.mu.Unlock()

	cancel()
	// The wait is outside the lock on purpose: the run needs the lock to
	// write its terminal state, which is what closes done.
	<-done
	return store.Get(context.Background(), taskID)
}

// runInspection is the work behind one InspectArchive call. The row exists
// before the goroutine starts, and every state after the start is written to
// the row before the matching event is emitted, so an event can never
// describe a state the row does not hold.
func (s *ApplicationService) runInspection(record *inspectionRecord, path string) {
	// Closes last: every defer below (the stage removal) runs first, so a
	// CancelTask waiter is released only once the cleanup is done.
	defer close(record.done)

	ctx := context.Background()
	store := tasks.New(s.db.DB())
	s.emit(EventTaskStarted, TaskEvent{TaskID: record.taskID, Operation: OperationInspectArchive, Message: startMessage})

	// The limits come from the six settings keys, and a read failure fails
	// the row with the read's own typed code: falling back to the built-in
	// defaults would silently ignore a limit the user lowered.
	limits, err := s.archiveLimits(ctx)
	if err != nil {
		s.finish(record, nil, failurePayload(err), "")
		return
	}
	if err := store.UpdateProgress(ctx, record.taskID, 0, 0, startMessage); err != nil {
		s.finish(record, nil, failurePayload(err), "")
		return
	}

	stage := archive.StageDir(s.paths.Temp, record.taskID)
	// The stage belongs to this run alone and leaves the disk on every path:
	// success, failure, cancellation, and a panic inside the extraction.
	defer func() { _ = os.RemoveAll(stage) }()

	onProgress := func(progress archive.Progress) {
		// A progress write that fails is not retried: the row keeps the last
		// state that did land, and the terminal write below is what the
		// interface branches on.
		_ = store.UpdateProgress(ctx, record.taskID, progress.Current, progress.Total, progress.Message)
		s.emit(EventTaskProgress, TaskEvent{
			TaskID:    record.taskID,
			Operation: OperationInspectArchive,
			Current:   progress.Current,
			Total:     progress.Total,
			Message:   progress.Message,
		})
	}
	result, err := archive.Extract(record.ctx, path, stage, limits, onProgress)
	if err != nil {
		// Extract removed its own partial stage; the deferred removal above
		// covers anything it left.
		s.finish(record, nil, failurePayload(err), "")
		return
	}

	// Scanning is indeterminate — internal/inspect streams nothing — so the
	// row and the event both carry 0/0 with the phase message: a reloaded
	// frontend renders the same state the live event showed.
	_ = store.UpdateProgress(ctx, record.taskID, 0, 0, scanMessage)
	s.emit(EventTaskProgress, TaskEvent{TaskID: record.taskID, Operation: OperationInspectArchive, Message: scanMessage})

	preview := &Preview{
		SourcePath:    path,
		OriginalName:  filepath.Base(path),
		SizeBytes:     result.SizeBytes,
		ArchiveSHA256: result.ArchiveSHA256,
		PackageSHA256: result.PackageSHA256,
		Preview:       inspect.Inspect(os.DirFS(stage), inspect.Options{CaseInsensitivePaths: caseInsensitivePaths()}),
	}
	s.finish(record, preview, nil, summaryOf(preview))
}

// finish writes the terminal state of a run and emits its final event. The
// decision between the run's own outcome and cancellation, the row write,
// and the record's transition all happen under the service lock, so a
// CancelTask that wins the lock first always lands as cancelled and one that
// arrives after always reports TASK_NOT_CANCELLABLE — a cancelled row can
// never overwrite a finished one, and the reverse cannot happen either.
func (s *ApplicationService) finish(record *inspectionRecord, preview *Preview, failure *InspectionFailure, summary string) {
	ctx := context.Background()
	store := tasks.New(s.db.DB())

	s.mu.Lock()
	cancelled := record.cancelRequested
	record.live = false
	if !cancelled {
		record.preview = preview
		record.failure = failure
	}
	switch {
	case cancelled:
		// Best effort: a write that fails leaves the row running, which is
		// the honest record of a terminal write that never landed.
		_ = store.Cancel(ctx, record.taskID, cancelMessage)
	case failure != nil:
		_ = store.Fail(ctx, record.taskID, apperror.Code(failure.Code), failure.Message)
	default:
		_ = store.Succeed(ctx, record.taskID, summary)
	}
	s.mu.Unlock()

	switch {
	case cancelled:
		s.emit(EventTaskCancelled, TaskEvent{TaskID: record.taskID, Operation: OperationInspectArchive, Message: cancelMessage})
	case failure != nil:
		s.emit(EventTaskFailed, TaskEvent{TaskID: record.taskID, Operation: OperationInspectArchive, Message: failure.Message})
	default:
		s.emit(EventTaskCompleted, TaskEvent{TaskID: record.taskID, Operation: OperationInspectArchive, Message: summary})
	}
}

// summaryOf renders the row's result summary: what the preview holds, in
// words, with each count agreeing with its noun.
func summaryOf(preview *Preview) string {
	return fmt.Sprintf("%s, %s", plural(len(preview.Units), "unit"), plural(len(preview.Findings), "finding"))
}

// plural renders one count with its noun in the right number.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// archiveLimits reads the six configured budgets into archive.Limits. Every
// field comes from its settings key; a read or decode failure is returned
// rather than papered over with the built-in default.
func (s *ApplicationService) archiveLimits(ctx context.Context) (archive.Limits, error) {
	svc := settings.New(s.db.DB())
	read := func(key string) (int, error) {
		raw, err := svc.Get(ctx, key)
		if err != nil {
			return 0, err
		}
		value, ok := raw.(int)
		if !ok {
			return 0, apperror.NewRecoverable(apperror.CodeInspectionFailed,
				"the archive limits could not be read",
				fmt.Sprintf("setting %q decoded to %T, want an integer", key, raw))
		}
		return value, nil
	}
	archiveBytes, err := read(settingArchiveBytes)
	if err != nil {
		return archive.Limits{}, err
	}
	expandedBytes, err := read(settingExpandedBytes)
	if err != nil {
		return archive.Limits{}, err
	}
	entries, err := read(settingEntries)
	if err != nil {
		return archive.Limits{}, err
	}
	ratio, err := read(settingRatio)
	if err != nil {
		return archive.Limits{}, err
	}
	pathDepth, err := read(settingPathDepth)
	if err != nil {
		return archive.Limits{}, err
	}
	pathBytes, err := read(settingPathBytes)
	if err != nil {
		return archive.Limits{}, err
	}
	return archive.Limits{
		MaxArchiveBytes:  int64(archiveBytes),
		MaxExpandedBytes: int64(expandedBytes),
		MaxEntries:       entries,
		MaxRatio:         ratio,
		MaxPathDepth:     pathDepth,
		MaxPathBytes:     pathBytes,
	}, nil
}

// failurePayload renders the typed failure of a run. An apperror keeps its
// code, message, and details verbatim; anything else — a store write that
// failed, a context that ended — is reported as INSPECTION_FAILED with the
// error text in details, never dropped.
func failurePayload(err error) *InspectionFailure {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		return &InspectionFailure{Code: string(appErr.Code), Message: appErr.Message, Details: appErr.Details}
	}
	return &InspectionFailure{
		Code:    string(apperror.CodeInspectionFailed),
		Message: "the inspection failed",
		Details: err.Error(),
	}
}

// missingInspection reports INSPECTION_NOT_FOUND, naming what is actually
// known about the id so the interface can show the expired state honestly.
func (s *ApplicationService) missingInspection(store *tasks.Store, taskID string) error {
	row, err := store.Get(context.Background(), taskID)
	if err != nil {
		return apperror.NewRecoverable(apperror.CodeInspectionNotFound,
			fmt.Sprintf("no inspection %q", taskID),
			fmt.Sprintf("inspection %q: this process has no record for the id, and no task row carries it", taskID))
	}
	if row.Operation != OperationInspectArchive {
		return apperror.NewRecoverable(apperror.CodeInspectionNotFound,
			fmt.Sprintf("no inspection %q", taskID),
			fmt.Sprintf("task %q is a %s task, not an inspection", taskID, row.Operation))
	}
	return apperror.NewRecoverable(apperror.CodeInspectionNotFound,
		fmt.Sprintf("no inspection %q", taskID),
		fmt.Sprintf("inspection %q is no longer in memory (%s): nothing about a preview is persisted, so a result from a previous app run is gone", taskID, row.Status))
}

// notCancellable reports TASK_NOT_FOUND for an id no row carries and
// TASK_NOT_CANCELLABLE for a row that is not live in this process.
func notCancellable(store *tasks.Store, taskID string) error {
	row, err := store.Get(context.Background(), taskID)
	if err != nil {
		return err
	}
	return apperror.NewRecoverable(apperror.CodeTaskNotCancellable,
		fmt.Sprintf("task %q cannot be cancelled", taskID),
		fmt.Sprintf("task %q is %s; only a running inspection in this process can be cancelled", taskID, row.Status))
}

// viewStatus maps a row status onto the inspection view vocabulary: a row
// that has not finished reads as running.
func viewStatus(status tasks.Status) string {
	switch status {
	case tasks.StatusSucceeded:
		return viewStatusSucceeded
	case tasks.StatusFailed:
		return viewStatusFailed
	case tasks.StatusCancelled:
		return viewStatusCancelled
	default:
		return viewStatusRunning
	}
}

// caseInsensitivePaths follows the host platform, where the extracted names
// actually landed: Windows and macOS fold case, every other host does not.
// It mirrors the comparison internal/store applies to paths.
func caseInsensitivePaths() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}

// emit hands one event to the sink. The sink is read under the lock but
// called outside it, so a sink that calls back into the service cannot
// deadlock, and a nil sink — the headless case — is simply no sink at all.
func (s *ApplicationService) emit(name string, event TaskEvent) {
	s.mu.Lock()
	sink := s.sink
	s.mu.Unlock()
	if sink == nil {
		return
	}
	sink.Emit(name, event)
}

// liveInspectionLocked returns the running inspection, if any. Only one run
// can be live at a time, so the first live record is the only live record.
func (s *ApplicationService) liveInspectionLocked() *inspectionRecord {
	for _, record := range s.inspections {
		if record.live {
			return record
		}
	}
	return nil
}

// findRecordLocked returns the record for taskID, if this process holds one.
func (s *ApplicationService) findRecordLocked(taskID string) *inspectionRecord {
	for _, record := range s.inspections {
		if record.taskID == taskID {
			return record
		}
	}
	return nil
}

// addRecordLocked appends a record and evicts the oldest finished one while
// more than maxInspectionRecords are held. A live record is never evicted:
// the interface must be able to cancel what is running.
func (s *ApplicationService) addRecordLocked(record *inspectionRecord) {
	s.inspections = append(s.inspections, record)
	for len(s.inspections) > maxInspectionRecords {
		evict := -1
		for i, held := range s.inspections {
			if !held.live {
				evict = i
				break
			}
		}
		if evict < 0 {
			return
		}
		s.inspections = append(s.inspections[:evict], s.inspections[evict+1:]...)
	}
}
