package discover

import (
	"os"
	"path/filepath"
	"runtime"
)

// gogGameDirs is stubbed in tests so GOG enumeration never touches the
// machine outside production.
var gogGameDirs = defaultGOGGameDirs

// defaultGOGGameDirs reports the GOG/default game directories on this
// machine: registry-located copies plus installer defaults on Windows, the
// standard bundle inner dir on macOS, and the offline-installer default on
// Linux. The Xbox-app layout is manual-only by feasibility verdict and is
// never proposed.
func defaultGOGGameDirs() []string {
	var home string
	if dir, err := userHomeDir(); err == nil {
		home = dir
	}
	return gogGameDirsFor(runtime.GOOS, home, gogPathsFromRegistry())
}

// gogGameDirsFor is the pure OS table behind defaultGOGGameDirs. The macOS
// entry points at the bundle's game dir (Contents/MacOS inside
// /Applications/Stardew Valley.app), never the .app wrapper itself. POSIX
// joins use forward slashes so the table is testable on any host.
func gogGameDirsFor(goos, home string, registry []string) []string {
	switch goos {
	case "windows":
		dirs := []string{
			`C:\Program Files (x86)\GOG Galaxy\Games\Stardew Valley`,
			`C:\GOG Games\Stardew Valley`,
		}
		return append(dirs, registry...)
	case "darwin":
		return []string{"/Applications/Stardew Valley.app/Contents/MacOS"}
	case "linux":
		if home == "" {
			return nil
		}
		return []string{home + "/GOGGames/StardewValley/game"}
	default:
		return nil
	}
}

// gogCandidates proposes every GOG/default game directory. Missing paths
// simply fail detection downstream — enumeration never stats the disk.
func gogCandidates() []Candidate {
	var out []Candidate
	for _, dir := range gogGameDirs() {
		if dir == "" {
			continue
		}
		out = append(out, Candidate{Path: dir, Source: SourceGOG})
	}
	return out
}

// defaultDeckMountRoots enumerates Deck SD-card mount points under
// /run/media instead of hardcoding card labels. Each mount is a Steam
// library root candidate. Any failure yields no mounts, never an error.
func defaultDeckMountRoots() []string {
	if runtime.GOOS != "linux" {
		return nil
	}
	return deckMountRootsAt("/run/media")
}

// deckMountRootsAt enumerates two levels under base (/run/media/<user>/...):
// the user dirs, then the card-label mounts beneath each.
func deckMountRootsAt(base string) []string {
	users, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []string
	for _, user := range users {
		if !user.IsDir() {
			continue
		}
		mounts, err := os.ReadDir(filepath.Join(base, user.Name()))
		if err != nil {
			continue
		}
		for _, mount := range mounts {
			if mount.IsDir() {
				out = append(out, filepath.Join(base, user.Name(), mount.Name()))
			}
		}
	}
	return out
}
