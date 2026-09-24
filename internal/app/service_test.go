package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/approot"
	"github.com/Zendevve/astradew/internal/buildinfo"
	"github.com/Zendevve/astradew/internal/settings"
	"github.com/Zendevve/astradew/internal/store"
	"github.com/Zendevve/astradew/internal/tasks"
)

func TestServiceReportsTheIdentityItWasConstructedWith(t *testing.T) {
	tests := []struct {
		name        string
		productName string
		version     string
	}{
		{name: "release build", productName: "Astradew", version: "1.4.2"},
		{name: "development build", productName: "Astradew", version: "0.0.0-dev"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := New(test.productName, test.version).Info()
			if got.Name != test.productName || got.Version != test.version {
				t.Fatalf("Info() = %+v, want {Name:%q Version:%q}", got, test.productName, test.version)
			}
		})
	}
}

// The generated frontend bindings read the JSON keys of Info. Renaming a field
// silently empties the window, so the wire shape is asserted here rather than
// assumed.
func TestInfoMarshalsToTheFieldNamesTheFrontendBindsTo(t *testing.T) {
	encoded, err := json.Marshal(New("Astradew", "1.4.2").Info())
	if err != nil {
		t.Fatalf("marshalling Info: %v", err)
	}

	const want = `{"name":"Astradew","version":"1.4.2"}`
	if string(encoded) != want {
		t.Fatalf("Info JSON = %s, want %s", encoded, want)
	}
}

// A build whose version is empty or unparseable shows a meaningless version in
// the window, so the version compiled into this build must be a real one.
func TestBuildVersionIsSemantic(t *testing.T) {
	info := New(buildinfo.Name, buildinfo.Version).Info()

	if info.Name != "Astradew" {
		t.Errorf("buildinfo.Name = %q, want %q", info.Name, "Astradew")
	}

	semver := regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

	if !semver.MatchString(info.Version) {
		t.Errorf("buildinfo.Version = %q, want a semantic version", info.Version)
	}
}

// ProbeFailure must report a zero Info plus a PROBE_FAILURE error so the
// frontend has a typed code to branch on.
func TestProbeFailureReturnsZeroInfoAndCodedError(t *testing.T) {
	info, err := New("Astradew", "1.4.2").ProbeFailure()
	if info != (Info{}) {
		t.Fatalf("ProbeFailure Info = %+v, want zero Info", info)
	}
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("ProbeFailure err = %#v, want *apperror.AppError", err)
	}
	if appErr.Code != apperror.CodeProbeFailure {
		t.Fatalf("ProbeFailure code = %q, want %q", appErr.Code, apperror.CodeProbeFailure)
	}
}

// The code must survive the real boundary mechanism: the ProbeFailure error
// is routed through the bound method's marshalError hook (the per-service
// MarshalError path), whose output is what lands on the TypeScript
// rejection's cause. Code out must equal code in.
func TestProbeFailureCodeSurvivesBoundaryMarshaller(t *testing.T) {
	_ = application.New(application.Options{})
	bindings := application.NewBindings(nil, nil)
	svc := New("Astradew", "1.4.2")
	if err := bindings.Add(application.NewServiceWithOptions(svc, application.ServiceOptions{
		MarshalError: apperror.MarshalError,
	})); err != nil {
		t.Fatalf("bindings.Add() error = %v", err)
	}

	bound := bindings.Get(&application.CallOptions{
		MethodName: "github.com/Zendevve/astradew/internal/app.ApplicationService.ProbeFailure",
	})
	if bound == nil {
		t.Fatal("bound ProbeFailure method not found")
	}

	_, err := bound.Call(context.TODO(), nil)
	var callErr *application.CallError
	if !errors.As(err, &callErr) {
		t.Fatalf("Call err = %#v, want *application.CallError", err)
	}
	if callErr.Kind != application.RuntimeError {
		t.Fatalf("CallError.Kind = %q, want RuntimeError", callErr.Kind)
	}

	cause, marshalErr := json.Marshal(callErr.Cause)
	if marshalErr != nil {
		t.Fatalf("marshalling CallError cause: %v", marshalErr)
	}
	var decoded apperror.AppError
	if err := json.Unmarshal(cause, &decoded); err != nil {
		t.Fatalf("decoding cause %s: %v", cause, err)
	}
	if decoded.Code != apperror.CodeProbeFailure {
		t.Fatalf("code out = %q, want code in %q", decoded.Code, apperror.CodeProbeFailure)
	}
}

