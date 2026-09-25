package app

import (
	"context"

	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/discover"
	"github.com/Zendevve/astradew/internal/settings"
	"github.com/Zendevve/astradew/internal/store"
)

// detectCandidates is stubbed in tests so DetectNow never touches the
// machine outside production: fixtures name temp game dirs while CI stays
// headless.
var detectCandidates = discover.Candidates

// DetectNowResult is one automatic discovery pass: the upserted installs,
// whether the pointer outcome needs an explicit choice, and the sentence
// the frontend renders. PointerOutcome reuses the settled vocabulary:
// adopted-vacuum (no pointer before), adopted-stale (dangling pointer
// repaired), kept-healthy (a healthy pointer survives), needs-choice (many
// finds leave the pointer for the per-row choosers). No new error codes.
type DetectNowResult struct {
	Found         []GameInstallView `json:"found"`
	Adopted       *GameInstallView  `json:"adopted"`
	Pointer       string            `json:"pointerOutcome"`
	NeedsChoice   bool              `json:"needsChoice"`
	Message       string            `json:"message"`
	InstallsKnown int               `json:"installsKnown"`
}

// upsertDetected probes one candidate through the same detector the manual
// flow uses, folds in the best-effort last-run log header, and upserts the
// durable row with the candidate's source (steam|gog — never manual for
// auto finds). Detection refusals skip the candidate, never fail the pass.
func (s *ApplicationService) upsertDetected(candidate discover.Candidate) (store.GameInstall, bool) {
	canonical, err := store.CanonicalGamePath(candidate.Path)
	if err != nil {
		return store.GameInstall{}, false
	}
	report, err := probeGameReport(canonical)
	if err != nil {
		return store.GameInstall{}, false
	}
	var smapiExe *string
	if report.SmapiExePath != "" {
		entry := report.SmapiExePath
		smapiExe = &entry
	}
	gameVersion, smapiVersion := versionsToPersist(report)
	install, err := store.UpsertGameInstall(s.db.DB(), canonical, candidate.Source, smapiExe, gameVersion, smapiVersion)
	if err != nil {
		return store.GameInstall{}, false
	}
	return install, true
}

// DetectNow probes every enumerated candidate (Steam roots plus alternates,
// GOG/default locations, the macOS standard bundle) through os.DirFS and
// Detect, records every valid install, and resolves the pointer without
// ever stealing a healthy one. Zero finds leave the pointer untouched and
// guide to manual selection; one find fills an unset or stale pointer only;
// many finds never auto-pick — the pointer stays, the summary reports
// needs-choice, and the per-row choosers decide. A healthy pointer survives
// even a failed pass. A nil store refuses with SETTING_UNAVAILABLE.
func (s *ApplicationService) DetectNow() (DetectNowResult, error) {
	if s.db == nil {
		return DetectNowResult{}, apperror.NewRecoverable(apperror.CodeSettingUnavailable, "settings unavailable", "settings unavailable: service constructed without a database handle")
	}
	ctx := context.Background()
	svc := settings.New(s.db.DB())
	before, err := store.ListGameInstalls(s.db.DB())
	if err != nil {
		return DetectNowResult{}, err
	}
	previousPrimary := s.primaryInstallID(ctx, svc, before)
	raw, _ := svc.Get(ctx, settings.PrimaryGameInstallIDKey)
	rawID, _ := raw.(int)
	stale := rawID >= 1 && previousPrimary == 0

	var valid []store.GameInstall
	for _, candidate := range detectCandidates() {
		if candidate.Source != discover.SourceSteam && candidate.Source != discover.SourceGOG {
			continue
		}
		if install, ok := s.upsertDetected(candidate); ok {
			valid = append(valid, install)
		}
	}

	installs, err := store.ListGameInstalls(s.db.DB())
	if err != nil {
		return DetectNowResult{}, err
	}
	primaryID := s.primaryInstallID(ctx, svc, installs)
	views := make([]GameInstallView, 0, len(valid))
	byID := map[int64]GameInstallView{}
	for _, install := range installs {
		byID[install.ID] = toView(install, primaryID)
	}
	for _, install := range valid {
		if view, ok := byID[install.ID]; ok {
			views = append(views, view)
		}
	}

	switch {
	case len(valid) == 0:
		return DetectNowResult{
			Found:         []GameInstallView{},
			Pointer:       "kept-healthy",
			Message:       "No Stardew Valley install found. Choose the game folder manually.",
			InstallsKnown: len(installs),
		}, nil
	case len(valid) == 1:
		only := valid[0]
		if previousPrimary != 0 {
			return DetectNowResult{
				Found:         views,
				Pointer:       "kept-healthy",
				Message:       "One install found. The current primary is unchanged — use the per-row chooser to switch.",
				InstallsKnown: len(installs),
			}, nil
		}
		s.maybeAdoptPrimary(ctx, svc, only.ID, len(installs))
		primaryID = s.primaryInstallID(ctx, svc, installs)
		adopted := byID[only.ID]
		adopted.IsPrimary = only.ID == primaryID
		for i := range views {
			if views[i].ID == only.ID {
				views[i].IsPrimary = only.ID == primaryID
			}
		}
		outcome := "adopted-vacuum"
		if stale {
			outcome = "adopted-stale"
		}
		return DetectNowResult{
			Found:         views,
			Adopted:       &adopted,
			Pointer:       outcome,
			Message:       "One install found and set as primary.",
			InstallsKnown: len(installs),
		}, nil
	default:
		if previousPrimary != 0 {
			return DetectNowResult{
				Found:         views,
				Pointer:       "kept-healthy",
				NeedsChoice:   true,
				Message:       "Several installs found. The current primary is unchanged — pick one with the per-row chooser.",
				InstallsKnown: len(installs),
			}, nil
		}
		return DetectNowResult{
			Found:         views,
			Pointer:       "needs-choice",
			NeedsChoice:   true,
			Message:       "Several installs found. Pick the primary with the per-row chooser.",
			InstallsKnown: len(installs),
		}, nil
	}
}
