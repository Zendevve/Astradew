// This file holds the record side of the mapping: one manifest.Record becomes
// one preview finding, or — for the manifest-less folder the scanner reports —
// the hidden-manifest probe below it. Every reason the scanner can report has a
// case here, so a finding is never invented and a record is never dropped
// silently.
package inspect

import (
	"io/fs"
	"path"
	"strings"

	"github.com/Zendevve/astradew/internal/manifest"
)

// recordFindings maps one non-unit record into the findings it contributes.
// Most records contribute one finding carrying the scanner's own note, so the
// explanation the user reads is the one SMAPI's wording produced and never a
// re-statement here.
func recordFindings(fsys fs.FS, record manifest.Record, opts Options) []Finding {
	switch record.Outcome {
	case manifest.OutcomeXnb:
		return []Finding{{Kind: FindingLegacyXnb, Path: record.Path, Message: record.Note}}
	case manifest.OutcomeInvalid:
		switch record.Reason {
		case manifest.ReasonEmptyFolder:
			return []Finding{{Kind: FindingEmptyFolder, Path: record.Path, Message: record.Note}}
		case manifest.ReasonEmptyVortexFolder:
			return []Finding{{Kind: FindingVortexLeftover, Path: record.Path, Message: record.Note}}
		case manifest.ReasonInstaller:
			return []Finding{{Kind: FindingSMAPIInstaller, Path: record.Path, Message: record.Note}}
		case manifest.ReasonUnreadable:
			return []Finding{{Kind: FindingUnreadable, Path: record.Path, Message: record.Note}}
		case manifest.ReasonManifestMissing:
			// A manifest-less folder of ordinary files is not a unit and not a
			// problem. Only a manifest buried below it — which SMAPI will never
			// read — is worth reporting, and the record itself contributes
			// nothing then: the probe's findings are its whole story.
			return hiddenManifestFindings(fsys, record.Path, opts)
		}
	case manifest.OutcomeIgnored:
		switch record.Reason {
		case manifest.ReasonIgnoredFolder, manifest.ReasonLinkLoop:
			return []Finding{{Kind: FindingIgnoredFolder, Path: record.Path, Message: record.Note}}
		case manifest.ReasonLooseRootFiles:
			return []Finding{{Kind: FindingLooseRootFiles, Path: record.Path, Message: looseRootsMessage(record)}}
		case manifest.ReasonScanLimit:
			return []Finding{{Kind: FindingScanLimit, Path: record.Path, Message: record.Note}}
		}
	}
	// The scanner produces none of the remaining combinations. A record this
	// mapper does not recognize is surfaced rather than dropped: a new reason
	// must be handled explicitly above, and until it is, its own note is the
	// honest thing to show. Unreadable is the closest kind — the record is a
	// thing whose contents the preview could not establish.
	return []Finding{{Kind: FindingUnreadable, Path: record.Path, Message: record.Note}}
}

// looseRootsMessage appends the ignored file names to the scanner's note, so
// the finding names the files the user has to move into folders of their own.
func looseRootsMessage(record manifest.Record) string {
	if len(record.Files) == 0 {
		return record.Note
	}
	return record.Note + " Files: " + strings.Join(record.Files, ", ")
}

// hiddenManifestFindings probes one manifest-less folder for manifests buried
// below it and reports each by its own path. The probe is a bounded walk, so a
// hostile tree cannot make it expensive, and it prunes dot-prefixed folders and
// manager metadata, so a __MACOSX payload beside a stray file never reports a
// phantom mod. Nothing here becomes a unit: the preview reports, it never
// repairs.
func hiddenManifestFindings(fsys fs.FS, folder string, opts Options) []Finding {
	findings := []Finding{}
	walkBounded(fsys, folder, func(rel string) bool {
		if !manifest.IsManifestName(path.Base(rel), opts.CaseInsensitivePaths) {
			return true
		}
		findings = append(findings, Finding{
			Kind: FindingHiddenManifests, Path: rel, Message: hiddenManifestNote,
		})
		return true
	})
	return findings
}