// Paths must report the resolved data layout the service was constructed
// with, so the frontend can render the real locations. The wire shape
// carries the lowerCamel JSON keys the bindings generate.
func TestPathsReportsResolvedLayout(t *testing.T) {
	paths := approot.Paths{
		Root:     `C:\Users\someone\AppData\Local\Astradew`,
		Database: `C:\Users\someone\AppData\Local\Astradew\database`,
		Library:  `C:\Users\someone\AppData\Local\Astradew\library`,
		Profiles: `C:\Users\someone\AppData\Local\Astradew\profiles`,
		Backups:  `C:\Users\someone\AppData\Local\Astradew\backups`,
		Cache:    `C:\Users\someone\AppData\Local\Astradew\cache`,
		Logs:     `C:\Users\someone\AppData\Local\Astradew\logs`,
		Temp:     `C:\Users\someone\AppData\Local\Astradew\temp`,
	}
	if got := NewWithPaths("Astradew", "1.4.2", paths).Paths(); got != paths {
		t.Fatalf("Paths() = %+v, want %+v", got, paths)
	}
	encoded, err := json.Marshal(NewWithPaths("Astradew", "1.4.2", paths).Paths())
	if err != nil {
		t.Fatalf("marshalling Paths: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding Paths JSON %s: %v", encoded, err)
	}
	for _, key := range []string{"root", "database", "library", "profiles", "backups", "cache", "logs", "temp"} {
		if decoded[key] == "" {
			t.Fatalf("Paths JSON %s missing key %q", encoded, key)
		}
	}
	if decoded["root"] != paths.Root {
		t.Fatalf("Paths JSON root = %q, want %q", decoded["root"], paths.Root)
	}
}

// New keeps its old contract: no root injected means a zero layout, never a
// nil panic.
func TestNewReportsZeroPaths(t *testing.T) {
	if got := New("Astradew", "1.4.2").Paths(); got != (approot.Paths{}) {
		t.Fatalf("Paths() = %+v, want zero", got)
	}
}

// Paths must travel the real boundary the way ProbeFailure does: through the
// bound method's call path, so the code the service reports is the code the
// frontend can read.
func TestPathsSurvivesBoundaryCall(t *testing.T) {
	_ = application.New(application.Options{})
	bindings := application.NewBindings(nil, nil)
	paths := approot.Paths{
		Root:     `C:\Users\someone\AppData\Local\Astradew`,
		Database: `C:\Users\someone\AppData\Local\Astradew\database`,
		Library:  `C:\Users\someone\AppData\Local\Astradew\library`,
		Profiles: `C:\Users\someone\AppData\Local\Astradew\profiles`,
		Backups:  `C:\Users\someone\AppData\Local\Astradew\backups`,
		Cache:    `C:\Users\someone\AppData\Local\Astradew\cache`,
		Logs:     `C:\Users\someone\AppData\Local\Astradew\logs`,
		Temp:     `C:\Users\someone\AppData\Local\Astradew\temp`,
	}
	svc := NewWithPaths("Astradew", "1.4.2", paths)
	if err := bindings.Add(application.NewServiceWithOptions(svc, application.ServiceOptions{
		MarshalError: apperror.MarshalError,
	})); err != nil {
		t.Fatalf("bindings.Add() error = %v", err)
	}

	bound := bindings.Get(&application.CallOptions{
		MethodName: "github.com/Zendevve/astradew/internal/app.ApplicationService.Paths",
	})
	if bound == nil {
		t.Fatal("bound Paths method not found")
	}
	result, err := bound.Call(context.TODO(), nil)
	if err != nil {
		t.Fatalf("Call error = %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshalling Call result: %v", err)
	}
	var decoded approot.Paths
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding result %s: %v", encoded, err)
	}
	if decoded != paths {
		t.Fatalf("boundary Paths = %+v, want %+v", decoded, paths)
	}
}

