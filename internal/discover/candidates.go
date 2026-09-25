package discover

import "sort"

// Candidates enumerates every game-directory path worth probing on this
// machine: Steam libraries (default plus libraryfolders.vdf alternates),
// GOG/default locations, and the macOS standard bundle. Results are
// deduplicated by exact path and sorted for determinism. Missing or
// unmounted locations are included freely — detection skips what is not a
// valid install, so enumeration never stats the disk.
func Candidates() []Candidate {
	seen := map[string]bool{}
	var out []Candidate
	for _, candidate := range append(steamCandidates(), gogCandidates()...) {
		if candidate.Path == "" || seen[candidate.Path] {
			continue
		}
		seen[candidate.Path] = true
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
