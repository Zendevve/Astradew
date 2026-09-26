package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// probeFS wraps a filesystem and records the names passed to ReadFile and
// ReadDir, so a cache hit or a work bound is observable as a file the scan never
// re-read or a listing it never took. ReadFile can also be made to fail per
// name, which models a transient read failure.
type probeFS struct {
	fsys     fs.FS
	mu       sync.Mutex
	reads    []string
	dirs     []string
	failRead map[string]bool
}

func newProbe(fsys fs.FS) *probeFS {
	return &probeFS{fsys: fsys, failRead: map[string]bool{}}
}

// fail makes ReadFile report an error for name until repaired.
func (p *probeFS) fail(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failRead[name] = true
}

// repair makes ReadFile serve name again.
func (p *probeFS) repair(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.failRead, name)
}

func (p *probeFS) Open(name string) (fs.File, error) { return p.fsys.Open(name) }

func (p *probeFS) ReadDir(name string) ([]fs.DirEntry, error) {
	p.mu.Lock()
	p.dirs = append(p.dirs, name)
	p.mu.Unlock()
	return fs.ReadDir(p.fsys, name)
}

func (p *probeFS) Stat(name string) (fs.FileInfo, error) { return fs.Stat(p.fsys, name) }

func (p *probeFS) ReadFile(name string) ([]byte, error) {
	p.mu.Lock()
	p.reads = append(p.reads, name)
	failing := p.failRead[name]
	p.mu.Unlock()
	if failing {
		return nil, fs.ErrPermission
	}
	return fs.ReadFile(p.fsys, name)
}

// countReads reports how many ReadFile calls named name.
func (p *probeFS) countReads(name string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, read := range p.reads {
		if read == name {
			n++
		}
	}
	return n
}

// readNames lists every name read so far, in order.
func (p *probeFS) readNames() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.reads...)
}

// countDirs reports how many times a folder listing named name was read.
func (p *probeFS) countDirs(name string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, dir := range p.dirs {
		if dir == name {
			n++
		}
	}
	return n
}

// hashOf is the SHA-256 oracle for one body, in the hex form Hash returns.
func hashOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func TestCacheServesUnchangedRescan(t *testing.T) {
	tree := NewFixture().Mod("Alpha", "Alpha.Mod").Mod("Beta", "Beta.Mod").FS()
	probe := newProbe(tree)
	opts := Options{Cache: NewCache()}

	first := Scan(probe, opts)
	second := Scan(probe, opts)

	for _, name := range []string{"Alpha/manifest.json", "Beta/manifest.json"} {
		if got := probe.countReads(name); got != 1 {
			t.Fatalf("%s read %d times, want once: an unchanged rescan must hit the cache (reads: %v)", name, got, probe.readNames())
		}
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("rescan records differ:\nfirst:  %+v\nsecond: %+v", first, second)
	}
	for i := range first {
		if first[i].Unit != second[i].Unit {
			t.Fatalf("%s: rescan served a different unit, want the parse the cache stored", first[i].Path)
		}
	}
	if unit := byPath(second)["Alpha"].Unit; unit == nil || unit.Manifest.UniqueID != "Alpha.Mod" || unit.Verdict != VerdictValid {
		t.Fatalf("Alpha unit = %+v, want the parsed Alpha.Mod unit", unit)
	}
}

func TestCacheReparsesGrownManifestAlone(t *testing.T) {
	tree := NewFixture().Mod("Alpha", "Alpha.Mod").Mod("Beta", "Beta.Mod").Mod("Gamma", "Gamma.Mod").FS()
	probe := newProbe(tree)
	opts := Options{Cache: NewCache()}
	first := Scan(probe, opts)

	grown := `{"Name":"Alpha","Author":"A","Version":"1.0.0","Description":"a longer body","UniqueID":"Alpha.Mod","EntryDll":"Mod.dll"}`
	tree["Alpha/manifest.json"] = &fstest.MapFile{Data: []byte(grown)}

	second := byPath(Scan(probe, opts))
	if unit := second["Alpha"].Unit; unit == nil || unit.Manifest.Description != "a longer body" {
		t.Fatalf("Alpha unit = %+v, want the rewritten body's description", unit)
	}
	for name, want := range map[string]int{
		"Alpha/manifest.json": 2, // the parse it had is stale, so it is read again
		"Beta/manifest.json":  1,
		"Gamma/manifest.json": 1,
	} {
		if got := probe.countReads(name); got != want {
			t.Fatalf("%s read %d times, want %d: one changed stat must invalidate exactly that entry (reads: %v)", name, got, want, probe.readNames())
		}
	}
	if firstUnit := byPath(first)["Beta"].Unit; firstUnit != second["Beta"].Unit {
		t.Fatal("Beta was re-parsed although only Alpha's manifest changed")
	}
}