// openTestStore opens a real migrated database under a temp root for Health
// tests. The store is real so database health reads live state.
func openTestStore(t *testing.T, paths approot.Paths) *store.Store {
	t.Helper()
	db, err := store.Open(paths)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// Health reports the constructed identity, every resolved directory as
// writable, the live database, the recorded startup steps, and the
// not-yet-available capabilities.
func TestHealthReportsObservedState(t *testing.T) {
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openTestStore(t, paths)
	report := NewWithPathsAndStore("Astradew", "1.4.2", paths, db).Health()

	if report.Name != "Astradew" || report.Version != "1.4.2" {
		t.Fatalf("identity = %q %q, want Astradew 1.4.2", report.Name, report.Version)
	}
	if report.DataRoot != paths.Root {
		t.Fatalf("DataRoot = %q, want %q", report.DataRoot, paths.Root)
	}
	if len(report.Directories) != len(paths.Dirs()) {
		t.Fatalf("directories = %d, want %d (every dir in Dirs())", len(report.Directories), len(paths.Dirs()))
	}
	for _, dir := range report.Directories {
		if !dir.Writable {
			t.Fatalf("directory %q at %s reported unwritable on a fresh root", dir.Name, dir.Path)
		}
	}
	if len(report.Findings) != 0 {
		t.Fatalf("findings = %+v, want none on a fresh root", report.Findings)
	}
	if report.Database.Path != db.Path() || report.Database.Version != db.Version() {
		t.Fatalf("database = %+v, want path %q version %d", report.Database, db.Path(), db.Version())
	}
	if !report.Database.Healthy || report.Database.State != "open" {
		t.Fatalf("database = %+v, want healthy open", report.Database)
	}
	if len(report.Initialisation) != 2 || !report.Initialisation[0].OK || !report.Initialisation[1].OK {
		t.Fatalf("initialisation = %+v, want the two bind-time steps ok", report.Initialisation)
	}
	for _, want := range []string{"Game detection", "SMAPI detection", "Mod health"} {
		found := false
		for _, entry := range report.Unavailable {
			if entry.Name == want {
				found = true
				if entry.Status != "unavailable" || entry.Reason == "" {
					t.Fatalf("unavailable %q = %+v, want status unavailable with a reason", want, entry)
				}
			}
		}
		if !found {
			t.Fatalf("unavailable entry %q missing", want)
		}
	}
}

// The logger step is absent until main.go records it: Health never claims a
// step that did not run.
func TestHealthClaimsNoLoggerStepBeforeItRuns(t *testing.T) {
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openTestStore(t, paths)
	svc := NewWithPathsAndStore("Astradew", "1.4.2", paths, db)
	for _, step := range svc.Health().Initialisation {
		if step.Name == "Create logger" {
			t.Fatal("logger step present before NoteLoggerCreated: Health claimed a step that did not run")
		}
	}
	svc.NoteLoggerCreated()
	found := false
	for _, step := range svc.Health().Initialisation {
		if step.Name == "Create logger" && step.OK {
			found = true
		}
	}
	if !found {
		t.Fatal("logger step missing after NoteLoggerCreated")
	}
}

// A file where a directory belongs surfaces as an error finding with an
// action — never as a writable pass.
func TestHealthSurfacesBrokenDirectory(t *testing.T) {
	base := t.TempDir()
	paths, err := approot.ResolveWithBase(base)
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openTestStore(t, paths)
	broken := paths
	broken.Cache = t.TempDir()
	blocker := broken.Cache
	if err := os.RemoveAll(blocker); err != nil {
		t.Fatalf("removing cache dir: %v", err)
	}
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("seeding blocker file: %v", err)
	}
	broken.Cache = blocker

	report := NewWithPathsAndStore("Astradew", "1.4.2", broken, db).Health()
	for _, dir := range report.Directories {
		if dir.Path == blocker && dir.Writable {
			t.Fatal("blocker path reported writable: a file-where-dir must not pass")
		}
	}
	found := false
	for _, finding := range report.Findings {
		if finding.Severity == "error" && strings.Contains(finding.What, blocker) && strings.Contains(finding.Action, blocker) {
			found = true
		}
	}
	if !found {
		t.Fatalf("findings = %+v, want an error finding naming %q with an action", report.Findings, blocker)
	}
}

