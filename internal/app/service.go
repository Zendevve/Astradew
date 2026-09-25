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
	"path/filepath"
	"strings"

	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/approot"
	"github.com/Zendevve/astradew/internal/detect"
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

// HealthGameSection is the observed primary Game Installation. Null on the
// wire until the primary pointer resolves to a row: path and source name
// the install, installsKnown counts game_installs rows, and a null
// gameVersion means the game is present but its version is unknown — never
// guessed.
type HealthGameSection struct {
	Path          string  `json:"path"`
	Source        string  `json:"source"`
	GameVersion   *string `json:"gameVersion"`
	InstallsKnown int     `json:"installsKnown"`
}

// HealthSmapiSection is the observed SMAPI state inside the primary Game
// Installation. Null until a game is observed (SMAPI is unobservable
// without a game dir); an absent SMAPI is an observed state, never null.
// State repeats the detect.SmapiStatus vocabulary verbatim
// (absent|complete|partial). A null version means unknown — or conflicted,
// which trusts none of its sources and stores nothing. Detail is one honest
// sentence (missing list, conflict sources, last-run-only caveat). Missing
// names the absent signals when partial, else empty, never null.
type HealthSmapiSection struct {
	State   string   `json:"state"`
	Version *string  `json:"version"`
	Detail  string   `json:"detail"`
	Missing []string `json:"missing"`
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
	Game           *HealthGameSection  `json:"game"`
	Smapi          *HealthSmapiSection `json:"smapi"`
}

// unavailableCapabilities lists what this phase cannot do yet. Every entry
// carries status "unavailable" with its reason — never "healthy".
func unavailableCapabilities() []HealthUnavailable {
	return []HealthUnavailable{
		{Name: "Game detection", Status: "unavailable", Reason: "no primary game install observed yet — pick the game folder in Settings"},
		{Name: "SMAPI detection", Status: "unavailable", Reason: "SMAPI is unobservable without an observed game install"},
		{Name: "Mod health", Status: "unavailable", Reason: "mod health checks are not yet implemented in this phase; they run once a game with mods is configured"},
	}
}

