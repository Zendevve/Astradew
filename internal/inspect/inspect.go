// Package inspect derives the install preview for one staged package tree: the
// Mod Units an extracted archive holds, the layout findings that explain what
// the user is looking at, the duplicate Unique IDs that would block a launch,
// and whether the package can be installed at all.
//
// The seam is an fs.FS rooted at the staged package — the same read-only shape
// internal/manifest scans and the archive layer extracts into. Inspection is
// pure derivation: it reads the tree, never writes, repairs, or rewrites it,
// and never invents a Mod Unit the files do not support. Everything that cannot
// be read becomes a finding instead, so Inspect has no error return and its
// answer is always complete and honest about the tree it saw.
//
// Classification belongs to internal/manifest. Inspect runs manifest.Scan and
// maps its records, one at a time, into a Unit or a Finding. What it adds on
// top is the root pass — the layouts the scanner itself cannot report, because
// its root loop only descends into children and never treats the scanned root
// as a mod folder — and the package-level judgments the scanner deliberately
// leaves out: the legacy-XNB mark, launch blockers, and installability.
//
// Deliberate choices, and why:
//
//   - The stage root's own manifest.json is a Mod Unit. Its RelativePath is ""
//     (the ASCII-empty path, which means the stage root itself), and its
//     FolderName comes from the manifest's Name, because a root manifest ships
//     no folder name — the Vortex convention names the folder from the
//     manifest. PRD §15 Example B is exactly this layout, and the preview must
//     show it. With that unit present the scanner's loose-root-files record is
//     dropped: the manifest is accounted for, and reporting it as ignored loose
//     files would contradict the unit. A root manifest that cannot be read
//     becomes an unreadable finding at the manifest's path instead.
//   - Wrapper folders stay. Unit paths come straight from the scanner records,
//     with FolderName = path.Base(RelativePath): nothing is stripped, renamed,
//     or guessed, so the library installs the tree the archive actually holds
//     (ADR 0007).
//   - A manifest-less folder is probed with one bounded walk for manifests
//     buried below it. Each one becomes a hidden-manifests finding with re-pack
//     guidance, and none becomes a unit: the preview reports a poisoned
//     organizer — a stray file beside nested mod folders, which SMAPI reads as
//     one broken folder — it never repairs one. A folder holding ordinary files
//     and no manifest at all contributes no finding: it is simply not a unit,
//     and the preview shows the package as holding nothing usable.
//   - Manager metadata (__MACOSX, ._*, .DS_Store, mcs, desktop.ini, Thumbs.db,
//     the Vortex markers) stays pruned exactly as the scanner prunes it: no
//     finding, ever. Dot-prefixed folders stay ignored folders, not junk.
//   - Both root-pass walks are bounded (walkEntryLimit entries examined,
//     walkDepthLimit levels below the folder they start from). A walk cut short
//     by a bound reports nothing, which is the honest answer to "found nothing"
//     under a bounded search. The opposite rule applies to the scan itself: a
//     scan that stopped at a bound is reported as a scan-limit finding and the
//     package is not installable, because a bounded scan cannot claim the
//     package is complete.
//   - Duplicate groups of one entry are dropped. manifest.DuplicateGroups
//     reports every participating unit as a group, and the preview's duplicate
//     section is about duplicates: a group of one is not one.
//   - Every slice of the returned Preview — and every slice inside a Unit — is
//     non-nil, so the wire form carries [] rather than null.
package inspect

import (
	"io/fs"
	"path"
	"strings"

	"github.com/Zendevve/astradew/internal/manifest"
)

// Options configures one inspection.
type Options struct {
	// CaseInsensitivePaths mirrors manifest.Options.CaseInsensitivePaths: when
	// set, a mis-cased manifest.json still identifies a Mod Unit, at the stage
	// root and in every folder the scan reads. SMAPI defaults it on for Linux
	// and Android and off elsewhere; the zero value is off.
	CaseInsensitivePaths bool
}

// Kind is what a staged folder is, as the preview shows it.
type Kind string

const (
	// KindCodeMod is a Mod Unit SMAPI loads directly.
	KindCodeMod Kind = "code-mod"
	// KindContentPack is a Mod Unit consumed by its host mod.
	KindContentPack Kind = "content-pack"
	// KindInvalid is a folder whose manifest failed validation: it keeps
	// everything the author wrote, but it is not loadable.
	KindInvalid Kind = "invalid"
)