// A service constructed without a store reports the database unavailable —
// never healthy, never fabricated.
func TestHealthReportsNilStoreUnavailable(t *testing.T) {
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	for _, svc := range []*ApplicationService{New("Astradew", "1.4.2"), NewWithPaths("Astradew", "1.4.2", paths)} {
		report := svc.Health()
		if report.Database.Healthy {
			t.Fatal("database healthy with no store handle: health must not fabricate state")
		}
		if !strings.Contains(report.Database.State, "unavailable") {
			t.Fatalf("database state = %q, want an unavailable explanation", report.Database.State)
		}
		if len(report.Initialisation) != 0 && svc.paths.Root == "" {
			t.Fatalf("initialisation = %+v, want no claimed steps without a resolved root", report.Initialisation)
		}
	}
}

// The frontend binds to these JSON keys, so the Health wire shape is asserted
// here rather than assumed.
func TestHealthMarshalsToTheContractKeys(t *testing.T) {
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openTestStore(t, paths)
	encoded, err := json.Marshal(NewWithPathsAndStore("Astradew", "1.4.2", paths, db).Health())
	if err != nil {
		t.Fatalf("marshalling Health: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding Health JSON: %v", err)
	}
	for _, key := range []string{"name", "version", "dataRoot", "directories", "database", "initialisation", "findings", "unavailable"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("Health JSON %s missing key %q", encoded, key)
		}
	}
	dbJSON, ok := decoded["database"].(map[string]any)
	if !ok {
		t.Fatalf("database = %v, want an object", decoded["database"])
	}
	for _, key := range []string{"path", "version", "healthy", "state"} {
		if _, ok := dbJSON[key]; !ok {
			t.Fatalf("database JSON %v missing key %q", dbJSON, key)
		}
	}
}

// Health must travel the real boundary the way Paths does: through the bound
// method's call path, so the frontend reads what the service reported.
func TestHealthSurvivesBoundaryCall(t *testing.T) {
	_ = application.New(application.Options{})
	bindings := application.NewBindings(nil, nil)
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openTestStore(t, paths)
	svc := NewWithPathsAndStore("Astradew", "1.4.2", paths, db)
	if err := bindings.Add(application.NewServiceWithOptions(svc, application.ServiceOptions{
		MarshalError: apperror.MarshalError,
	})); err != nil {
		t.Fatalf("bindings.Add() error = %v", err)
	}

	bound := bindings.Get(&application.CallOptions{
		MethodName: "github.com/Zendevve/astradew/internal/app.ApplicationService.Health",
	})
	if bound == nil {
		t.Fatal("bound Health method not found")
	}
	result, err := bound.Call(context.TODO(), nil)
	if err != nil {
		t.Fatalf("Call error = %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshalling Call result: %v", err)
	}
	var decoded HealthReport
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding result %s: %v", encoded, err)
	}
	if decoded.Name != "Astradew" || decoded.Version != "1.4.2" {
		t.Fatalf("boundary identity = %q %q, want Astradew 1.4.2", decoded.Name, decoded.Version)
	}
	if decoded.DataRoot != paths.Root {
		t.Fatalf("boundary dataRoot = %q, want %q", decoded.DataRoot, paths.Root)
	}
	if !decoded.Database.Healthy || decoded.Database.Version != db.Version() {
		t.Fatalf("boundary database = %+v, want healthy at version %d", decoded.Database, db.Version())
	}
}