// dropObservedUnavailable drops the Game/SMAPI unavailable entries exactly
// when the corresponding section is observed. Mod health stays unavailable
// for all of Phase 1, so "Not yet available" never renders empty.
func dropObservedUnavailable(entries []HealthUnavailable, gameObserved, smapiObserved bool) []HealthUnavailable {
	kept := make([]HealthUnavailable, 0, len(entries))
	for _, entry := range entries {
		if gameObserved && entry.Name == "Game detection" {
			continue
		}
		if smapiObserved && entry.Name == "SMAPI detection" {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}
// smapiLogDirCandidates names the OS ErrorLogs directories a last-run
// SMAPI-latest.txt may live in, highest-priority first. It is a stubbable
// variable so tests never depend on ambient machine state: the default
// consults the OS user-config locations, while tests point it at temp dirs.
var smapiLogDirCandidates = defaultSmapiLogDirCandidates

// defaultSmapiLogDirCandidates reports the per-OS SMAPI log locations: the
// user-config StardewValley/ErrorLogs dir (Windows %APPDATA%, Unix
// $XDG_CONFIG_HOME or ~/.config) plus the Xbox-app LocalCache variant. All
// are best-effort hints — absent dirs simply yield no header.
func defaultSmapiLogDirCandidates() []string {
	var dirs []string
	if config, err := os.UserConfigDir(); err == nil && config != "" {
		dirs = append(dirs, filepath.Join(config, "StardewValley", "ErrorLogs"))
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		dirs = append(dirs, filepath.Join(xdg, "StardewValley", "ErrorLogs"))
	} else if home := os.Getenv("HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, ".config", "StardewValley", "ErrorLogs"))
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		matches, _ := filepath.Glob(filepath.Join(local, "Packages", "ConcernedApe.StardewValleyPC_*", "LocalCache", "StardewValley", "ErrorLogs"))
		dirs = append(dirs, matches...)
	}
	return dirs
}

// probeLogHeader tries each candidate ErrorLogs dir via os.DirFS and
// returns the first parsable SMAPI-latest.txt header. Every failure
// degrades to unknown (ok=false), never an error: the last run is a
// fallback source, never a refusal.
func probeLogHeader() (smapi, game string, ok bool) {
	for _, dir := range smapiLogDirCandidates() {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		if smapi, game, ok := detect.ReadLogHeader(os.DirFS(dir)); ok {
			return smapi, game, ok
		}
	}
	return "", "", false
}

// probeGameReport detects the game dir at canonical and folds in the
// best-effort last-run log header. Detection refusals (GAME_*) return
// untouched — callers surface them, never a fabricated report.
func probeGameReport(canonical string) (detect.Report, error) {
	report, err := detect.Detect(os.DirFS(canonical))
	if err != nil {
		return detect.Report{}, err
	}
	if smapi, game, ok := probeLogHeader(); ok {
		report.ApplyLogHeader(smapi, game)
	}
	return report, nil
}

// versionsToPersist maps a detection report onto the nullable row columns:
// gameVersion is nil exactly when the game version is unknown; smapiVersion
// is nil when unknown OR conflicted — a conflict trusts none of its
// sources, so nothing is stored. A log-derived resolution is stored
// plainly (the row has no provenance column by #17): whether a version came
// from disk or the last run is re-derived by a fresh probe at Health time
// (see observePrimaryGame); when no fresh probe is possible the fallback
// sections say so instead of claiming on-disk truth (see sectionsFromRow).
func versionsToPersist(report detect.Report) (gameVersion, smapiVersion *string) {
	if report.GameVersion != "" {
		gameVersion = &report.GameVersion
	}
	if report.Version.Conflict == "" && report.Version.Resolved != "" {
		smapiVersion = &report.Version.Resolved
	}
	return gameVersion, smapiVersion
}

// sectionsFromReport renders the observed Health game/SMAPI sections from a
// fresh probe of the primary install. The SMAPI detail comes from
// Versions.Detail(); when the game version itself fell back to the last-run
// log, the game caveat is appended so a log-derived game version is never
// presented as installed truth.
func sectionsFromReport(row store.GameInstall, report detect.Report, installsKnown int) (*HealthGameSection, *HealthSmapiSection) {
	game := &HealthGameSection{
		Path:          row.Path,
		Source:        row.Source,
		InstallsKnown: installsKnown,
	}
	if report.GameVersion != "" {
		version := report.GameVersion
		game.GameVersion = &version
	}
	detail := report.Version.Detail()
	if report.GameFromLog && report.GameVersion != "" {
		detail += "; game version from the last SMAPI run, not installed truth"
	}
	smapi := &HealthSmapiSection{
		State:   string(report.Smapi),
		Detail:  detail,
		Missing: append([]string{}, report.Missing...),
	}
	if report.Version.Conflict == "" && report.Version.Resolved != "" {
		version := report.Version.Resolved
		smapi.Version = &version
	}
	return game, smapi
}

// sectionsFromRow renders Health sections from the durable row alone when a
// fresh probe fails: stored versions (nil = unknown), SMAPI state from the
// stored entry point (present → complete, absent → absent, mirroring
// toView), and an honest detail. Missing stays empty, never null.
func sectionsFromRow(row store.GameInstall, installsKnown int) (*HealthGameSection, *HealthSmapiSection) {
	game := &HealthGameSection{
		Path:          row.Path,
		Source:        row.Source,
		GameVersion:   row.GameVersion,
		InstallsKnown: installsKnown,
	}
	state := "absent"
	if row.SmapiExePath != nil {
		state = "complete"
	}
	smapi := &HealthSmapiSection{State: state, Version: row.SmapiVersion, Detail: "SMAPI version unknown", Missing: []string{}}
	if row.SmapiVersion != nil {
		smapi.Detail = "SMAPI " + *row.SmapiVersion + " last recorded; on-disk state unobservable right now"
	} else if state == "absent" {
		smapi.Detail = "No SMAPI detected — the game runs unmodded"
	}
	return game, smapi
}

// observePrimaryGame resolves the primary Game Installation row and
// fresh-probes it: Detect over os.DirFS plus the best-effort last-run log
// header. Any failure (no store, no primary, vanished dir, detection
// refusal) falls back to the row values or to unobserved (nil sections) —
// Health never fails and never reports a guess. NO new findings here:
// stale/partial/conflict/mismatch findings belong to #27.
func (s *ApplicationService) observePrimaryGame() (*HealthGameSection, *HealthSmapiSection) {
	if s.db == nil {
		return nil, nil
	}
	ctx := context.Background()
	svc := settings.New(s.db.DB())
	installs, err := store.ListGameInstalls(s.db.DB())
	if err != nil || len(installs) == 0 {
		return nil, nil
	}
	primaryID := s.primaryInstallID(ctx, svc, installs)
	if primaryID == 0 {
		return nil, nil
	}
	var row store.GameInstall
	for _, install := range installs {
		if install.ID == primaryID {
			row = install
			break
		}
	}
	if row.ID == 0 {
		return nil, nil
	}
	canonical, err := store.CanonicalGamePath(row.Path)
	if err != nil {
		canonical = row.Path
	}
	report, err := probeGameReport(canonical)
	if err != nil {
		return sectionsFromRow(row, len(installs))
	}
	return sectionsFromReport(row, report, len(installs))
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
	game, smapi := s.observePrimaryGame()
	report.Game, report.Smapi = game, smapi
	report.Unavailable = dropObservedUnavailable(report.Unavailable, game != nil, smapi != nil)
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

// GameInstallView is one game_installs row plus its primary marker. Nullable
// columns arrive as nil: null means present-but-unknown (versions) or
// not-yet-detected/absent (entry point), never guessed.
type GameInstallView struct {
	ID           int64   `json:"id"`
	Path         string  `json:"path"`
	Source       string  `json:"source"`
	SmapiExePath *string `json:"smapiExePath"`
	GameVersion  *string `json:"gameVersion"`
	SmapiVersion *string `json:"smapiVersion"`
	SmapiState   string  `json:"smapiState"`
	IsPrimary    bool    `json:"isPrimary"`
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
	if key == settings.PrimaryGameInstallIDKey {
		return apperror.NewRecoverable(apperror.CodeSettingInvalid, fmt.Sprintf("invalid value for setting %q", key), fmt.Sprintf("setting %q: the primary install pointer is set only through the installs chooser (SetPrimaryGameInstall); raw numeric editing is refused", key))
	}
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

// primaryInstallID resolves the primary-game-install-id pointer at read/use
// time: the stored id when it names an existing row, else 0 meaning no
// primary. A dangling pointer degrades to "no primary", never to a wrong
// install; row existence is resolved here, never at Set.
func (s *ApplicationService) primaryInstallID(ctx context.Context, svc *settings.Service, installs []store.GameInstall) int64 {
	raw, err := svc.Get(ctx, settings.PrimaryGameInstallIDKey)
	if err != nil {
		return 0
	}
	id, ok := raw.(int)
	if !ok || id < 1 {
		return 0
	}
	for _, install := range installs {
		if install.ID == int64(id) {
			return int64(id)
		}
	}
	return 0
}

// toView renders one row as a GameInstallView: nullable columns arrive as
// nil (unknown by contract), smapiState from the stored entry point
// (present → complete, absent → absent; partial is a detector observation
// #27 surfaces from a fresh probe, never stored).
func toView(install store.GameInstall, primaryID int64) GameInstallView {
	state := "absent"
	if install.SmapiExePath != nil {
		state = "complete"
	}
	return GameInstallView{
		ID:           install.ID,
		Path:         install.Path,
		Source:       install.Source,
		SmapiExePath: install.SmapiExePath,
		GameVersion:  install.GameVersion,
		SmapiVersion: install.SmapiVersion,
		SmapiState:   state,
		IsPrimary:    install.ID == primaryID,
	}
}

// GameInstalls lists every known game installation with its primary marker.
// A nil store refuses with recoverable SETTING_UNAVAILABLE, never an empty
// list fabrication.
func (s *ApplicationService) GameInstalls() ([]GameInstallView, error) {
	if s.db == nil {
		return nil, apperror.NewRecoverable(apperror.CodeSettingUnavailable, "settings unavailable", "settings unavailable: service constructed without a database handle")
	}
	ctx := context.Background()
	installs, err := store.ListGameInstalls(s.db.DB())
	if err != nil {
		return nil, err
	}
	primaryID := s.primaryInstallID(ctx, settings.New(s.db.DB()), installs)
	out := make([]GameInstallView, 0, len(installs))
	for _, install := range installs {
		out = append(out, toView(install, primaryID))
	}
	return out, nil
}

// SetPrimaryGameInstall points the primary at an existing row. An unknown id
// refuses with recoverable SETTING_INVALID and the stored value is left
// untouched. A nil store refuses with SETTING_UNAVAILABLE.
func (s *ApplicationService) SetPrimaryGameInstall(id int64) error {
	if s.db == nil {
		return apperror.NewRecoverable(apperror.CodeSettingUnavailable, "settings unavailable", "settings unavailable: service constructed without a database handle")
	}
	ctx := context.Background()
	if _, err := store.GetGameInstall(s.db.DB(), id); err != nil {
		return apperror.NewRecoverable(apperror.CodeSettingInvalid, fmt.Sprintf("no known install with id %d", id), fmt.Sprintf("no known install with id %d; pointer unchanged", id))
	}
	return settings.New(s.db.DB()).Set(ctx, settings.PrimaryGameInstallIDKey, json.RawMessage(fmt.Sprintf("%d", id)))
}

// maybeAdoptPrimary sets the pointer to id when it is unset, stale (names a
// missing row), or this is the only row — NEVER stealing a healthy pointer.
func (s *ApplicationService) maybeAdoptPrimary(ctx context.Context, svc *settings.Service, id int64, rowCount int) {
	raw, err := svc.Get(ctx, settings.PrimaryGameInstallIDKey)
	if err != nil {
		return
	}
	current, ok := raw.(int)
	if !ok {
		return
	}
	if current >= 1 && rowCount > 1 {
		if _, err := store.GetGameInstall(s.db.DB(), int64(current)); err == nil {
			return
		}
	}
	_ = svc.Set(ctx, settings.PrimaryGameInstallIDKey, json.RawMessage(fmt.Sprintf("%d", id)))
}

// AddGameInstall canonicalises path Go-side, probes os.DirFS(path) through
// the same detector the automatic pass uses, folds in the best-effort
// last-run log header, and upserts the durable row: re-picks refresh instead
// of duplicating (versions included), an unknown version persists as nil,
// and a second install never steals a healthy primary. A SMAPI conflict
// trusts none of its sources and persists nothing. Refusals carry the typed
// GAME_* codes with per-code recovery copy. A nil store refuses with
// SETTING_UNAVAILABLE.
func (s *ApplicationService) AddGameInstall(path string) (GameInstallView, error) {
	if s.db == nil {
		return GameInstallView{}, apperror.NewRecoverable(apperror.CodeSettingUnavailable, "settings unavailable", "settings unavailable: service constructed without a database handle")
	}
	canonical, err := store.CanonicalGamePath(path)
	if err != nil {
		return GameInstallView{}, apperror.NewRecoverable(apperror.CodeGameInvalid, "game folder is unreadable", fmt.Sprintf("game folder is unreadable: %s", err))
	}
	report, err := probeGameReport(canonical)
	if err != nil {
		return GameInstallView{}, err
	}
	var smapiExe *string
	if report.SmapiExePath != "" {
		entry := report.SmapiExePath
		smapiExe = &entry
	}
	gameVersion, smapiVersion := versionsToPersist(report)
	ctx := context.Background()
	install, err := store.UpsertGameInstall(s.db.DB(), canonical, "manual", smapiExe, gameVersion, smapiVersion)
	if err != nil {
		return GameInstallView{}, err
	}
	svc := settings.New(s.db.DB())
	installs, err := store.ListGameInstalls(s.db.DB())
	if err != nil {
		return GameInstallView{}, err
	}
	s.maybeAdoptPrimary(ctx, svc, install.ID, len(installs))
	return toView(install, s.primaryInstallID(ctx, svc, installs)), nil
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
