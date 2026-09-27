// This file holds the service-level tests of archive inspection: the durable
// task row, the staged extraction, the preview, the failure payload, the
// event hints, cancellation, and the in-memory record FIFO. Every test runs
// over a real temp data root, a real store, and real archives written to disk
// by internal/archive's own fixture builder, so the seam under test is the
// one main.go binds.
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/approot"
	"github.com/Zendevve/astradew/internal/archive"
	"github.com/Zendevve/astradew/internal/inspect"
	"github.com/Zendevve/astradew/internal/tasks"
)

// newInspectionService builds a service over a real temp data root and a real
// store, the way main.go does before it injects the sink, and returns the
// resolved layout so a test can look for the stages an inspection leaves
// behind.
func newInspectionService(t *testing.T) (*ApplicationService, approot.Paths) {
	t.Helper()
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	return NewWithPathsAndStore("Astradew", "1.4.2", paths, openTestStore(t, paths)), paths
}

// writeArchive writes one fixture into its own temp directory under name and
// returns the archive's path.
func writeArchive(t *testing.T, name string, fixture *archive.Fixture) string {
	t.Helper()
	filePath := filepath.Join(t.TempDir(), name)
	if err := fixture.WriteTo(filePath); err != nil {
		t.Fatalf("writing fixture %q: %v", name, err)
	}
	return filePath
}

// slowFixture is a package big enough that a run is observably in flight: its
// payload cannot be hashed, written, and scanned between two service calls,
// which is what lets a test observe the running row and cancel mid-extraction.
// The bodies are stored rather than deflated and generated once, so the
// fixture costs one write of its payload rather than a compression pass.
func slowFixture() *archive.Fixture {
	body := make([]byte, 2<<20)
	if _, err := rand.Read(body); err != nil {
		panic("archive fixture: reading entropy: " + err.Error())
	}
	fixture := archive.NewFixture().Mod("BulkMod", "Astradew.BulkMod")
	for i := 0; i < 16; i++ {
		fixture.Stored(path.Join("BulkMod", "assets", fmt.Sprintf("blob-%02d.bin", i)), string(body))
	}
	return fixture
}