func TestCacheReparsesNewModTimeAlone(t *testing.T) {
	tree := NewFixture().Mod("Alpha", "Alpha.Mod").FS()
	probe := newProbe(tree)
	opts := Options{Cache: NewCache()}
	Scan(probe, opts)

	file := tree["Alpha/manifest.json"]
	tree["Alpha/manifest.json"] = &fstest.MapFile{Data: file.Data, ModTime: time.Unix(1_700_000_000, 0)}

	records := byPath(Scan(probe, opts))
	if got := probe.countReads("Alpha/manifest.json"); got != 2 {
		t.Fatalf("Alpha read %d times, want twice: a new modification time alone is an edit (reads: %v)", got, probe.readNames())
	}
	if unit := records["Alpha"].Unit; unit == nil || unit.Manifest.UniqueID != "Alpha.Mod" {
		t.Fatalf("Alpha unit = %+v, want the re-parsed unit", unit)
	}
}

func TestCacheKeysOnPathNotJustStat(t *testing.T) {
	tree := NewFixture().Mod("One", "One.Mod").Mod("Two", "Two.Mod").FS()
	when := time.Unix(1_600_000_000, 0)
	for _, name := range []string{"One/manifest.json", "Two/manifest.json"} {
		file := tree[name]
		tree[name] = &fstest.MapFile{Data: file.Data, ModTime: when}
	}
	if len(tree["One/manifest.json"].Data) != len(tree["Two/manifest.json"].Data) {
		t.Fatal("the two manifests must be the same size for this test to mean anything")
	}

	records := byPath(Scan(newProbe(tree), Options{Cache: NewCache()}))
	if got := records["One"].Unit.Manifest.UniqueID; got != "One.Mod" {
		t.Fatalf("One = %q, want its own Unique ID: two paths with the same size and mtime must not collide", got)
	}
	if got := records["Two"].Unit.Manifest.UniqueID; got != "Two.Mod" {
		t.Fatalf("Two = %q, want its own Unique ID", got)
	}
}

func TestCacheNeverRemembersAFailure(t *testing.T) {
	tree := NewFixture().Mod("Alpha", "Alpha.Mod").Mod("Beta", "Beta.Mod").FS()
	probe := newProbe(tree)
	opts := Options{Cache: NewCache()}

	probe.fail("Alpha/manifest.json")
	broken := byPath(Scan(probe, opts))
	failed := broken["Alpha"]
	if failed.Outcome != OutcomeInvalid || failed.Reason != ReasonUnreadable {
		t.Fatalf("failed read = %+v, want invalid/unreadable", failed)
	}
	if !strings.Contains(failed.Note, "its manifest couldn't be read:") {
		t.Fatalf("note = %q, want the read failure named", failed.Note)
	}
	if failed.Unit != nil {
		t.Fatal("a failed read must not produce a unit")
	}
	if r := broken["Beta"]; r.Outcome != OutcomeSmapi || r.Unit == nil {
		t.Fatalf("Beta = %+v, want an unaffected unit beside the failure", r)
	}

	probe.repair("Alpha/manifest.json")
	healed := byPath(Scan(probe, opts))
	if r := healed["Alpha"]; r.Outcome != OutcomeSmapi || r.Unit == nil || r.Unit.Manifest.UniqueID != "Alpha.Mod" {
		t.Fatalf("retry after the failure = %+v, want the parsed unit", r)
	}

	third := byPath(Scan(probe, opts))
	if got := probe.countReads("Alpha/manifest.json"); got != 2 {
		t.Fatalf("Alpha read %d times, want twice: the failure isn't cached and the successful retry is (reads: %v)", got, probe.readNames())
	}
	if third["Alpha"].Unit != healed["Alpha"].Unit {
		t.Fatal("the successful retry wasn't cached: the next scan parsed Alpha again")
	}
}

