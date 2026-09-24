// Package app exposes Astradew's own identity to the frontend.
//
// The service is a plain type: it holds the name and version it was constructed
// with and reports them. It has no dependency on the framework runtime, so it is
// exercised directly by Go tests.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/approot"
	"github.com/Zendevve/astradew/internal/settings"
	"github.com/Zendevve/astradew/internal/store"
	"github.com/Zendevve/astradew/internal/tasks"
)

// Info is the identity of the running application as the interface sees it.
type Info struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ApplicationService reports the identity of the application it was constructed
// for. It is bound to the frontend as ApplicationService.
type ApplicationService struct {
	name    string
	version string
	paths   approot.Paths
	db      *store.Store
	init    []InitStep
}

// New returns an ApplicationService reporting the given product name and
// version.
func New(name, version string) *ApplicationService {
	return &ApplicationService{name: name, version: version}
}

// NewWithPaths returns an ApplicationService reporting the given product name,
// version, and resolved application data layout. main.go resolves the root
// before binding and passes it here; New keeps working for callers (tests)
// with no root to report.
func NewWithPaths(name, version string, paths approot.Paths) *ApplicationService {
	return &ApplicationService{name: name, version: version, paths: paths}
}

// NewWithPathsAndStore returns an ApplicationService with the resolved data
// layout and the open database main.go passes at bind time, so Health reads
// live state instead of reopening the file (the store's single-connection
// pool owns it). The init steps record exactly the two accomplishments
// visible at bind time — root resolved, database opened; the logger step is
// added later via NoteLoggerCreated and never claimed before it runs.
// A nil store is honest, not fatal: Health reports the database unavailable.
func NewWithPathsAndStore(name, version string, paths approot.Paths, db *store.Store) *ApplicationService {
	s := &ApplicationService{name: name, version: version, paths: paths, db: db}
	if paths.Root != "" {
		s.init = append(s.init, InitStep{Name: "Resolve application data root", OK: true, Message: "resolved at " + paths.Root})
	}
	if db != nil {
		s.init = append(s.init, InitStep{Name: "Open database", OK: true, Message: fmt.Sprintf("opened %s at schema version %d", db.Path(), db.Version())})
	}
	return s
}

// NoteLoggerCreated records the logger step. main.go calls it after creating
// the logger, so Health never claims a step that did not run.
func (s *ApplicationService) NoteLoggerCreated() {
	message := "logger created"
	if s.paths.Logs != "" {
		message = "writing to " + s.paths.Logs
	}
	s.init = append(s.init, InitStep{Name: "Create logger", OK: true, Message: message})
}

// Info returns the application's identity.
func (s *ApplicationService) Info() Info {
	return Info{Name: s.name, Version: s.version}
}

// ProbeFailure is a failing demo probe for the typed-error path. It returns a
// zero Info and a PROBE_FAILURE *apperror.AppError; Wails serialises the error
// through the service's MarshalError hook (apperror.MarshalError) so the
// TypeScript call rejects with the structured error as its cause.
func (s *ApplicationService) ProbeFailure() (Info, error) {
	return Info{}, apperror.NewRecoverable(apperror.CodeProbeFailure, "probe failure", "demo probe")
}

// Paths returns the resolved application data layout: the single Astradew/
// root under the OS data directory and every product subdirectory. The
// frontend renders it so the resolved locations stay visible.
func (s *ApplicationService) Paths() approot.Paths {
	return s.paths
}

// InitStep is one startup step and whether it completed. Only steps that ran
// are recorded; a step that did not run is absent, never marked ok.
type InitStep struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// HealthDirectory is one product directory and whether Astradew can write to
// it right now.
type HealthDirectory struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Writable bool   `json:"writable"`
}

// HealthDatabase is the live database state read through the open handle. A
// nil handle reports State "unavailable: ..." with Healthy false — never
// fabricated as healthy.
type HealthDatabase struct {
	Path    string `json:"path"`
	Version int    `json:"version"`
	Healthy bool   `json:"healthy"`
	State   string `json:"state"`
}