// waitForInspection polls Inspection until the run leaves the running state
// and returns the settled view.
func waitForInspection(t *testing.T, svc *ApplicationService, taskID string) InspectionView {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		view, err := svc.Inspection(taskID)
		if err != nil {
			t.Fatalf("Inspection(%q) error = %v", taskID, err)
		}
		if view.Status != viewStatusRunning {
			return view
		}
		if time.Now().After(deadline) {
			t.Fatalf("inspection %q still running after 60s", taskID)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitForRunningView polls Inspection until the live run reads as running: a
// reloaded frontend sees the row, so the view must report the in-flight state
// with no payload attached.
func waitForRunningView(t *testing.T, svc *ApplicationService, taskID string) InspectionView {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		view, err := svc.Inspection(taskID)
		if err != nil {
			t.Fatalf("Inspection(%q) error = %v", taskID, err)
		}
		if view.Status == viewStatusRunning {
			return view
		}
		if time.Now().After(deadline) {
			t.Fatalf("inspection %q read as %q for 60s, want %q while the run is live", taskID, view.Status, viewStatusRunning)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitForTaskStatus polls the durable row until it reaches status, proving a
// transition a test cannot see any other way.
func waitForTaskStatus(t *testing.T, svc *ApplicationService, taskID string, status tasks.Status) tasks.Task {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		row, err := svc.Task(taskID)
		if err != nil {
			t.Fatalf("Task(%q) error = %v", taskID, err)
		}
		if row.Status == status {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %q status = %q after 60s, want %q", taskID, row.Status, status)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitForStage polls until the run's stage directory exists — the extraction
// phase has started writing into it — and fails if the run finished first.
func waitForStage(t *testing.T, stage string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := os.Stat(stage); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("stage %q never appeared; the run finished before extraction could be observed", stage)
		}
		time.Sleep(time.Millisecond)
	}
}

// stageGone asserts the run's stage is removed: the inspection owns it, no
// path may leak it, and the removal is part of the run's cleanup. The wait is
// bounded because a success writes its terminal row first and deletes the
// stage afterwards, so the row can report success a moment before the disk
// catches up; a cancellation, which waits for the cleanup, finds it already
// gone.
func stageGone(t *testing.T, paths approot.Paths, taskID string) {
	t.Helper()
	stage := archive.StageDir(paths.Temp, taskID)
	deadline := time.Now().Add(60 * time.Second)
	for {
		_, err := os.Stat(stage)
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		if err != nil {
			t.Fatalf("stat of stage %q: %v", stage, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("stage %q still exists after 60s, want it removed", stage)
		}
		time.Sleep(time.Millisecond)
	}
}

// wantCode asserts err carries the given typed code and returns the error's
// details for callers that assert on them.
func wantCode(t *testing.T, err error, code apperror.Code) *apperror.AppError {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %v (%T), want *apperror.AppError with code %s", err, err, code)
	}
	if appErr.Code != code {
		t.Fatalf("error code = %q, want %q (%v)", appErr.Code, code, err)
	}
	return appErr
}

// recordedEvent is one event as the sink received it: the payload struct and
// the JSON shape the frontend decodes, because the keys are the contract.
type recordedEvent struct {
	name    string
	payload TaskEvent
	fields  map[string]any
}

type recordingSink struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (r *recordingSink) Emit(name string, data ...any) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	event := recordedEvent{name: name, fields: map[string]any{}}
	if len(data) > 0 {
		event.payload, _ = data[0].(TaskEvent)
		if encoded, err := json.Marshal(data[0]); err == nil {
			_ = json.Unmarshal(encoded, &event.fields)
		}
	}
	r.events = append(r.events, event)
	return true
}

func (r *recordingSink) snapshot() []recordedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedEvent(nil), r.events...)
}

func (r *recordingSink) byName(name string) []recordedEvent {
	var out []recordedEvent
	for _, event := range r.snapshot() {
		if event.name == name {
			out = append(out, event)
		}
	}
	return out
}

func (r *recordingSink) names() []string {
	var out []string
	for _, event := range r.snapshot() {
		out = append(out, event.name)
	}
	return out
}

// InspectArchive on a real archive must report the fixture's units and digests
// through Inspection, end the row succeeded with a summary, and leave no stage
// behind.
func TestInspectArchiveReportsThePreviewAndLeavesNoStage(t *testing.T) {
	svc, paths := newInspectionService(t)
	srcPath := writeArchive(t, "Stardew Valley Expanded-1.2.3.zip", archive.MultiMod())

	row, err := svc.InspectArchive(srcPath)
	if err != nil {
		t.Fatalf("InspectArchive() error = %v", err)
	}
	if row.ID == "" {
		t.Fatal("InspectArchive() returned a row with no id")
	}
	if row.Operation != OperationInspectArchive {
		t.Fatalf("row operation = %q, want %q", row.Operation, OperationInspectArchive)
	}
	if row.Status != tasks.StatusPending && row.Status != tasks.StatusRunning {
		t.Fatalf("returned row status = %q, want pending or running: the call returns before the work does", row.Status)
	}

	view := waitForInspection(t, svc, row.ID)
	if view.Status != viewStatusSucceeded {
		t.Fatalf("inspection status = %q, want succeeded (row = %+v, failure = %+v)", view.Status, view.Task, view.Failure)
	}
	if view.Failure != nil {
		t.Fatalf("failure = %+v, want nil on success", view.Failure)
	}
	if view.Preview == nil {
		t.Fatal("preview = nil, want the finished inspection's preview")
	}
	if view.Task.Status != tasks.StatusSucceeded {
		t.Fatalf("row status = %q, want %q", view.Task.Status, tasks.StatusSucceeded)
	}
	if view.Task.Outcome == nil || *view.Task.Outcome != "3 units, 0 findings" {
		t.Fatalf("row outcome = %v, want %q", view.Task.Outcome, "3 units, 0 findings")
	}

	preview := view.Preview
	if preview.SourcePath != srcPath {
		t.Fatalf("preview sourcePath = %q, want the chosen path %q", preview.SourcePath, srcPath)
	}
	if preview.OriginalName != "Stardew Valley Expanded-1.2.3.zip" {
		t.Fatalf("preview originalName = %q, want the file's base name", preview.OriginalName)
	}
	info, err := os.Stat(srcPath)
	if err != nil {
		t.Fatalf("stat of the source archive: %v", err)
	}
	if preview.SizeBytes != info.Size() {
		t.Fatalf("preview sizeBytes = %d, want %d", preview.SizeBytes, info.Size())
	}
	contents, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("reading the source archive: %v", err)
	}
	digest := sha256.Sum256(contents)
	if preview.ArchiveSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("preview archiveSha256 = %q, want the source file's SHA-256", preview.ArchiveSHA256)
	}
	if preview.PackageSHA256 == "" {
		t.Fatal("preview packageSha256 is empty, want the extracted tree's digest")
	}
	if !preview.Installable {
		t.Fatalf("preview installable = false, want true for a multi-mod bundle (findings = %+v)", preview.Findings)
	}

	wantPaths := []string{
		"Stardew Valley Expanded/StardewValleyExpanded",
		"Stardew Valley Expanded/[CP] Stardew Valley Expanded",
		"Stardew Valley Expanded/[FTM] Stardew Valley Expanded",
	}
	wantKinds := []inspect.Kind{inspect.KindCodeMod, inspect.KindContentPack, inspect.KindContentPack}
	if len(preview.Units) != len(wantPaths) {
		t.Fatalf("preview units = %d, want %d (%+v)", len(preview.Units), len(wantPaths), preview.Units)
	}
	for i, want := range wantPaths {
		if preview.Units[i].RelativePath != want {
			t.Errorf("unit %d relativePath = %q, want %q", i, preview.Units[i].RelativePath, want)
		}
		if preview.Units[i].Kind != wantKinds[i] {
			t.Errorf("unit %d kind = %q, want %q", i, preview.Units[i].Kind, wantKinds[i])
		}
	}

	stageGone(t, paths, row.ID)
}

