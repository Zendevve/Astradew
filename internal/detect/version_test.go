package detect

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"testing/fstest"
)

// makePE builds a synthetic minimal PE: a valid DOS+COFF header plus one
// .rsrc section carrying UTF-16LE StringFileInfo ProductVersion/FileVersion
// entries, so debug/pe parses it exactly like the real DLLs. Empty values
// omit the key, modelling a DLL with no version strings.
func makePE(product, file string) []byte {
	var rsrc bytes.Buffer
	writeEntry := func(key, value string) {
		if value == "" {
			return
		}
		for i := range key {
			rsrc.WriteByte(key[i])
			rsrc.WriteByte(0)
		}
		rsrc.Write([]byte{0, 0, 0, 0, 0, 0})
		for i := range value {
			rsrc.WriteByte(value[i])
			rsrc.WriteByte(0)
		}
		rsrc.Write([]byte{0, 0})
	}
	writeEntry("ProductVersion", product)
	writeEntry("FileVersion", file)
	if rsrc.Len() == 0 {
		rsrc.WriteString("NOVERSIONKEYS........")
	}
	raw := rsrc.Bytes()

	buf := new(bytes.Buffer)
	dos := make([]byte, 64)
	dos[0], dos[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(dos[0x3c:], 64)
	buf.Write(dos)
	buf.WriteString("PE\x00\x00")
	binary.Write(buf, binary.LittleEndian, uint16(0x14c))  // Machine: i386
	binary.Write(buf, binary.LittleEndian, uint16(1))      // NumberOfSections
	binary.Write(buf, binary.LittleEndian, uint32(0))      // TimeDateStamp
	binary.Write(buf, binary.LittleEndian, uint32(0))      // PointerToSymbolTable
	binary.Write(buf, binary.LittleEndian, uint32(0))      // NumberOfSymbols
	binary.Write(buf, binary.LittleEndian, uint16(96))     // SizeOfOptionalHeader
	binary.Write(buf, binary.LittleEndian, uint16(0x010f)) // Characteristics
	opt := make([]byte, 96)
	opt[0], opt[1] = 0x0b, 0x01 // PE32 magic
	binary.LittleEndian.PutUint32(opt[92:], 0)
	buf.Write(opt)
	name := make([]byte, 8)
	copy(name, ".rsrc")
	buf.Write(name)
	rawOff := 64 + 4 + 20 + 96 + 40
	binary.Write(buf, binary.LittleEndian, uint32(len(raw))) // VirtualSize
	binary.Write(buf, binary.LittleEndian, uint32(0x1000))   // VirtualAddress
	binary.Write(buf, binary.LittleEndian, uint32(len(raw))) // SizeOfRawData
	binary.Write(buf, binary.LittleEndian, uint32(rawOff))   // PointerToRawData
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint16(0))
	binary.Write(buf, binary.LittleEndian, uint16(0))
	binary.Write(buf, binary.LittleEndian, uint32(0x40000040))
	buf.Write(raw)
	return buf.Bytes()
}