// HealthFinding is one observed problem: what was seen, why it matters, and
// what the player can do about it.
type HealthFinding struct {
	Severity string `json:"severity"`
	What     string `json:"what"`
	Why      string `json:"why"`
	Action   string `json:"action"`
}

// HealthUnavailable is one capability the backend cannot offer yet. Anything
// unobservable lands here, never as a pass.
type HealthUnavailable struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// HealthReport is everything the Health view can honestly report. The JSON
// keys are the contract the frontend binds to.
type HealthReport struct {
	Name           string              `json:"name"`
	Version        string              `json:"version"`
	DataRoot       string              `json:"dataRoot"`
	Directories    []HealthDirectory   `json:"directories"`
	Database       HealthDatabase      `json:"database"`
	Initialisation []InitStep          `json:"initialisation"`
	Findings       []HealthFinding     `json:"findings"`
	Unavailable    []HealthUnavailable `json:"unavailable"`
}

// unavailableCapabilities lists what this phase cannot do yet. Every entry
// carries status "unavailable" with its reason — never "healthy".
func unavailableCapabilities() []HealthUnavailable {
	return []HealthUnavailable{
		{Name: "Game detection", Status: "unavailable", Reason: "game detection is not yet implemented in this phase"},
		{Name: "SMAPI detection", Status: "unavailable", Reason: "SMAPI detection is not yet implemented in this phase"},
		{Name: "Mod health", Status: "unavailable", Reason: "mod health checks are not yet implemented in this phase; they run once a game with mods is configured"},
	}
}

// Health reports only what the backend can observe: the constructed identity,
// per-directory writability under the resolved root, live database state
// through the open handle, the recorded startup steps, observed findings,
// and the not-yet-available capabilities.
func (s *ApplicationService) Health() HealthReport {
	report := HealthReport{
		Name:           s.name,
		Version:        s.version,
		DataRoot:       s.paths.Root,
		Directories:    []HealthDirectory{},
		Initialisation: append([]InitStep(nil), s.init...),
		Findings:       []HealthFinding{},
		Unavailable:    unavailableCapabilities(),
	}
	dirs := []HealthDirectory{
		{Name: "Root", Path: s.paths.Root},
		{Name: "Database", Path: s.paths.Database},
		{Name: "Library", Path: s.paths.Library},
		{Name: "Profiles", Path: s.paths.Profiles},
		{Name: "Backups", Path: s.paths.Backups},
		{Name: "Cache", Path: s.paths.Cache},
		{Name: "Logs", Path: s.paths.Logs},
		{Name: "Temporary files", Path: s.paths.Temp},
	}
	for i := range dirs {
		if err := probeDirWritable(dirs[i].Path); err != nil {
			dirs[i].Writable = false
			report.Findings = append(report.Findings, HealthFinding{
				Severity: "error",
				What:     fmt.Sprintf("directory %q at %s is not usable: %s", dirs[i].Name, dirs[i].Path, err),
				Why:      fmt.Sprintf("Astradew keeps %s data there; without a writable directory it cannot store what that area owns", dirs[i].Name),
				Action:   fmt.Sprintf("Fix permissions on %s and restart, or free the path if a file blocks it", dirs[i].Path),
			})
		} else {
			dirs[i].Writable = true
		}
	}
	report.Directories = dirs
	report.Database = s.databaseHealth()
	if s.db != nil && !report.Database.Healthy {
		report.Findings = append(report.Findings, HealthFinding{
			Severity: "error",
			What:     fmt.Sprintf("database at %s is not answering: %s", report.Database.Path, report.Database.State),
			Why:      "health is read from the open database; an unreachable database means stored state cannot be trusted",
			Action:   fmt.Sprintf("Restart the application; if it persists, restore the newest backup over %s and restart, or move %s aside and restart", report.Database.Path, report.Database.Path),
		})
	}
	return report
}