// Task must travel the real boundary the way Paths does: through the bound
// method's call path, so the record the frontend reads is the durable row
// the service re-read from SQLite.
func TestTaskSurvivesBoundaryCall(t *testing.T) {
	_ = application.New(application.Options{})
	bindings := application.NewBindings(nil, nil)
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openTestStore(t, paths)
	ctx := context.Background()
	created, err := tasks.New(db.DB()).Create(ctx, "startup")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := tasks.New(db.DB()).Succeed(ctx, created.ID, "startup complete at schema version 1"); err != nil {
		t.Fatalf("Succeed() error = %v", err)
	}
	svc := NewWithPathsAndStore("Astradew", "1.4.2", paths, db)
	if err := bindings.Add(application.NewServiceWithOptions(svc, application.ServiceOptions{
		MarshalError: apperror.MarshalError,
	})); err != nil {
		t.Fatalf("bindings.Add() error = %v", err)
	}

	bound := bindings.Get(&application.CallOptions{
		MethodName: "github.com/Zendevve/astradew/internal/app.ApplicationService.Task",
	})
	if bound == nil {
		t.Fatal("bound Task method not found")
	}
	idArg, err := json.Marshal(created.ID)
	if err != nil {
		t.Fatalf("marshalling id: %v", err)
	}
	result, err := bound.Call(context.TODO(), []json.RawMessage{idArg})
	if err != nil {
		t.Fatalf("Call error = %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshalling Call result: %v", err)
	}
	var decoded tasks.Task
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding result %s: %v", encoded, err)
	}
	if decoded.ID != created.ID || decoded.Status != tasks.StatusSucceeded {
		t.Fatalf("boundary Task = %+v, want id %q status succeeded", decoded, created.ID)
	}
	if decoded.Outcome == nil || *decoded.Outcome != "startup complete at schema version 1" {
		t.Fatalf("boundary Task outcome = %v, want the succeed summary", decoded.Outcome)
	}

	// An unknown id keeps its typed code across the boundary: the
	// marshalled error must still name TASK_NOT_FOUND.
	unknownArg, err := json.Marshal("task-does-not-exist")
	if err != nil {
		t.Fatalf("marshalling unknown id: %v", err)
	}
	if _, err := bound.Call(context.TODO(), []json.RawMessage{unknownArg}); err == nil {
		t.Fatal("Call unknown id error = nil, want TASK_NOT_FOUND")
	} else {
		var callErr *application.CallError
		if !errors.As(err, &callErr) {
			t.Fatalf("Call err = %#v, want *application.CallError", err)
		}
		cause, marshalErr := json.Marshal(callErr.Cause)
		if marshalErr != nil {
			t.Fatalf("marshalling CallError cause: %v", marshalErr)
		}
		var payload apperror.AppError
		if err := json.Unmarshal(cause, &payload); err != nil {
			t.Fatalf("decoding cause %s: %v", cause, err)
		}
		if payload.Code != apperror.CodeTaskNotFound {
			t.Fatalf("boundary error code = %q, want %q", payload.Code, apperror.CodeTaskNotFound)
		}
	}
}

