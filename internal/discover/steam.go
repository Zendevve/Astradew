package discover

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Discovery seams. Production reads the machine; tests stub these so CI
// stays headless with no Steam, GOG, or registry present.
var (
	userHomeDir = os.UserHomeDir

	readLibraryFile = func(path string) (string, bool) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", false
		}
		return string(raw), true
	}

	steamDataDirs  = defaultSteamDataDirs
	deckMountRoots = defaultDeckMountRoots
)

// defaultSteamDataDirs reports the Steam data dirs on this machine: the
// registry-located root plus the install default on Windows, the standard
// root on macOS, and the native/symlink/snap/flatpak forms plus Deck mounts
// on Linux. Anything absent simply yields no candidates downstream.
func defaultSteamDataDirs() []string {
	var home string
	if dir, err := userHomeDir(); err == nil {
		home = dir
	}
	return steamDataDirsFor(runtime.GOOS, home, steamRootFromRegistry(), deckMountRoots())
}

// joinVDFPath joins a Steam data dir with a VDF relative path using
// forward slashes: libraryfolders.vdf entries are Valve text, not OS paths,
// so tests can name POSIX fixtures on any host.
func joinVDFPath(dir, rel string) string {
	return strings.TrimSuffix(dir, "/") + "/" + rel
}

// steamDataDirsFor is the pure OS table behind defaultSteamDataDirs:
// registryRoot is the Windows Steam root from the registry ("" when absent),
// mounts enumerates Deck SD-card mount points (never hardcoded labels).
// POSIX paths are joined with forward slashes so the table is testable on
// any host; Windows entries are native literals.
func steamDataDirsFor(goos, home, registryRoot string, mounts []string) []string {
	switch goos {
	case "windows":
		var dirs []string
		if registryRoot != "" {
			dirs = append(dirs, registryRoot)
		}
		return append(dirs, `C:\Program Files (x86)\Steam`)
	case "darwin":
		if home == "" {
			return nil
		}
		return []string{home + "/Library/Application Support/Steam"}
	case "linux":
		var dirs []string
		if home != "" {
			dirs = append(dirs,
				home+"/.local/share/Steam",
				home+"/.steam/steam",
				home+"/snap/steam/common/.local/share/Steam",
				home+"/.var/app/com.valvesoftware.Steam/.local/share/Steam",
				home+"/.var/app/com.valvesoftware.Steam/data/Steam",
			)
		}
		return append(dirs, mounts...)
	default:
		return nil
	}
}

// libraryRoots expands data dirs into library roots: each data dir is itself
// the default library, plus every root named by its modern
// config/libraryfolders.vdf and legacy steamapps/libraryfolders.vdf files.
// Missing or unparsable files contribute nothing — unmounted or absent
// libraries are tolerated, never fatal.
func libraryRoots(dataDirs []string, read func(string) (string, bool)) []string {
	var roots []string
	for _, dir := range dataDirs {
		if dir == "" {
			continue
		}
		roots = append(roots, dir)
		if text, ok := read(joinVDFPath(dir, "config/libraryfolders.vdf")); ok {
			roots = append(roots, LibraryRootsFromText(text)...)
		}
		if text, ok := read(joinVDFPath(dir, "steamapps/libraryfolders.vdf")); ok {
			roots = append(roots, LibraryRootsFromText(text)...)
		}
	}
	return roots
}

// joinCandidate joins a library root with the game relative path. The VDF
// may name a Windows library (drive letters, backslashes) even when tests
// run on POSIX, so the separator follows the root's own style: roots
// containing a backslash or drive letter join with backslashes, otherwise
// with path.Join.
func joinCandidate(lib, rel string) string {
	forward := path.Join(filepath.ToSlash(lib), filepath.ToSlash(rel))
	if strings.Contains(lib, "\\") || (len(lib) > 1 && lib[1] == ':') {
		return strings.ReplaceAll(forward, "/", "\\")
	}
	return forward
}

// steamCandidates proposes one game directory per Steam library root:
// <lib>/steamapps/common/Stardew Valley for app ID 413150. On macOS both
// steamapps spellings are proposed; the miss simply fails detection.
func steamCandidates() []Candidate {
	var out []Candidate
	for _, lib := range libraryRoots(steamDataDirs(), readLibraryFile) {
		out = append(out, Candidate{
			Path:   joinCandidate(lib, "steamapps/common/Stardew Valley"),
			Source: SourceSteam,
		})
		if runtime.GOOS == "darwin" {
			out = append(out, Candidate{
				Path:   joinCandidate(lib, "SteamApps/common/Stardew Valley"),
				Source: SourceSteam,
			})
		}
	}
	return out
}