// TestCacheServesConcurrentScans pins the cache's promise that scans can run
// from concurrent tasks: several scans sharing one Cache see the same records,
// and the sharing is what forces every task through the same map. Go's runtime
// kills a test outright on a concurrent map write, so the many tasks here are
// also what keeps the map's locking honest.
func TestCacheServesConcurrentScans(t *testing.T) {
	fixture := NewFixture()
	const mods = 24
	for i := range mods {
		uid := fmt.Sprintf("Mod%02d.UID", i)
		fixture.Mod(fmt.Sprintf("Mod%02d", i), uid)
	}
	tree := fixture.FS()
	opts := Options{Cache: NewCache()}

	const scans = 16
	results := make([][]Record, scans)
	var wg sync.WaitGroup
	wg.Add(scans)
	for i := range results {
		go func(i int) {
			defer wg.Done()
			results[i] = Scan(newProbe(tree), opts)
		}(i)
	}
	wg.Wait()

	for i := range results {
		if len(results[i]) != mods {
			t.Fatalf("concurrent scan %d reported %d records, want %d", i, len(results[i]), mods)
		}
		if !reflect.DeepEqual(results[0], results[i]) {
			t.Fatalf("concurrent scan %d differs from the first:\nfirst: %+v\nscan %d: %+v", i, results[0], i, results[i])
		}
	}
}

// TestCacheNeverCachesFoldersWithoutAManifest pins the rule that only parsed
// manifests are cached: ignored, empty, XNB, and manifest-less folders are
// re-derived by every scan, which here shows up as zero manifest reads.
func TestCacheNeverCachesFoldersWithoutAManifest(t *testing.T) {
	tree := NewFixture().
		Ignored(".Trash", "Trash.Mod").
		File(".Backup/Mod.dll", "dll").
		MissingManifest("Leftovers").
		XnbMod("Legacy").
		Dir("Empty").
		FS()
	probe := newProbe(tree)
	opts := Options{Cache: NewCache()}

	want := map[string]Reason{
		".Trash":    ReasonIgnoredFolder,
		".Backup":   ReasonIgnoredFolder,
		"Leftovers": ReasonManifestMissing,
		"Legacy":    ReasonXnbMod,
		"Empty":     ReasonEmptyFolder,
	}
	for scan := 1; scan <= 2; scan++ {
		scanned := Scan(probe, opts)
		records := byPath(scanned)
		if len(scanned) != len(want) {
			t.Fatalf("scan %d: records = %v, want the five folders the fixture declares", scan, scanPaths(scanned))
		}
		for name, reason := range want {
			record, ok := records[name]
			if !ok {
				t.Fatalf("scan %d: %s not reported; paths = %v", scan, name, scanPaths(scanned))
			}
			if record.Reason != reason {
				t.Fatalf("scan %d: %s = %+v, want reason %q", scan, name, record, reason)
			}
			if record.Unit != nil {
				t.Fatalf("scan %d: %s carries a unit, want none: nothing is cached for a folder with no manifest", scan, name)
			}
		}
	}
	if names := probe.readNames(); len(names) != 0 {
		t.Fatalf("these folders are re-derived without reading a manifest, but read %v", names)
	}
}

// swapFS rewrites one file in the middle of a read: a filesystem racing a scan,
// which is the case the stat-read-stat guard exists for.
type swapFS struct {
	fstest.MapFS
	name    string
	replace *fstest.MapFile
	swapped bool
}

func (s *swapFS) ReadFile(name string) ([]byte, error) {
	if name != s.name || s.swapped {
		return fs.ReadFile(s.MapFS, name)
	}
	data, err := fs.ReadFile(s.MapFS, name)
	s.MapFS[s.name] = s.replace
	s.swapped = true
	return data, err
}

func TestCacheStoresOnlyAReadThatStayedStable(t *testing.T) {
	const (
		bodyV1 = `{"Name":"Alpha","Author":"A","Version":"1.0.0","UniqueID":"Alpha.Mod","EntryDll":"Mod.dll"}`
		bodyV2 = `{"Name":"Alpha","Author":"A","Version":"2.0.0","UniqueID":"Alpha.Mod","EntryDll":"Mod.dll"}`
	)
	when := time.Unix(1_700_000_000, 0)
	racing := &swapFS{
		MapFS:   fstest.MapFS{"Alpha/manifest.json": &fstest.MapFile{Data: []byte(bodyV1), ModTime: when}},
		name:    "Alpha/manifest.json",
		replace: &fstest.MapFile{Data: []byte(bodyV2 + " "), ModTime: when.Add(time.Hour)},
	}
	cache := NewCache()

	raced := byPath(Scan(racing, Options{Cache: cache}))
	if unit := raced["Alpha"].Unit; unit == nil || unit.Manifest.Version != "1.0.0" {
		t.Fatalf("raced unit = %+v, want the bytes that were actually read", unit)
	}

	// The file settles back to exactly the stat that read observed, with other
	// bytes under it (a same-size rewrite). Nothing may be served from the
	// racing read: its stat moved while it was being read, so it was never
	// cached.
	racing.MapFS["Alpha/manifest.json"] = &fstest.MapFile{Data: []byte(bodyV2), ModTime: when}
	settled := byPath(Scan(racing, Options{Cache: cache}))
	if unit := settled["Alpha"].Unit; unit == nil || unit.Manifest.Version != "2.0.0" {
		t.Fatalf("settled unit = %+v, want the file's own bytes: a read whose stat moved must not be cached", unit)
	}
}