// Unit is one Mod Unit the staged package holds, with everything the preview
// and the confirmation step show about it.
type Unit struct {
	// RelativePath is the unit's folder relative to the stage root,
	// slash-separated. "" is the stage root itself, which happens when the
	// package's own manifest.json sits at its top level (PRD §15 Example B).
	RelativePath string `json:"relativePath"`
	// FolderName is the folder's name — path.Base(RelativePath) — or, for a
	// root manifest, the manifest's Name, since the folder that will hold the
	// installed unit takes its name from the manifest.
	FolderName string `json:"folderName"`
	// Name, Author, Version and UniqueID are the manifest's identity as
	// authored; an invalid manifest keeps them rather than dropping them.
	Name     string `json:"name"`
	Author   string `json:"author"`
	Version  string `json:"version"`
	UniqueID string `json:"uniqueID"`
	// Kind is the unit's type, or KindInvalid when its manifest failed.
	Kind Kind `json:"kind"`
	// EntryDll is the code mod's entry assembly, "" for a content pack.
	EntryDll string `json:"entryDll"`
	// ContentPackFor is the host Unique ID a content pack declares, "" for a
	// code mod.
	ContentPackFor string `json:"contentPackFor"`
	// Dependencies are the declared dependency links, in manifest order.
	Dependencies []Dependency `json:"dependencies"`
	// Verdict is the manifest parser's judgment.
	Verdict manifest.Verdict `json:"verdict"`
	// FieldErrors are the parser's per-field diagnoses, in parser order.
	FieldErrors []FieldError `json:"fieldErrors"`
	// SystemMod reports whether the Unique ID names one of SMAPI's bundled
	// Mod Units.
	SystemMod bool `json:"systemMod"`
	// Installable reports whether this unit itself can be installed: its
	// verdict parsed and its kind is a loadable one.
	Installable bool `json:"installable"`
	// Note explains a unit that is not installable; "" otherwise.
	Note string `json:"note"`
}

// Dependency is one declared dependency link, as authored. It is reported,
// never evaluated: whether the target exists or is new enough is the Health
// phase's question.
type Dependency struct {
	UniqueID       string `json:"uniqueID"`
	MinimumVersion string `json:"minimumVersion"`
	IsRequired     bool   `json:"isRequired"`
}

// FieldError is one diagnosed manifest problem, mapped from the parser's own
// error so the preview never re-states a message.
type FieldError struct {
	Field   string         `json:"field"`
	Stage   manifest.Stage `json:"stage"`
	Message string         `json:"message"`
}

// FindingKind is what one layout finding is about.
type FindingKind string

const (
	// FindingLegacyXnb is legacy packed content that can never be a Mod Unit.
	// The per-folder finding carries the scanner's explanation; the
	// package-level finding carries PRD §16's copy verbatim.
	FindingLegacyXnb FindingKind = "legacy-xnb"
	// FindingRootContentTree is a top-level Content directory of packed
	// content: game-folder content, not a mod.
	FindingRootContentTree FindingKind = "root-content-tree"
	// FindingSMAPIInstaller is the SMAPI installer bundle a user unzipped into
	// the package by mistake.
	FindingSMAPIInstaller FindingKind = "smapi-installer"
	// FindingSMAPIPayload is the extracted installer's own payload directory.
	FindingSMAPIPayload FindingKind = "smapi-payload"
	// FindingHiddenManifests is a manifest buried below a folder SMAPI reads
	// as manifest-less, the shape a poisoned organizer makes.
	FindingHiddenManifests FindingKind = "hidden-manifests"
	// FindingEmptyFolder is a folder with nothing SMAPI considers.
	FindingEmptyFolder FindingKind = "empty-folder"
	// FindingIgnoredFolder is a folder SMAPI skips by convention: a dot-prefixed
	// name, or a folder that links back to one already scanned.
	FindingIgnoredFolder FindingKind = "ignored-folder"
	// FindingVortexLeftover is a folder another manager left behind holding
	// only its marker.
	FindingVortexLeftover FindingKind = "vortex-leftover"
	// FindingLooseRootFiles is mod-looking files directly in the stage root,
	// which SMAPI never treats as a mod.
	FindingLooseRootFiles FindingKind = "loose-root-files"
	// FindingUnreadable is a folder or manifest the filesystem refused to read.
	FindingUnreadable FindingKind = "unreadable"
	// FindingScanLimit is where the scan's work bound ran out: everything below
	// it is unknown, so the package cannot be judged complete.
	FindingScanLimit FindingKind = "scan-limit"
)

// Finding is one thing the preview reports about the staged tree that is not a
// Mod Unit.
type Finding struct {
	Kind    FindingKind `json:"kind"`
	Path    string      `json:"path"`
	Message string      `json:"message"`
}

// DuplicateEntry is one unit among those claiming a duplicate Unique ID.
type DuplicateEntry struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

// DuplicateGroup is one Unique ID claimed by more than one unit. SMAPI loads
// both copies under one identity, so the product treats the group as a launch
// blocker: after launch the two copies cannot be told apart.
type DuplicateGroup struct {
	UniqueID      string           `json:"uniqueID"`
	Entries       []DuplicateEntry `json:"entries"`
	LaunchBlocker bool             `json:"launchBlocker"`
}