// One inspection runs at a time: a call made while one is live refuses with
// INSPECTION_BUSY and leaves no task row behind. The live run keeps its own
// row moving pending → running → succeeded.
func TestInspectArchiveRefusesASecondRunWhileOneIsLive(t *testing.T) {
	svc, _ := newInspectionService(t)
	srcPath := writeArchive(t, "bulk.zip", slowFixture())

	first, err := svc.InspectArchive(srcPath)
	if err != nil {
		t.Fatalf("InspectArchive() error = %v", err)
	}
	if first.Status != tasks.StatusPending {
		t.Fatalf("returned row status = %q, want %q before the work starts", first.Status, tasks.StatusPending)
	}
	running := waitForTaskStatus(t, svc, first.ID, tasks.StatusRunning)
	if running.Current != 0 || running.Message == "" {
		t.Fatalf("running row = %+v, want a message with the run in flight", running)
	}
	live := waitForRunningView(t, svc, first.ID)
	if live.Preview != nil || live.Failure != nil {
		t.Fatalf("running view = %+v, want neither a preview nor a failure before the run ends", live)
	}

	before, err := svc.RecentTasks()
	if err != nil {
		t.Fatalf("RecentTasks() error = %v", err)
	}
	if _, err := svc.InspectArchive(srcPath); err == nil {
		t.Fatal("second InspectArchive() error = nil while one run is live, want INSPECTION_BUSY")
	} else if busy := wantCode(t, err, apperror.CodeInspectionBusy); !busy.Recoverable {
		t.Fatalf("INSPECTION_BUSY = %+v, want a recoverable refusal", busy)
	}
	after, err := svc.RecentTasks()
	if err != nil {
		t.Fatalf("RecentTasks() error = %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("task rows = %d after the refusal, want %d: a refusal before starting must not leave a row behind", len(after), len(before))
	}

	view := waitForInspection(t, svc, first.ID)
	if view.Status != viewStatusSucceeded || view.Preview == nil {
		t.Fatalf("first run = %+v (preview %v), want it to finish on its own", view.Task, view.Preview)
	}
	if view.Task.Status != tasks.StatusSucceeded {
		t.Fatalf("first run row status = %q, want %q", view.Task.Status, tasks.StatusSucceeded)
	}
}

// Inspection answers only for what this process remembers: an unknown id, a
// task that is not an inspection, and an evicted record all report
// INSPECTION_NOT_FOUND, while the four most recent inspections stay
// answerable.
func TestInspectionReportsUnknownForeignAndEvictedIDs(t *testing.T) {
	svc, _ := newInspectionService(t)
	srcPath := writeArchive(t, "FishZones.zip", archive.SingleMod())

	if _, err := svc.Inspection("task-does-not-exist"); err == nil {
		t.Fatal("Inspection() unknown id error = nil, want INSPECTION_NOT_FOUND")
	} else if missing := wantCode(t, err, apperror.CodeInspectionNotFound); !missing.Recoverable {
		t.Fatalf("INSPECTION_NOT_FOUND = %+v, want a recoverable refusal", missing)
	}

	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		row, err := svc.InspectArchive(srcPath)
		if err != nil {
			t.Fatalf("InspectArchive() #%d error = %v", i+1, err)
		}
		if view := waitForInspection(t, svc, row.ID); view.Status != viewStatusSucceeded {
			t.Fatalf("inspection #%d status = %q, want succeeded", i+1, view.Status)
		}
		ids = append(ids, row.ID)
	}

	for _, id := range ids[1:] {
		view, err := svc.Inspection(id)
		if err != nil {
			t.Fatalf("Inspection(%q) error = %v, want the four most recent records kept", id, err)
		}
		if view.Preview == nil {
			t.Fatalf("Inspection(%q) preview = nil, want the finished preview", id)
		}
	}
	if _, err := svc.Inspection(ids[0]); err == nil {
		t.Fatal("Inspection() of the fifth-oldest inspection error = nil, want the record evicted by the four-record FIFO")
	} else {
		wantCode(t, err, apperror.CodeInspectionNotFound)
	}

	ctx := context.Background()
	foreign, err := tasks.New(svc.db.DB()).Create(ctx, "startup")
	if err != nil {
		t.Fatalf("creating a foreign task: %v", err)
	}
	if _, err := svc.Inspection(foreign.ID); err == nil {
		t.Fatal("Inspection() of a non-inspection task error = nil, want INSPECTION_NOT_FOUND")
	} else {
		wantCode(t, err, apperror.CodeInspectionNotFound)
	}
}