func TestCacheHashReadsOncePerStat(t *testing.T) {
	tree := NewFixture().File("Data/notes.json", "abc").FS()
	probe := newProbe(tree)
	cache := NewCache()

	sum, err := cache.Hash(probe, "Data/notes.json")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if want := hashOf("abc"); sum != want {
		t.Fatalf("hash = %q, want the hex SHA-256 %q", sum, want)
	}
	again, err := cache.Hash(probe, "Data/notes.json")
	if err != nil {
		t.Fatalf("second Hash: %v", err)
	}
	if again != sum {
		t.Fatalf("second hash = %q, want the reused %q", again, sum)
	}
	if got := probe.countReads("Data/notes.json"); got != 1 {
		t.Fatalf("file read %d times, want once while its stat is unchanged (reads: %v)", got, probe.readNames())
	}
}

func TestCacheHashFollowsContentAndStat(t *testing.T) {
	body := codeMod("A.Mod")
	tree := fstest.MapFS{"Mod/manifest.json": &fstest.MapFile{Data: []byte(body)}}
	probe := newProbe(tree)
	cache := NewCache()

	first, err := cache.Hash(probe, "Mod/manifest.json")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if want := hashOf(body); first != want {
		t.Fatalf("hash = %q, want %q", first, want)
	}

	// A longer rewrite: the stat's size moves.
	grown := body + " "
	tree["Mod/manifest.json"] = &fstest.MapFile{Data: []byte(grown)}
	second, err := cache.Hash(probe, "Mod/manifest.json")
	if err != nil {
		t.Fatalf("Hash after grow: %v", err)
	}
	if want := hashOf(grown); second != want {
		t.Fatalf("hash after grow = %q, want %q", second, want)
	}

	// The rewrite a stat alone can't see, reached through a new mtime: same
	// size, different bytes.
	swapped := strings.Replace(grown, "1.0.0", "2.0.0", 1)
	tree["Mod/manifest.json"] = &fstest.MapFile{Data: []byte(swapped), ModTime: time.Unix(1_700_000_000, 0)}
	third, err := cache.Hash(probe, "Mod/manifest.json")
	if err != nil {
		t.Fatalf("Hash after mtime change: %v", err)
	}
	if want := hashOf(swapped); third != want {
		t.Fatalf("hash after mtime change = %q, want %q", third, want)
	}
	if third == second {
		t.Fatal("a same-size rewrite with a new mtime must produce a different hash")
	}
}

func TestCacheHashNeverCachesAFailure(t *testing.T) {
	body := codeMod("A.Mod")
	tree := fstest.MapFS{
		"Mod/manifest.json":     &fstest.MapFile{Data: []byte(body)},
		"Missing/manifest.json": &fstest.MapFile{Data: []byte("late")},
	}
	probe := newProbe(tree)
	cache := NewCache()

	if _, err := cache.Hash(probe, "Gone/manifest.json"); err == nil {
		t.Fatal("hashing a missing file must report an error")
	}
	if sum, err := cache.Hash(probe, "Missing/manifest.json"); err != nil {
		t.Fatalf("Hash: %v", err)
	} else if want := hashOf("late"); sum != want {
		t.Fatalf("hash = %q, want %q: a missing-file failure must poison nothing", sum, want)
	}

	probe.fail("Mod/manifest.json")
	if _, err := cache.Hash(probe, "Mod/manifest.json"); err == nil {
		t.Fatal("hashing an unreadable file must report an error")
	}
	probe.repair("Mod/manifest.json")
	sum, err := cache.Hash(probe, "Mod/manifest.json")
	if err != nil {
		t.Fatalf("Hash after the failure: %v", err)
	}
	if want := hashOf(body); sum != want {
		t.Fatalf("hash after the failure = %q, want %q: the failed read stored nothing", sum, want)
	}
}
