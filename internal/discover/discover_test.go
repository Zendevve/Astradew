package discover

import (
	"reflect"
	"testing"
)

func TestLibraryRootsModernShape(t *testing.T) {
	text := `"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Program Files (x86)\\Steam"
		"apps"
		{
			"413150"		"1024"
		}
	}
	"2"
	{
		"path"		"D:\\SteamLibrary"
		"apps"
		{
			"413150"		"2048"
		}
	}
}`
	got := LibraryRootsFromText(text)
	want := []string{`C:\Program Files (x86)\Steam`, `D:\SteamLibrary`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LibraryRootsFromText() = %#v, want %#v", got, want)
	}
}

func TestLibraryRootsLegacyShape(t *testing.T) {
	text := `"LibraryFolders"
{
	"0"		"C:\\Program Files (x86)\\Steam"
	"1"		"E:\\Games\\Steam"
}`
	got := LibraryRootsFromText(text)
	want := []string{`C:\Program Files (x86)\Steam`, `E:\Games\Steam`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LibraryRootsFromText() = %#v, want %#v", got, want)
	}
}

func TestLibraryRootsNonContiguousAndMissingTolerated(t *testing.T) {
	text := `"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Steam"
	}
	"7"
	{
		"label"		"no path here"
	}
	"9"
	{
		"path"		""
	}
	"12"
	{
		"path"		"/Volumes/External/SteamLibrary"
	}
}`
	got := LibraryRootsFromText(text)
	want := []string{`C:\Steam`, `/Volumes/External/SteamLibrary`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LibraryRootsFromText() = %#v, want %#v", got, want)
	}
}

func TestLibraryRootsMalformedYieldsNothing(t *testing.T) {
	for _, text := range []string{"", "not vdf at all", `"libraryfolders" { "0" { "path" `} {
		if got := LibraryRootsFromText(text); len(got) != 0 {
			t.Fatalf("LibraryRootsFromText(%q) = %#v, want no roots", text, got)
		}
	}
}

func TestSteamDataDirsWindowsUsesRegistryRootFirst(t *testing.T) {
	dirs := steamDataDirsFor("windows", `C:\Users\someone`, `D:\Steam`, nil)
	if len(dirs) != 2 || dirs[0] != `D:\Steam` || dirs[1] != `C:\Program Files (x86)\Steam` {
		t.Fatalf("steamDataDirsFor(windows) = %#v, want registry root then default", dirs)
	}
}

func TestSteamDataDirsMacOSStandard(t *testing.T) {
	dirs := steamDataDirsFor("darwin", "/Users/someone", "", nil)
	want := []string{"/Users/someone/Library/Application Support/Steam"}
	if !reflect.DeepEqual(dirs, want) {
		t.Fatalf("steamDataDirsFor(darwin) = %#v, want %#v", dirs, want)
	}
}

func TestSteamDataDirsLinuxNativeAndVariants(t *testing.T) {
	dirs := steamDataDirsFor("linux", "/home/deck", "", []string{"/run/media/deck/Card"})
	want := []string{
		"/home/deck/.local/share/Steam",
		"/home/deck/.steam/steam",
		"/home/deck/snap/steam/common/.local/share/Steam",
		"/home/deck/.var/app/com.valvesoftware.Steam/.local/share/Steam",
		"/home/deck/.var/app/com.valvesoftware.Steam/data/Steam",
		"/run/media/deck/Card",
	}
	if !reflect.DeepEqual(dirs, want) {
		t.Fatalf("steamDataDirsFor(linux) = %#v, want %#v", dirs, want)
	}
}

func TestLibraryRootsExpandsModernPlusLegacy(t *testing.T) {
	read := func(path string) (string, bool) {
		switch path {
		case "/steam/config/libraryfolders.vdf":
			return `"libraryfolders" { "1" { "path" "/mnt/alt" } }`, true
		case "/steam/steamapps/libraryfolders.vdf":
			return `"LibraryFolders" { "0" "/legacy" }`, true
		}
		return "", false
	}
	got := libraryRoots([]string{"/steam"}, read)
	want := []string{"/steam", "/mnt/alt", "/legacy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("libraryRoots() = %#v, want %#v", got, want)
	}
}

func TestLibraryRootsMissingFilesTolerated(t *testing.T) {
	got := libraryRoots([]string{"/absent"}, func(string) (string, bool) { return "", false })
	if !reflect.DeepEqual(got, []string{"/absent"}) {
		t.Fatalf("libraryRoots() = %#v, want the bare default dir", got)
	}
}

func TestGOGDirsMacOSPointsInsideBundle(t *testing.T) {
	dirs := gogGameDirsFor("darwin", "/Users/someone", nil)
	want := []string{"/Applications/Stardew Valley.app/Contents/MacOS"}
	if !reflect.DeepEqual(dirs, want) {
		t.Fatalf("gogGameDirsFor(darwin) = %#v, want %#v", dirs, want)
	}
}

func TestGOGDirsLinuxOfflineDefault(t *testing.T) {
	dirs := gogGameDirsFor("linux", "/home/someone", nil)
	want := []string{"/home/someone/GOGGames/StardewValley/game"}
	if !reflect.DeepEqual(dirs, want) {
		t.Fatalf("gogGameDirsFor(linux) = %#v, want %#v", dirs, want)
	}
}

func TestGOGDirsWindowsDefaultsPlusRegistry(t *testing.T) {
	dirs := gogGameDirsFor("windows", `C:\Users\someone`, []string{`E:\Stardew`})
	want := []string{
		`C:\Program Files (x86)\GOG Galaxy\Games\Stardew Valley`,
		`C:\GOG Games\Stardew Valley`,
		`E:\Stardew`,
	}
	if !reflect.DeepEqual(dirs, want) {
		t.Fatalf("gogGameDirsFor(windows) = %#v, want %#v", dirs, want)
	}
}

func TestCandidatesDeduplicateAndSort(t *testing.T) {
	oldDataDirs, oldGOG := steamDataDirs, gogGameDirs
	defer func() { steamDataDirs, gogGameDirs = oldDataDirs, oldGOG }()
	steamDataDirs = func() []string { return []string{"/b", "/a"} }
	gogGameDirs = func() []string { return []string{"/b/steamapps/common/Stardew Valley"} }
	oldRead := readLibraryFile
	readLibraryFile = func(string) (string, bool) { return "", false }
	defer func() { readLibraryFile = oldRead }()

	got := Candidates()
	want := []Candidate{
		{Path: "/a/steamapps/common/Stardew Valley", Source: SourceSteam},
		{Path: "/b/steamapps/common/Stardew Valley", Source: SourceSteam},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Candidates() = %#v, want %#v", got, want)
	}
}