// A hostile archive ends failed with the extractor's typed code visible
// through both the row's outcome and Inspection's failure payload, and leaves
// no stage — nor anything outside it.
func TestInspectArchiveReportsAHostileArchiveAsAFailure(t *testing.T) {
	svc, paths := newInspectionService(t)
	sink := &recordingSink{}
	svc.SetEventSink(sink)
	srcPath := writeArchive(t, "hostile.zip", archive.NewFixture().File("../escape.txt", "x"))

	row, err := svc.InspectArchive(srcPath)
	if err != nil {
		t.Fatalf("InspectArchive() error = %v", err)
	}
	view := waitForInspection(t, svc, row.ID)
	if view.Status != viewStatusFailed {
		t.Fatalf("inspection status = %q, want failed", view.Status)
	}
	if view.Preview != nil {
		t.Fatalf("preview = %+v, want nil on failure", view.Preview)
	}
	if view.Failure == nil {
		t.Fatal("failure = nil, want the typed refusal payload")
	}
	if view.Failure.Code != string(apperror.CodeArchivePathTraversal) {
		t.Fatalf("failure code = %q, want %q", view.Failure.Code, apperror.CodeArchivePathTraversal)
	}
	if !strings.Contains(view.Failure.Details, "../escape.txt") {
		t.Fatalf("failure details = %q, want the offending entry named", view.Failure.Details)
	}
	if view.Task.Status != tasks.StatusFailed {
		t.Fatalf("row status = %q, want %q", view.Task.Status, tasks.StatusFailed)
	}
	if view.Task.Outcome == nil || *view.Task.Outcome != string(apperror.CodeArchivePathTraversal) {
		t.Fatalf("row outcome = %v, want the typed code %q", view.Task.Outcome, apperror.CodeArchivePathTraversal)
	}
	stageGone(t, paths, row.ID)
	if _, err := os.Stat(filepath.Join(paths.Temp, "escape.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("escape.txt exists in the temp root (stat error = %v): a hostile entry escaped the stage", err)
	}

	failed := sink.byName(EventTaskFailed)
	if len(failed) != 1 {
		t.Fatalf("task.failed events = %d, want exactly 1 (events: %v)", len(failed), sink.names())
	}
	if failed[0].payload.TaskID != row.ID || failed[0].payload.Operation != OperationInspectArchive {
		t.Fatalf("task.failed payload = %+v, want the task's id and operation", failed[0].payload)
	}
	if failed[0].payload.Message != view.Failure.Message {
		t.Fatalf("task.failed message = %q, want the failure's sentence %q", failed[0].payload.Message, view.Failure.Message)
	}
	if completed := sink.byName(EventTaskCompleted); len(completed) != 0 {
		t.Fatalf("task.completed events = %d on a failed run, want none", len(completed))
	}
}

// Cancelling a live run stops it, waits for its cleanup, records the
// cancelled status, emits the cancelled event, and refuses every later
// attempt to cancel the same row.
func TestCancelTaskStopsALiveRunAndCleansItsStage(t *testing.T) {
	svc, paths := newInspectionService(t)
	sink := &recordingSink{}
	svc.SetEventSink(sink)
	srcPath := writeArchive(t, "bulk.zip", slowFixture())

	row, err := svc.InspectArchive(srcPath)
	if err != nil {
		t.Fatalf("InspectArchive() error = %v", err)
	}
	stage := archive.StageDir(paths.Temp, row.ID)
	waitForStage(t, stage)

	cancelled, err := svc.CancelTask(row.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if cancelled.Status != tasks.StatusCancelled {
		t.Fatalf("cancelled row status = %q, want %q", cancelled.Status, tasks.StatusCancelled)
	}
	if cancelled.Message != cancelMessage {
		t.Fatalf("cancelled row message = %q, want %q", cancelled.Message, cancelMessage)
	}
	if cancelled.Outcome != nil {
		t.Fatalf("cancelled row outcome = %q, want nil: a stopped run has no summary or code", *cancelled.Outcome)
	}
	// CancelTask waits for the run to finish its cleanup, so the stage is
	// already gone the moment it returns — not merely soon afterwards.
	if _, err := os.Stat(stage); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stage %q still exists (stat error = %v) when CancelTask returned, want the cleanup already done", stage, err)
	}

	view, err := svc.Inspection(row.ID)
	if err != nil {
		t.Fatalf("Inspection() after cancel error = %v", err)
	}
	if view.Status != viewStatusCancelled {
		t.Fatalf("inspection status = %q, want %q", view.Status, viewStatusCancelled)
	}
	if view.Preview != nil || view.Failure != nil {
		t.Fatalf("cancelled inspection = %+v, want no preview and no failure", view)
	}

	if events := sink.byName(EventTaskCancelled); len(events) != 1 {
		t.Fatalf("task.cancelled events = %d, want exactly 1 (events: %v)", len(events), sink.names())
	}

	if _, err := svc.CancelTask(row.ID); err == nil {
		t.Fatal("second CancelTask() error = nil, want TASK_NOT_CANCELLABLE")
	} else if refusal := wantCode(t, err, apperror.CodeTaskNotCancellable); !refusal.Recoverable {
		t.Fatalf("TASK_NOT_CANCELLABLE = %+v, want a recoverable refusal", refusal)
	}
	if _, err := svc.CancelTask("task-does-not-exist"); err == nil {
		t.Fatal("CancelTask() unknown id error = nil, want TASK_NOT_FOUND")
	} else {
		wantCode(t, err, apperror.CodeTaskNotFound)
	}
}

// The event hints carry the documented names and payload: started first,
// progress with the phase's numbers, completed last with the summary. A
// service with no sink at all runs the same inspection successfully, because
// no correctness path depends on an emission.
func TestInspectionEmitsTheDocumentedEvents(t *testing.T) {
	svc, _ := newInspectionService(t)
	sink := &recordingSink{}
	svc.SetEventSink(sink)
	srcPath := writeArchive(t, "FishZones.zip", archive.SingleMod())

	row, err := svc.InspectArchive(srcPath)
	if err != nil {
		t.Fatalf("InspectArchive() error = %v", err)
	}
	if view := waitForInspection(t, svc, row.ID); view.Status != viewStatusSucceeded {
		t.Fatalf("inspection status = %q, want succeeded", view.Status)
	}

	events := sink.snapshot()
	if len(events) < 3 {
		t.Fatalf("events = %v, want at least started, progress, and completed", sink.names())
	}
	if events[0].name != EventTaskStarted {
		t.Fatalf("first event = %q, want %q", events[0].name, EventTaskStarted)
	}
	last := events[len(events)-1]
	if last.name != EventTaskCompleted {
		t.Fatalf("last event = %q, want %q", last.name, EventTaskCompleted)
	}

	info, err := os.Stat(srcPath)
	if err != nil {
		t.Fatalf("stat of the source archive: %v", err)
	}
	progress := sink.byName(EventTaskProgress)
	if len(progress) == 0 {
		t.Fatalf("no %s events; events = %v", EventTaskProgress, sink.names())
	}
	first := progress[0]
	if first.payload.Message != "hashing the archive" {
		t.Fatalf("first progress message = %q, want the hashing phase's message", first.payload.Message)
	}
	if first.payload.Current != 0 || first.payload.Total != info.Size() {
		t.Fatalf("first progress = %d/%d, want 0/%d", first.payload.Current, first.payload.Total, info.Size())
	}
	scanning := false
	for _, event := range progress {
		if event.payload.Message == scanMessage {
			scanning = true
			if event.payload.Current != 0 || event.payload.Total != 0 {
				t.Fatalf("scanning progress = %d/%d, want 0/0: the scanning phase is indeterminate", event.payload.Current, event.payload.Total)
			}
		}
	}
	if !scanning {
		t.Fatal("no scanning progress event: the phase change must reach the interface the way a reloaded row shows it")
	}

	for _, event := range []recordedEvent{events[0], first, last} {
		if event.payload.TaskID != row.ID {
			t.Errorf("%s taskId = %q, want %q", event.name, event.payload.TaskID, row.ID)
		}
		if event.payload.Operation != OperationInspectArchive {
			t.Errorf("%s operation = %q, want %q", event.name, event.payload.Operation, OperationInspectArchive)
		}
		for _, key := range []string{"taskId", "operation", "current", "total", "message"} {
			if _, ok := event.fields[key]; !ok {
				t.Errorf("%s payload %+v is missing key %q", event.name, event.fields, key)
			}
		}
	}
	if last.payload.Message != "1 unit, 0 findings" {
		t.Fatalf("completed message = %q, want %q", last.payload.Message, "1 unit, 0 findings")
	}
}

// A nil sink is the headless case: the inspection runs and finishes exactly
// the same, because events are hints and the row is the truth.
func TestInspectArchiveRunsWithANilSink(t *testing.T) {
	svc, paths := newInspectionService(t)
	srcPath := writeArchive(t, "FishZones.zip", archive.SingleMod())

	row, err := svc.InspectArchive(srcPath)
	if err != nil {
		t.Fatalf("InspectArchive() error = %v", err)
	}
	view := waitForInspection(t, svc, row.ID)
	if view.Status != viewStatusSucceeded || view.Preview == nil {
		t.Fatalf("inspection without a sink = %+v (preview %v), want the same success", view.Task, view.Preview)
	}
	stageGone(t, paths, row.ID)
}

// The six settings keys are honoured end to end: the entry budget a test
// lowers decides whether the archive is accepted, which is what proves the
// key-to-field mapping the registry's own default guard cannot see.
func TestInspectArchiveHonoursTheConfiguredLimits(t *testing.T) {
	svc, paths := newInspectionService(t)
	srcPath := writeArchive(t, "FishZones.zip", archive.SingleMod())

	if err := svc.SetSetting(settingEntries, 2); err != nil {
		t.Fatalf("SetSetting(%s, 2) error = %v", settingEntries, err)
	}
	row, err := svc.InspectArchive(srcPath)
	if err != nil {
		t.Fatalf("InspectArchive() error = %v", err)
	}
	view := waitForInspection(t, svc, row.ID)
	if view.Status != viewStatusFailed {
		t.Fatalf("inspection status = %q, want failed under a two-entry budget", view.Status)
	}
	if view.Failure == nil || view.Failure.Code != string(apperror.CodeArchiveLimitExceeded) {
		t.Fatalf("failure = %+v, want %q", view.Failure, apperror.CodeArchiveLimitExceeded)
	}
	if view.Task.Outcome == nil || *view.Task.Outcome != string(apperror.CodeArchiveLimitExceeded) {
		t.Fatalf("row outcome = %v, want %q", view.Task.Outcome, apperror.CodeArchiveLimitExceeded)
	}
	stageGone(t, paths, row.ID)

	// Raising the same key accepts the archive: the budget is read per run,
	// not cached from the first inspection.
	if err := svc.SetSetting(settingEntries, 100); err != nil {
		t.Fatalf("SetSetting(%s, 100) error = %v", settingEntries, err)
	}
	row, err = svc.InspectArchive(srcPath)
	if err != nil {
		t.Fatalf("InspectArchive() error = %v", err)
	}
	if view := waitForInspection(t, svc, row.ID); view.Status != viewStatusSucceeded {
		t.Fatalf("inspection status = %q under a hundred-entry budget, want succeeded (%+v)", view.Status, view.Failure)
	}
}

// A row left running by a previous app run is not this process's work: it is
// neither inspectable nor cancellable, and a refused cancellation leaves it
// exactly as it was.
func TestStaleRunningRowIsNotFoundAndNotCancellable(t *testing.T) {
	svc, _ := newInspectionService(t)
	ctx := context.Background()
	store := tasks.New(svc.db.DB())
	stale, err := store.Create(ctx, OperationInspectArchive)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := store.UpdateProgress(ctx, stale.ID, 4, 8, "extracting the archive"); err != nil {
		t.Fatalf("UpdateProgress() error = %v", err)
	}

	if _, err := svc.Inspection(stale.ID); err == nil {
		t.Fatal("Inspection() of a stale running row error = nil, want INSPECTION_NOT_FOUND")
	} else {
		wantCode(t, err, apperror.CodeInspectionNotFound)
	}
	if _, err := svc.CancelTask(stale.ID); err == nil {
		t.Fatal("CancelTask() of a stale running row error = nil, want TASK_NOT_CANCELLABLE")
	} else {
		wantCode(t, err, apperror.CodeTaskNotCancellable)
	}
	after, err := store.Get(ctx, stale.ID)
	if err != nil {
		t.Fatalf("Get() after the refused cancel error = %v", err)
	}
	if after.Status != tasks.StatusRunning || after.Current != 4 {
		t.Fatalf("stale row = %+v, want it untouched at running 4/8", after)
	}
}

// The generated bindings read the JSON keys of the inspection wire types.
// Renaming a field silently empties the sheet, so the shape is asserted here
// rather than assumed.
func TestInspectionWireShapeKeepsTheContractKeys(t *testing.T) {
	preview := &Preview{
		SourcePath:    `C:\mods\SomeMod 1.4.2.zip`,
		OriginalName:  "SomeMod 1.4.2.zip",
		SizeBytes:     1234,
		ArchiveSHA256: "aa",
		PackageSHA256: "bb",
		Preview: inspect.Preview{
			Units:       []inspect.Unit{{RelativePath: "SomeMod", FolderName: "SomeMod", Kind: inspect.KindCodeMod}},
			Findings:    []inspect.Finding{},
			Duplicates:  []inspect.DuplicateGroup{},
			Installable: true,
		},
	}
	view := InspectionView{
		Task:    tasks.Task{ID: "task-1", Operation: OperationInspectArchive, Status: tasks.StatusSucceeded},
		Status:  viewStatusSucceeded,
		Preview: preview,
	}

	requireKeys(t, view, "task", "status", "preview", "failure")
	requireKeys(t, view.Task, "id", "operation", "status", "current", "total", "message", "outcome", "createdAt", "updatedAt")
	requireKeys(t, *view.Preview, "sourcePath", "originalName", "sizeBytes", "archiveSha256", "packageSha256", "units", "findings", "duplicates", "installable")

	running, err := json.Marshal(InspectionView{Task: tasks.Task{ID: "task-2"}, Status: viewStatusRunning})
	if err != nil {
		t.Fatalf("marshalling a running view: %v", err)
	}
	const wantRunning = `{"task":{"id":"task-2","operation":"","status":"","current":0,"total":0,"message":"","outcome":null,"createdAt":"","updatedAt":""},"status":"running","preview":null,"failure":null}`
	if string(running) != wantRunning {
		t.Fatalf("running view JSON = %s, want %s", running, wantRunning)
	}

	requireKeys(t, InspectionFailure{Code: "ARCHIVE_CORRUPT", Message: "m", Details: "d"}, "code", "message", "details")
	requireKeys(t, TaskEvent{TaskID: "task-1", Operation: OperationInspectArchive}, "taskId", "operation", "current", "total", "message")
}

// The inspection refusals must travel the real boundary the way ProbeFailure
// does: through the bound method's call path, so the code the service reports
// is the code the frontend's promise rejects with.
func TestInspectionRefusalSurvivesBoundaryCall(t *testing.T) {
	_ = application.New(application.Options{})
	bindings := application.NewBindings(nil, nil)
	svc, _ := newInspectionService(t)
	if err := bindings.Add(application.NewServiceWithOptions(svc, application.ServiceOptions{
		MarshalError: apperror.MarshalError,
	})); err != nil {
		t.Fatalf("bindings.Add() error = %v", err)
	}
	bound := bindings.Get(&application.CallOptions{
		MethodName: "github.com/Zendevve/astradew/internal/app.ApplicationService.Inspection",
	})
	if bound == nil {
		t.Fatal("bound Inspection method not found")
	}
	arg, err := json.Marshal("task-does-not-exist")
	if err != nil {
		t.Fatalf("marshalling the task id: %v", err)
	}
	_, err = bound.Call(context.TODO(), []json.RawMessage{arg})
	var callErr *application.CallError
	if !errors.As(err, &callErr) {
		t.Fatalf("Call err = %#v, want *application.CallError", err)
	}
	var payload apperror.AppError
	if err := json.Unmarshal(callErr.Cause.(json.RawMessage), &payload); err != nil {
		t.Fatalf("decoding cause %v: %v", callErr.Cause, err)
	}
	if payload.Code != apperror.CodeInspectionNotFound {
		t.Fatalf("boundary error code = %q, want %q", payload.Code, apperror.CodeInspectionNotFound)
	}
	if !payload.Recoverable {
		t.Fatal("boundary error is not recoverable, want a recoverable refusal")
	}
}

// requireKeys asserts value marshals to a JSON object carrying every key. The
// bindings read these keys, so a rename is a breaking change the compiler
// cannot see.
func requireKeys(t *testing.T, value any, keys ...string) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshalling %T: %v", value, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decoding %T JSON %s: %v", value, encoded, err)
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			t.Errorf("%T JSON %s is missing key %q", value, encoded, key)
		}
	}
}