// Settings lists every registry setting with its declared default before
// anything is written, and nil-store honesty: without a store it refuses
// with SETTING_UNAVAILABLE, never fabricated values.
func TestSettingsListsDefaultsAndRefusesNilStore(t *testing.T) {
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openTestStore(t, paths)
	svc := NewWithPathsAndStore("Astradew", "1.4.2", paths, db)
	list, err := svc.Settings()
	if err != nil {
		t.Fatalf("Settings() error = %v", err)
	}
	if len(list) != len(settings.Registry)-1 {
		t.Fatalf("Settings() returned %d entries, want %d registry entries minus the pointer key", len(list), len(settings.Registry)-1)
	}
	for _, view := range list {
		if view.Name == settings.PrimaryGameInstallIDKey {
			t.Fatalf("Settings() lists %q, want it excluded: the chooser is its only editor", settings.PrimaryGameInstallIDKey)
		}
		if !view.IsDefault {
			t.Fatalf("Settings() %q IsDefault = false, want true before any write", view.Name)
		}
	}
	if _, err := svc.GetSetting("theme"); err != nil {
		t.Fatalf("GetSetting(theme) error = %v", err)
	}
	if _, err := svc.GetSetting(settings.PrimaryGameInstallIDKey); err != nil {
		t.Fatalf("GetSetting(pointer) error = %v, want readable", err)
	}

	bare := New("Astradew", "1.4.2")
	if _, err := bare.Settings(); settingsCode(t, err) != apperror.CodeSettingUnavailable {
		t.Fatalf("Settings() nil-store code = %v, want SETTING_UNAVAILABLE", err)
	}
	if _, err := bare.GetSetting("theme"); settingsCode(t, err) != apperror.CodeSettingUnavailable {
		t.Fatalf("GetSetting() nil-store code = %v, want SETTING_UNAVAILABLE", err)
	}
	if err := bare.SetSetting("theme", "dark"); settingsCode(t, err) != apperror.CodeSettingUnavailable {
		t.Fatalf("SetSetting() nil-store code = %v, want SETTING_UNAVAILABLE", err)
	}
}

// Set then get round-trips through the service: the stored scalar comes back,
// and Settings marks the row non-default.
func TestSetSettingRoundTrip(t *testing.T) {
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openTestStore(t, paths)
	svc := NewWithPathsAndStore("Astradew", "1.4.2", paths, db)
	if err := svc.SetSetting("theme", "dark"); err != nil {
		t.Fatalf("SetSetting(theme) error = %v", err)
	}
	got, err := svc.GetSetting("theme")
	if err != nil {
		t.Fatalf("GetSetting(theme) error = %v", err)
	}
	if got != "dark" {
		t.Fatalf("GetSetting(theme) = %#v, want %q", got, "dark")
	}
	list, err := svc.Settings()
	if err != nil {
		t.Fatalf("Settings() error = %v", err)
	}
	for _, view := range list {
		if view.Name == "theme" && view.IsDefault {
			t.Fatalf("Settings() theme IsDefault = true after write")
		}
	}
}

// The primary pointer is set only through the installs chooser: SetSetting
// on the pointer key refuses with SETTING_INVALID and leaves the stored
// value unchanged. (SetPrimaryGameInstall and the internal adoption path
// write via settings.Service.Set directly, so the guard cannot break them.)
func TestSetSettingRefusesPointerKey(t *testing.T) {
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openTestStore(t, paths)
	svc := NewWithPathsAndStore("Astradew", "1.4.2", paths, db)
	before, err := svc.GetSetting(settings.PrimaryGameInstallIDKey)
	if err != nil {
		t.Fatalf("GetSetting(pointer) error = %v", err)
	}
	if err := svc.SetSetting(settings.PrimaryGameInstallIDKey, 2); settingsCode(t, err) != apperror.CodeSettingInvalid {
		t.Fatalf("SetSetting(pointer) code = %v, want SETTING_INVALID", err)
	}
	after, err := svc.GetSetting(settings.PrimaryGameInstallIDKey)
	if err != nil {
		t.Fatalf("GetSetting(pointer) error = %v", err)
	}
	if before != after {
		t.Fatalf("pointer after refused SetSetting = %#v, want preserved %#v", after, before)
	}
}

