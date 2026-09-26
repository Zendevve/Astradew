// This file holds the dependency-edge and duplicate-group extraction: pure
// post-processing over a scan's records, never over the filesystem. An Edge is
// one link a Mod Unit declares to another Unique ID — a Dependencies entry or
// a Content Pack's host link — and a DuplicateGroup is one set of folders
// claiming the same Unique ID. Evaluation statuses (missing, disabled, too
// old, optional-absent), Health surfacing, scores, and ordering by severity
// stay out of this file: it reports only what the manifests declare, so the
// Health phase owns every judgment about whether a target is usable.
package manifest

import "strings"

// EdgeKind is which manifest construct declared a dependency edge.
type EdgeKind string

const (
	// EdgeManifestDependency is one entry of a manifest's Dependencies list.
	EdgeManifestDependency EdgeKind = "manifest_dependency"
	// EdgeContentPackHost is the host link a Content Pack declares with
	// ContentPackFor. A Content Pack is inert without its host, so this edge
	// is always required regardless of the manifest's own wording.
	EdgeContentPackHost EdgeKind = "content_pack_host"
)

// Edge is one declared dependency link of a Mod Unit, as authored. It is
// extracted, never evaluated: whether the target exists, is enabled, or is new
// enough is the Health phase's question.
type Edge struct {
	// From is the depending unit's folder path relative to the scan root.
	From string
	// FromUniqueID is the depending unit's own Unique ID, as authored.
	FromUniqueID string
	// Target is the target Unique ID, as authored. Unique IDs compare
	// case-insensitively; folder names never confer identity.
	Target string
	// MinimumVersion is the declared floor as authored; "" means no floor.
	MinimumVersion string
	// Required reports whether the link is mandatory: a Dependencies entry's
	// IsRequired flag, else true for a Content Pack host link.
	Required bool
	// Kind is which manifest construct declared the edge.
	Kind EdgeKind
}

// DuplicateEntry is one Mod Unit among those claiming a duplicate Unique ID.
type DuplicateEntry struct {
	// Path is the unit's folder path relative to the scan root.
	Path string
	// Version is the display form, exactly as the author wrote it.
	Version string
}

// DuplicateGroup is every participating Mod Unit claiming one Unique ID,
// compared case-insensitively. The product treats the same Unique ID in two
// folders as a launch blocker: two copies load under one identity, so neither
// copy can be told apart afterwards.
type DuplicateGroup struct {
	// UniqueID is the first occurrence's spelling, kept as authored.
	UniqueID string
	// Entries are the group's units in scan order.
	Entries []DuplicateEntry
	// LaunchBlocker reports whether more than one unit claims the ID.
	LaunchBlocker bool
}

// participates reports whether a scan record's unit takes part in dependency
// extraction and duplicate grouping: its Manifest parsed and its verdict is not
// invalid. An invalid manifest never loads in SMAPI, so it declares no live
// edge and cannot collide with a loadable identity; records with no unit at all
// (ignored, XNB, empty, manifest-less, unreadable) are not Mod Units.
func participates(unit *Unit) bool {
	return unit != nil && unit.Verdict != VerdictInvalid
}

// Edges extracts the declared dependency links of every participating unit.
// Dependencies entries are emitted in manifest order, then the Content Pack's
// host link last.
func Edges(records []Record) []Edge {
	edges := []Edge{}
	for _, r := range records {
		unit := r.Unit
		if !participates(unit) {
			continue
		}
		for _, dep := range unit.Manifest.Dependencies {
			edges = append(edges, Edge{
				From:           r.Path,
				FromUniqueID:   unit.Manifest.UniqueID,
				Target:         dep.UniqueID,
				MinimumVersion: dep.MinimumVersion,
				Required:       dep.IsRequired,
				Kind:           EdgeManifestDependency,
			})
		}
		if host := unit.Manifest.ContentPackFor; host != nil {
			edges = append(edges, Edge{
				From:           r.Path,
				FromUniqueID:   unit.Manifest.UniqueID,
				Target:         host.UniqueID,
				MinimumVersion: host.MinimumVersion,
				Required:       true,
				Kind:           EdgeContentPackHost,
			})
		}
	}
	return edges
}

// DuplicateGroups groups every participating unit by Unique ID compared
// case-insensitively, the way SMAPI and the product compare identities. Folder
// names never confer identity, so differently named folders with the same ID are
// one group. Groups come in first-occurrence order and entries keep scan order,
// each carrying its path plus the author's display version; LaunchBlocker marks
// the multi-entry groups the product must surface before launch.
func DuplicateGroups(records []Record) []DuplicateGroup {
	groups := []DuplicateGroup{}
	first := map[string]int{}
	for _, r := range records {
		unit := r.Unit
		if !participates(unit) {
			continue
		}
		key := strings.ToLower(unit.Manifest.UniqueID)
		i, seen := first[key]
		if !seen {
			i = len(groups)
			first[key] = i
			groups = append(groups, DuplicateGroup{UniqueID: unit.Manifest.UniqueID})
		}
		groups[i].Entries = append(groups[i].Entries, DuplicateEntry{
			Path:    r.Path,
			Version: unit.Manifest.Version,
		})
	}
	for i := range groups {
		groups[i].LaunchBlocker = len(groups[i].Entries) > 1
	}
	return groups
}
