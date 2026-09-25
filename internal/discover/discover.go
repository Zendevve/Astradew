// Package discover finds installed copies of Stardew Valley on the current
// machine. It is the automatic counterpart to manual folder selection:
// enumeration only proposes candidate game directories, and every candidate
// still passes through detect.Detect before anything is recorded.
//
// Enumeration is best-effort and headless-safe: missing files, unmounted
// volumes, and absent registry keys yield fewer candidates, never an error.
package discover

// SourceSteam names candidates derived from Steam libraries.
const SourceSteam = "steam"

// SourceGOG names candidates derived from GOG/default locations.
const SourceGOG = "gog"

// Candidate is one game-directory path worth probing, plus the source the
// durable row is recorded with when detection succeeds. Auto finds are
// never recorded as manual.
type Candidate struct {
	Path   string
	Source string
}