// An invalid value refuses with SETTING_INVALID and the stored value stays
func TestSetSettingInvalidPreservesPrevious(t *testing.T) {
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openTestStore(t, paths)
	svc := NewWithPathsAndStore("Astradew", "1.4.2", paths, db)
	if err := svc.SetSetting("ui-scale", 150.0); err != nil {
		t.Fatalf("SetSetting(ui-scale) error = %v", err)
	}
	before, err := svc.GetSetting("ui-scale")
	if err != nil {
		t.Fatalf("GetSetting(ui-scale) error = %v", err)
	}
	if err := svc.SetSetting("ui-scale", 500.0); settingsCode(t, err) != apperror.CodeSettingInvalid {
		t.Fatalf("SetSetting(ui-scale, 500) code = %v, want SETTING_INVALID", err)
	}
	after, err := svc.GetSetting("ui-scale")
	if err != nil {
		t.Fatalf("GetSetting(ui-scale) error = %v", err)
	}
	if before != after {
		t.Fatalf("GetSetting(ui-scale) after rejected Set = %#v, want preserved %#v", after, before)
	}
	if _, err := svc.GetSetting("no-such-setting"); settingsCode(t, err) != apperror.CodeSettingUnknown {
		t.Fatalf("GetSetting(unknown) code = %v, want SETTING_UNKNOWN", err)
	}
}

// The invalid path must travel the real boundary: through the bound method's
// call path with the MarshalError hook, the marshalled error still names
// SETTING_INVALID.
func TestSetSettingInvalidSurvivesBoundaryCall(t *testing.T) {
	_ = application.New(application.Options{})
	bindings := application.NewBindings(nil, nil)
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openTestStore(t, paths)
	svc := NewWithPathsAndStore("Astradew", "1.4.2", paths, db)
	if err := bindings.Add(application.NewServiceWithOptions(svc, application.ServiceOptions{
		MarshalError: apperror.MarshalError,
	})); err != nil {
		t.Fatalf("bindings.Add() error = %v", err)
	}

	bound := bindings.Get(&application.CallOptions{
		MethodName: "github.com/Zendevve/astradew/internal/app.ApplicationService.SetSetting",
	})
	if bound == nil {
		t.Fatal("bound SetSetting method not found")
	}
	keyArg, err := json.Marshal("theme")
	if err != nil {
		t.Fatalf("marshalling key: %v", err)
	}
	valueArg, err := json.Marshal("sepia")
	if err != nil {
		t.Fatalf("marshalling value: %v", err)
	}
	callErr := callSettings(t, bound, keyArg, valueArg)
	var payload apperror.AppError
	if err := json.Unmarshal(callErr.Cause.(json.RawMessage), &payload); err != nil {
		t.Fatalf("decoding cause %v: %v", callErr.Cause, err)
	}
	if payload.Code != apperror.CodeSettingInvalid {
		t.Fatalf("boundary error code = %q, want %q", payload.Code, apperror.CodeSettingInvalid)
	}
}

// callSettings runs a bound SetSetting call expecting a SETTING_INVALID
// refusal and returns the boundary *CallError. The boundary wraps the
// service error in a *CallError whose Cause carries the MarshalError output
// — the same path the probe test asserts — so the code is read off the
// cause, not the error itself.
func callSettings(t *testing.T, bound *application.BoundMethod, args ...json.RawMessage) *application.CallError {
	t.Helper()
	if _, err := bound.Call(context.TODO(), args); err == nil {
		t.Fatal("Call SetSetting(theme, sepia) error = nil, want SETTING_INVALID")
		return nil
	} else {
		var callErr *application.CallError
		if !errors.As(err, &callErr) {
			t.Fatalf("Call err = %#v, want *application.CallError", err)
		}
		return callErr
	}
}

// settingsCode extracts the typed code from an error, failing the test when
// the error carries none.
func settingsCode(t *testing.T, err error) apperror.Code {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error %#v carries no typed code", err)
	}
	return appErr.Code
}

// A service constructed without a store refuses task reads with
// STORE_OPEN_FAILED — never a fabricated record or an empty list.
func TestTaskRefusesNilStore(t *testing.T) {
	bare := New("Astradew", "1.4.2")
	if _, err := bare.Task("anything"); settingsCode(t, err) != apperror.CodeStoreOpenFailed {
		t.Fatalf("Task() nil-store code = %v, want STORE_OPEN_FAILED", err)
	}
	if _, err := bare.RecentTasks(); settingsCode(t, err) != apperror.CodeStoreOpenFailed {
		t.Fatalf("RecentTasks() nil-store code = %v, want STORE_OPEN_FAILED", err)
	}
}