// databaseHealth reads the live state through the open handle. It never
// reopens the file: the store's single-connection pool owns it.
func (s *ApplicationService) databaseHealth() HealthDatabase {
	if s.db == nil {
		return HealthDatabase{State: "unavailable: service constructed without a database handle"}
	}
	health := HealthDatabase{Path: s.db.Path(), Version: s.db.Version()}
	if err := s.db.DB().Ping(); err != nil {
		health.State = fmt.Sprintf("unreachable: %s", err)
		return health
	}
	health.Healthy = true
	health.State = "open"
	return health
}

// probeDirWritable proves dir accepts writes: it must exist as a directory
// and take a temp file. A local probe, mirroring approot's, so health
// observes the current state instead of trusting resolution-time checks.
func probeDirWritable(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory")
	}
	probe, err := os.CreateTemp(dir, ".health-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

// SettingView is one registry setting with its current value: what the
// /settings view renders per row. Value holds a JSON scalar (string, bool,
// or number); IsDefault reports whether it equals the declared default.
type SettingView struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Value       any    `json:"value"`
	IsDefault   bool   `json:"isDefault"`
	Description string `json:"description"`
}

// settingsService resolves the settings service over the open handle, or
// refuses with recoverable SETTING_UNAVAILABLE when constructed without a
// database — never a fabricated store.
func (s *ApplicationService) settingsService() (*settings.Service, error) {
	if s.db == nil {
		return nil, apperror.NewRecoverable(apperror.CodeSettingUnavailable, "settings unavailable", "settings unavailable: service constructed without a database handle")
	}
	return settings.New(s.db.DB()), nil
}

// Settings lists every registry setting with its current value and whether
// it is still the declared default.
func (s *ApplicationService) Settings() ([]SettingView, error) {
	svc, err := s.settingsService()
	if err != nil {
		return nil, err
	}
	values, err := svc.GetAll(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]SettingView, 0, len(values))
	for _, value := range values {
		out = append(out, SettingView{
			Name:        value.Name,
			Kind:        string(value.Kind),
			Value:       value.Value,
			IsDefault:   value.IsDefault,
			Description: value.Description,
		})
	}
	return out, nil
}

// GetSetting returns the current value of one registry setting: the stored
// JSON scalar, or the declared default when never written. An unknown key
// refuses with SETTING_UNKNOWN.
func (s *ApplicationService) GetSetting(key string) (any, error) {
	svc, err := s.settingsService()
	if err != nil {
		return nil, err
	}
	return svc.Get(context.Background(), key)
}

// SetSetting validates value against the key's declaration and stores it. An
// unknown key refuses with SETTING_UNKNOWN; an invalid value refuses with
// recoverable SETTING_INVALID, naming the key, the reason, and the preserved
// previous value. The stored value is left unchanged on any refusal.
func (s *ApplicationService) SetSetting(key string, value any) error {
	svc, err := s.settingsService()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return apperror.NewRecoverable(apperror.CodeSettingInvalid, fmt.Sprintf("invalid value for setting %q", key), fmt.Sprintf("setting %q: value is not JSON-encodable: %s", key, err))
	}
	return svc.Set(context.Background(), key, encoded)
}

// Task returns the durable record for id, re-read live from SQLite
// through the open handle (ADR 0006: rows are the source of truth, Wails
// events are live hints never replayed). An unknown id reports
// TASK_NOT_FOUND; a service with no open store reports the store
// unavailable instead of fabricating a record.
func (s *ApplicationService) Task(id string) (tasks.Task, error) {
	if s.db == nil {
		return tasks.Task{}, apperror.New(apperror.CodeStoreOpenFailed, "task store unavailable: no open database")
	}
	return tasks.New(s.db.DB()).Get(context.Background(), id)
}

// RecentTasks returns recent task records newest-first, re-read live from
// SQLite like Task. The frontend calls it on mount so the visible run
// always reflects the durable rows, never a replayed event.
func (s *ApplicationService) RecentTasks() ([]tasks.Task, error) {
	if s.db == nil {
		return nil, apperror.New(apperror.CodeStoreOpenFailed, "task store unavailable: no open database")
	}
	return tasks.New(s.db.DB()).List(context.Background(), tasks.DefaultListLimit)
}