func TestDetectSmapiProductVersionPreferred(t *testing.T) {
	report, err := Detect(gameFS("pe-smapi"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.Version.Assembly != "4.5.2" {
		t.Fatalf("Assembly = %q, want ProductVersion 4.5.2 (not FileVersion 4.5.2.0)", report.Version.Assembly)
	}
	if report.Version.Resolved != "4.5.2" || report.Version.Conflict != "" {
		t.Fatalf("version = %+v, want resolved 4.5.2 without conflict", report.Version)
	}
}

func TestDetectSmapiFileVersionFallback(t *testing.T) {
	report, err := Detect(gameFS("pe-smapi-fallback"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.Version.Assembly != "4.5.1.0" {
		t.Fatalf("Assembly = %q, want FileVersion fallback 4.5.1.0", report.Version.Assembly)
	}
	if report.Version.Resolved != "4.5.1.0" {
		t.Fatalf("Resolved = %q, want 4.5.1.0", report.Version.Resolved)
	}
}

func TestDetectSmapiAssemblyNoKeysUnknown(t *testing.T) {
	report, err := Detect(gameFS("pe-smapi-nokeys"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.Version.Assembly != "" || report.Version.Resolved != "" {
		t.Fatalf("version = %+v, want unknown when the PE carries no keys", report.Version)
	}
}

func TestDetectGameFileVersionIgnoresProduct(t *testing.T) {
	report, err := Detect(gameFS("pe-game"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.GameVersion != "1.6.15.0" {
		t.Fatalf("GameVersion = %q, want FileVersion 1.6.15.0, not malformed ProductVersion", report.GameVersion)
	}
}

func TestDetectManifestAgreeResolves(t *testing.T) {
	report, err := Detect(gameFS("manifest-ok"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.Version.Manifest != "4.5.2" || report.Version.Resolved != "4.5.2" {
		t.Fatalf("version = %+v, want agreed manifest 4.5.2 resolved", report.Version)
	}
}

func TestDetectManifestSingleResolves(t *testing.T) {
	report, err := Detect(gameFS("manifest-single"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.Version.Manifest != "4.5.2" || report.Version.Resolved != "4.5.2" {
		t.Fatalf("version = %+v, want the single bundled manifest used", report.Version)
	}
}

func TestDetectManifestConflictTrustsNone(t *testing.T) {
	report, err := Detect(gameFS("manifest-conflict"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	v := report.Version
	if v.Resolved != "" {
		t.Fatalf("Resolved = %q, want empty on conflict", v.Resolved)
	}
	if !strings.Contains(v.Conflict, "ConsoleCommands") || !strings.Contains(v.Conflict, "SaveBackup") {
		t.Fatalf("Conflict = %q, want both manifest sources named", v.Conflict)
	}
	if got := v.Detail(); !strings.Contains(got, "conflict") || !strings.Contains(got, v.Conflict) {
		t.Fatalf("Detail() = %q, want the conflict and its sources", got)
	}
	// Re-detect refreshes the same conflict rather than sticking anything.
	again, err := Detect(gameFS("manifest-conflict"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if again.Version.Resolved != "" || again.Version.Conflict == "" {
		t.Fatalf("re-detect = %+v, want conflict refreshed, nothing resolved", again.Version)
	}
}

func TestDetectAssemblyManifestDisagreeTrustsNone(t *testing.T) {
	report, err := Detect(gameFS("pe-smapi", "manifest-other"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	v := report.Version
	if v.Resolved != "" {
		t.Fatalf("Resolved = %q, want empty on assembly/manifest disagreement", v.Resolved)
	}
	if !strings.Contains(v.Conflict, "assembly") || !strings.Contains(v.Conflict, "manifest") {
		t.Fatalf("Conflict = %q, want assembly and manifest named", v.Conflict)
	}
}

func TestDetectAssemblyManifestAgree(t *testing.T) {
	report, err := Detect(gameFS("pe-smapi", "manifest-ok"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	v := report.Version
	if v.Assembly != "4.5.2" || v.Manifest != "4.5.2" || v.Resolved != "4.5.2" || v.Conflict != "" {
		t.Fatalf("version = %+v, want agreeing sources resolved", v)
	}
	if got := v.Detail(); !strings.Contains(got, "4.5.2") || strings.Contains(got, "unknown") {
		t.Fatalf("Detail() = %q, want the detected version stated", got)
	}
}

// Real-shape healthy install (version-read.md §4): ProductVersion keeps the
// build metadata while the agreed manifests carry the bare release. Display
// keeps the original string; no conflict.
func TestDetectAssemblyMetadataAgreesWithManifest(t *testing.T) {
	m := gameFS("manifest-ok")
	m["StardewModdingAPI.dll"] = &fstest.MapFile{Data: makePE("4.5.2+821167e5c511bf3a2d98f604e5e838561c469219", "4.5.2.0")}
	report, err := Detect(m)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	v := report.Version
	if v.Conflict != "" {
		t.Fatalf("version = %+v, want no conflict: metadata/padding are not disagreement", v)
	}
	if v.Resolved != "4.5.2+821167e5c511bf3a2d98f604e5e838561c469219" {
		t.Fatalf("Resolved = %q, want the original ProductVersion kept for display", v.Resolved)
	}
}

// FileVersion fallback "4.5.2.0" agrees with manifest "4.5.2": zero padding
// is not disagreement. Display keeps the original fallback string.
func TestDetectFileVersionPaddingAgreesWithManifest(t *testing.T) {
	m := gameFS("manifest-ok")
	m["StardewModdingAPI.dll"] = &fstest.MapFile{Data: makePE("", "4.5.2.0")}
	report, err := Detect(m)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.Version.Conflict != "" || report.Version.Resolved != "4.5.2.0" {
		t.Fatalf("version = %+v, want FileVersion fallback resolved without conflict", report.Version)
	}
}

// Prerelease suffixes are identity: a dev-build assembly beside a release
// manifest is a real disagreement, not padding.
func TestDetectPrereleaseDisagreesWithRelease(t *testing.T) {
	m := gameFS("manifest-ok")
	m["StardewModdingAPI.dll"] = &fstest.MapFile{Data: makePE("4.5.2-alpha.20240101", "4.5.2.0")}
	report, err := Detect(m)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.Version.Conflict == "" || report.Version.Resolved != "" {
		t.Fatalf("version = %+v, want prerelease-vs-release conflict trusting none", report.Version)
	}
}

func TestDetectManifestMinApiMismatchConflicts(t *testing.T) {
	report, err := Detect(gameFS("manifest-minapi"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	v := report.Version
	if v.Resolved != "" || !strings.Contains(v.Conflict, "MinimumApiVersion") {
		t.Fatalf("version = %+v, want Version-vs-MinimumApiVersion conflict", v)
	}
}

func TestDetectForgedManifestVersionIgnored(t *testing.T) {
	report, err := Detect(gameFS("manifest-forged-version"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.Version != (Versions{}) {
		t.Fatalf("version = %+v, want unknown: third-party UniqueID must never feed versions", report.Version)
	}
}

func TestDetectRenamedBundledManifestFeedsVersion(t *testing.T) {
	report, err := Detect(gameFS("renamed-version"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.Version.Manifest != "4.5.2" || report.Version.Resolved != "4.5.2" {
		t.Fatalf("version = %+v, want UniqueID-gated version from the renamed folder", report.Version)
	}
}

func TestDetectForbiddenFilesNeverFeedVersions(t *testing.T) {
	m := gameFS()
	m["steam_appid.txt"] = &fstest.MapFile{Data: []byte("4.9.9")}
	m["StardewModdingAPI.deps.json"] = &fstest.MapFile{Data: []byte(`{"version":"9.9.9"}`)}
	m["update.marker"] = &fstest.MapFile{Data: []byte("9.9.9")}
	m["CHANGELOG.md"] = &fstest.MapFile{Data: []byte("# 9.9.9")}
	m["smapi-internal/config.json"] = &fstest.MapFile{Data: []byte(`{"ApiVersion":"9.9.9"}`)}
	m["StardewModdingAPI.xml"] = &fstest.MapFile{Data: []byte("<doc>9.9.9</doc>")}
	report, err := Detect(m)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.GameVersion != "" {
		t.Fatalf("GameVersion = %q, want unknown: forbidden files must never feed versions", report.GameVersion)
	}
	if report.Version != (Versions{}) {
		t.Fatalf("version = %+v, want unknown: forbidden files must never feed versions", report.Version)
	}
}

func TestDetectAllAbsentUnknown(t *testing.T) {
	report, err := Detect(gameFS())
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.GameVersion != "" || report.Version != (Versions{}) {
		t.Fatalf("report = %+v, want unknown versions", report)
	}
	if got := report.Version.Detail(); !strings.Contains(got, "unknown") {
		t.Fatalf("Detail() = %q, want unknown stated honestly", got)
	}
}

// logFS is an ErrorLogs-style folder holding SMAPI-latest.txt.
func logFS(body string) fstest.MapFS {
	return fstest.MapFS{
		"SMAPI-latest.txt": {Data: []byte(body)},
	}
}

const logIntro = "[12:34:56 INFO  SMAPI] SMAPI 4.5.2 with Stardew Valley 1.6.14 on Microsoft Windows 11\n[12:34:57 DEBUG SMAPI] extra lines follow\n"

func TestReadLogHeaderParses(t *testing.T) {
	smapi, game, ok := ReadLogHeader(logFS(logIntro))
	if !ok {
		t.Fatal("ok = false, want the intro line parsed")
	}
	if smapi != "4.5.2" || game != "1.6.14" {
		t.Fatalf("header = %q/%q, want 4.5.2/1.6.14", smapi, game)
	}
}

func TestReadLogHeaderRefusals(t *testing.T) {
	for name, fs := range map[string]fstest.MapFS{
		"absent":     {},
		"unparsable": logFS("[12:34:56 INFO  SMAPI] mods loaded, no intro here\n"),
		"truncated":  logFS("SMAPI 4.5.2 with Stardew Valley 1.6.14\n"),
	} {
		if _, _, ok := ReadLogHeader(fs); ok {
			t.Fatalf("%s: ok = true, want false (never an error, just absent)", name)
		}
	}
	if _, _, ok := ReadLogHeader(nil); ok {
		t.Fatal("nil: ok = true, want false")
	}
}

func TestApplyLogHeaderLastRunOnly(t *testing.T) {
	report, err := Detect(gameFS())
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	smapi, game, ok := ReadLogHeader(logFS(logIntro))
	if !ok {
		t.Fatal("log header did not parse")
	}
	report.ApplyLogHeader(smapi, game)
	if report.Version.LastRun != "4.5.2" || report.Version.Resolved != "4.5.2" {
		t.Fatalf("version = %+v, want last-run 4.5.2 resolved when on-disk is empty", report.Version)
	}
	if report.GameVersion != "1.6.14" || !report.GameFromLog {
		t.Fatalf("report = %+v, want game fallen back to the log and flagged", report)
	}
	if got := report.Version.Detail(); !strings.Contains(got, "last SMAPI run") {
		t.Fatalf("Detail() = %q, want the last-run-only caveat", got)
	}
}

func TestApplyLogHeaderNeverOverwritesOnDisk(t *testing.T) {
	report, err := Detect(gameFS("pe-smapi", "pe-game"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	report.ApplyLogHeader("9.9.9", "9.9.9")
	if report.Version.Resolved != "4.5.2" {
		t.Fatalf("Resolved = %q, want on-disk 4.5.2 kept over the log", report.Version.Resolved)
	}
	if report.Version.LastRun != "9.9.9" {
		t.Fatalf("LastRun = %q, want the log recorded even when unused", report.Version.LastRun)
	}
	if report.GameVersion != "1.6.15.0" || report.GameFromLog {
		t.Fatalf("report = %+v, want on-disk game kept without the log flag", report)
	}
}

func TestApplyLogHeaderNeverClearsConflict(t *testing.T) {
	report, err := Detect(gameFS("manifest-conflict"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	report.ApplyLogHeader("4.5.2", "1.6.14")
	v := report.Version
	if v.Conflict == "" || v.Resolved != "" {
		t.Fatalf("version = %+v, want the conflict kept, nothing resolved", v)
	}
	if v.LastRun != "4.5.2" {
		t.Fatalf("LastRun = %q, want the log recorded even when conflicted", v.LastRun)
	}
}