// Preview is the whole answer about one staged package.
type Preview struct {
	// Units are the Mod Units found, the stage root's own unit first when the
	// package has one, then the scanner's units in scan order.
	Units []Unit `json:"units"`
	// Findings are the layout findings, record-derived ones in scan order
	// first, then the root pass's own, then the package-level XNB mark last.
	Findings []Finding `json:"findings"`
	// Duplicates are the multi-entry Unique ID groups only.
	Duplicates []DuplicateGroup `json:"duplicates"`
	// Installable reports whether Astradew can offer to install the package:
	// at least one installable unit, and no scan-limit finding.
	Installable bool `json:"installable"`
}

// Inspect derives the install preview for the package staged at fsys. It is
// read-only and total: it has no error return, because every unreadable or
// unrecognizable thing is part of the answer — a finding — rather than a reason
// to refuse to answer. The returned Preview's slices are always non-nil, so the
// wire form carries [] rather than null.
func Inspect(fsys fs.FS, opts Options) Preview {
	// The scanner is the classification authority: run it and map its records.
	records := manifest.Scan(fsys, manifest.Options{CaseInsensitivePaths: opts.CaseInsensitivePaths})
	root := inspectRoot(fsys, opts)

	units := []Unit{}
	if root.unit != nil {
		units = append(units, unitFromManifest(root.unit, "", root.folderName))
	}

	// A root Content tree is reported as one finding in place of every record
	// the scanner derived from that tree, and only when the tree is the
	// package's whole story: with a unit present, the tree is one more thing
	// the preview reports rather than the thing it is about.
	contentTree := root.contentXnb && root.unit == nil && !hasUnitRecord(records)

	findings := []Finding{}
	emitted := false
	for _, record := range records {
		if contentTree && underTree(record.Path, root.contentDir) {
			if !emitted {
				findings = append(findings, rootContentTreeFinding())
				emitted = true
			}
			continue
		}
		// The root unit accounts for a root manifest, so the scanner's
		// loose-root-files record — which would call that same manifest
		// ignored — has nothing left to say.
		if root.unit != nil && record.Outcome == manifest.OutcomeIgnored && record.Reason == manifest.ReasonLooseRootFiles {
			continue
		}
		if isUnitRecord(record) {
			units = append(units, unitFromRecord(record))
			continue // the unit carries the record's story; a finding would repeat it
		}
		findings = append(findings, recordFindings(fsys, record, opts)...)
	}
	if contentTree && !emitted {
		// A work bound can end the scan before it reaches the tree. The tree is
		// still the package's whole story, so the finding must not depend on a
		// record that may never arrive.
		findings = append(findings, rootContentTreeFinding())
	}
	if root.unreadable != nil {
		findings = append(findings, *root.unreadable)
	}
	if root.payload {
		findings = append(findings, Finding{
			Kind: FindingSMAPIPayload, Path: smapiPayloadDir, Message: smapiPayloadNote,
		})
	}
	if holdsXnb(records, root) && !anyInstallable(units) {
		// The package-level mark: the per-folder findings say why each XNB
		// folder can't load, and this one says the package as a whole replaces
		// game content, in PRD §16's own words.
		findings = append(findings, Finding{Kind: FindingLegacyXnb, Path: "", Message: legacyXnbNotice})
	}

	installable := anyInstallable(units)
	if installable {
		for _, finding := range findings {
			if finding.Kind == FindingScanLimit {
				installable = false
				break
			}
		}
	}

	return Preview{
		Units:       units,
		Findings:    findings,
		Duplicates:  duplicateGroups(records, root.unit),
		Installable: installable,
	}
}

// holdsXnb reports whether the package holds packed content: an XNB record from
// the scan, or a root Content tree the scanner's records never reach.
func holdsXnb(records []manifest.Record, root rootPass) bool {
	if root.contentXnb {
		return true
	}
	for _, record := range records {
		if record.Outcome == manifest.OutcomeXnb {
			return true
		}
	}
	return false
}

// anyInstallable reports whether at least one unit can be installed.
func anyInstallable(units []Unit) bool {
	for _, unit := range units {
		if unit.Installable {
			return true
		}
	}
	return false
}

// hasUnitRecord reports whether any record becomes a unit.
func hasUnitRecord(records []manifest.Record) bool {
	for _, record := range records {
		if isUnitRecord(record) {
			return true
		}
	}
	return false
}

// duplicateGroups surfaces the multi-entry groups manifest.DuplicateGroups
// found. The root unit joins the grouping as a synthetic record placed first,
// so it collides like any other unit and lands first in its group, matching its
// place in the units list. Groups of one entry are dropped: DuplicateGroups
// reports every participating unit as a group, and a group of one is not a
// duplicate.
func duplicateGroups(records []manifest.Record, rootUnit *manifest.Unit) []DuplicateGroup {
	grouped := records
	if rootUnit != nil {
		grouped = append([]manifest.Record{{Path: "", Outcome: rootOutcome(rootUnit), Unit: rootUnit}}, records...)
	}
	groups := []DuplicateGroup{}
	for _, group := range manifest.DuplicateGroups(grouped) {
		if len(group.Entries) < 2 {
			continue
		}
		entries := make([]DuplicateEntry, 0, len(group.Entries))
		for _, entry := range group.Entries {
			entries = append(entries, DuplicateEntry{Path: entry.Path, Version: entry.Version})
		}
		groups = append(groups, DuplicateGroup{
			UniqueID:      group.UniqueID,
			Entries:       entries,
			LaunchBlocker: group.LaunchBlocker,
		})
	}
	return groups
}

// rootOutcome is the synthetic record's outcome for the root-manifest unit: one
// of the two outcomes the scanner uses for units, so the record reads like any
// other record the grouping sees.
func rootOutcome(unit *manifest.Unit) manifest.Outcome {
	if unit.Manifest.ContentPackFor != nil {
		return manifest.OutcomeContentPack
	}
	return manifest.OutcomeSmapi
}

// isUnitRecord reports whether a scanner record becomes a Unit: a parsed Mod
// Unit, or the folder whose manifest failed validation. Every other record is a
// finding.
func isUnitRecord(record manifest.Record) bool {
	if record.Unit == nil {
		return false
	}
	switch record.Outcome {
	case manifest.OutcomeSmapi, manifest.OutcomeContentPack:
		return true
	case manifest.OutcomeInvalid:
		return record.Reason == manifest.ReasonManifestInvalid
	}
	return false
}

// unitFromRecord maps one unit-bearing record. The record's note is the reason
// its verdict failed; a note never accompanies a loadable record.
func unitFromRecord(record manifest.Record) Unit {
	unit := unitFromManifest(record.Unit, record.Path, path.Base(record.Path))
	if record.Note != "" {
		unit.Note = record.Note
	}
	return unit
}

// unitFromManifest maps one parsed manifest.Unit into the preview's Unit.
// relPath is the unit's folder relative to the stage root ("" for the root
// itself) and folderName the name the preview shows for it. The invalid note is
// derived the same way the scanner derives its record note — the last error
// names the failure, because Parse appends failures after warnings — so a unit
// that never went through a record (the stage root's own manifest) reads the
// same as one that did.
func unitFromManifest(unit *manifest.Unit, relPath, folderName string) Unit {
	m := unit.Manifest
	kind := KindCodeMod
	if m.ContentPackFor != nil {
		kind = KindContentPack
	}
	note := ""
	if unit.Verdict == manifest.VerdictInvalid {
		// A manifest that failed validation is not a loadable Mod Unit,
		// whatever it declares.
		kind = KindInvalid
		note = "its manifest is invalid."
		if n := len(unit.Errors); n > 0 {
			note = "parsing its manifest failed: " + unit.Errors[n-1].Message
		}
	}
	host := ""
	if m.ContentPackFor != nil {
		host = m.ContentPackFor.UniqueID
	}
	dependencies := make([]Dependency, 0, len(m.Dependencies))
	for _, dependency := range m.Dependencies {
		dependencies = append(dependencies, Dependency{
			UniqueID:       dependency.UniqueID,
			MinimumVersion: dependency.MinimumVersion,
			IsRequired:     dependency.IsRequired,
		})
	}
	fieldErrors := make([]FieldError, 0, len(unit.Errors))
	for _, fieldError := range unit.Errors {
		fieldErrors = append(fieldErrors, FieldError{
			Field:   fieldError.Field,
			Stage:   fieldError.Stage,
			Message: fieldError.Message,
		})
	}
	return Unit{
		RelativePath:   relPath,
		FolderName:     folderName,
		Name:           m.Name,
		Author:         m.Author,
		Version:        m.Version,
		UniqueID:       m.UniqueID,
		Kind:           kind,
		EntryDll:       m.EntryDll,
		ContentPackFor: host,
		Dependencies:   dependencies,
		Verdict:        unit.Verdict,
		FieldErrors:    fieldErrors,
		SystemMod:      unit.SystemMod,
		Installable:    kind == KindCodeMod || kind == KindContentPack,
		Note:           note,
	}
}

// underTree reports whether a record path is dir itself or sits below it,
// comparing case-insensitively: the directory was matched case-insensitively,
// and a package may spell it either way.
func underTree(relPath, dir string) bool {
	if dir == "" || len(relPath) < len(dir) {
		return false
	}
	if !strings.EqualFold(relPath[:len(dir)], dir) {
		return false
	}
	return len(relPath) == len(dir) || relPath[len(dir)] == '/'
}
