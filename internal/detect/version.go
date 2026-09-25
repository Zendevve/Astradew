package detect

import (
	"bytes"
	"debug/pe"
	"encoding/json"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Versions is the issue-25 SMAPI version verdict: every source observed plus
// the resolution. Empty means unknown. Resolved is the single version to
// show ("" when unknown or conflicted); Conflict names the disagreeing
// sources and trust is withheld (Resolved "") until the install is fixed.
type Versions struct {
	Assembly string // ProductVersion of StardewModdingAPI.dll, fallback FileVersion
	Manifest string // agreed UniqueID-gated bundled-manifest Version ("" when none/disagree)
	LastRun  string // SMAPI version from the last-run log header (ApplyLogHeader only)
	Resolved string // Assembly, then Manifest, then LastRun; "" on unknown/conflict
	Conflict string // disagreeing sources; "" when none
}

// Detail reports the verdict in one honest sentence: the conflict and its
// sources, the last-run-only caveat, what was detected, or unknown.
func (v Versions) Detail() string {
	if v.Conflict != "" {
		if v.LastRun != "" {
			return "SMAPI version conflict: " + v.Conflict + "; last run was " + v.LastRun + ", but the conflict trusts none of its sources; reinstall SMAPI to fix"
		}
		return "SMAPI version conflict: " + v.Conflict + "; reinstall SMAPI to fix"
	}
	if v.Resolved == "" {
		return "SMAPI version unknown"
	}
	if v.Assembly == "" && v.Manifest == "" && v.LastRun != "" {
		return "SMAPI " + v.Resolved + " from the last SMAPI run; on-disk version unknown"
	}
	return "SMAPI " + v.Resolved + " detected on disk"
}

// peVersionStrings reads the Win32 version-resource strings of one DLL in
// gameDir: ProductVersion first (it keeps the prerelease suffix, which is
// why SMAPI prefers it), FileVersion second. Any failure — missing file,
// unparsable PE, absent .rsrc, missing keys — yields "" per field. Versions
// are best-effort and never refuse detection.
func peVersionStrings(gameDir fs.FS, name string) (product, file string) {
	raw, err := fs.ReadFile(gameDir, name)
	if err != nil {
		return "", ""
	}
	f, err := pe.NewFile(bytes.NewReader(raw))
	if err != nil {
		return "", ""
	}
	var rsrc []byte
	for _, s := range f.Sections {
		if s.Name == ".rsrc" {
			rsrc, err = s.Data()
			if err != nil {
				return "", ""
			}
			break
		}
	}
	if len(rsrc) == 0 {
		return "", ""
	}
	return versionStringFromRsrc(rsrc, "ProductVersion"), versionStringFromRsrc(rsrc, "FileVersion")
}

// versionStringFromRsrc scans a .rsrc blob for one UTF-16LE StringFileInfo
// key and returns the NUL-terminated UTF-16LE value that follows it. Binary
// alignment padding between key and value is skipped. Anything unexpected
// (key absent, truncated blob, non-ASCII value) yields "".
func versionStringFromRsrc(rsrc []byte, key string) string {
	k := make([]byte, 0, 2*len(key)+2)
	for i := range len(key) {
		k = append(k, key[i], 0)
	}
	k = append(k, 0, 0)
	idx := bytes.Index(rsrc, k)
	if idx < 0 {
		return ""
	}
	p := idx + len(k)
	for p+1 < len(rsrc) && rsrc[p] == 0 && rsrc[p+1] == 0 {
		p += 2
	}
	var b []byte
	for p+1 < len(rsrc) {
		lo, hi := rsrc[p], rsrc[p+1]
		if lo == 0 && hi == 0 {
			break
		}
		if hi != 0 {
			break
		}
		b = append(b, lo)
		p += 2
	}
	return strings.TrimSpace(string(b))
}

// detectGameVersion reads the game version: FileVersion of
// Stardew Valley.dll only. ProductVersion is ignored — the game stamps all
// four parts into FileVersion while its ProductVersion is malformed.
func detectGameVersion(gameDir fs.FS) string {
	_, file := peVersionStrings(gameDir, "Stardew Valley.dll")
	return file
}

// manifestVersion is one UniqueID-gated bundled-manifest observation: the
// Mods subfolder it was read from plus its claimed versions.
type manifestVersion struct {
	dir     string
	version string
	minApi  string
}

// bundledManifestVersions enumerates Mods/*/manifest.json gated by the
// UniqueID allow-list: folder names never identify a System Mod, and only
// the two bundled manifests are ever opened — never third-party contents,
// never forbidden files. Entries without a Version contribute nothing.
func bundledManifestVersions(gameDir fs.FS) []manifestVersion {
	entries, err := fs.ReadDir(gameDir, "Mods")
	if err != nil {
		return nil
	}
	var out []manifestVersion
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := fs.ReadFile(gameDir, "Mods/"+e.Name()+"/manifest.json")
		if err != nil {
			continue
		}
		var m struct {
			UniqueID          string `json:"UniqueID"`
			Version           string `json:"Version"`
			MinimumApiVersion string `json:"MinimumApiVersion"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if !bundledModIDs[m.UniqueID] || strings.TrimSpace(m.Version) == "" {
			continue
		}
		out = append(out, manifestVersion{
			dir:     e.Name(),
			version: strings.TrimSpace(m.Version),
			minApi:  strings.TrimSpace(m.MinimumApiVersion),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dir < out[j].dir })
	return out
}

// versionIdentity normalises a version string for source comparison only:
// build metadata ("+...") never distinguishes an install, trailing zero
// components are padding ("4.5.2" == "4.5.2.0"), and surrounding space is
// ignored. Prerelease suffixes ("-alpha.<date>") are identity: a dev build
// beside a release manifest is a real disagreement. Display keeps the
// original string; only Conflict checks use the identity.
func versionIdentity(v string) string {
	trimmed := strings.TrimSpace(v)
	if i := strings.Index(trimmed, "+"); i >= 0 {
		trimmed = trimmed[:i]
	}
	core, pre, _ := strings.Cut(trimmed, "-")
	parts := strings.Split(core, ".")
	for len(parts) > 1 && parts[len(parts)-1] == "0" {
		parts = parts[:len(parts)-1]
	}
	identity := strings.Join(parts, ".")
	if pre != "" {
		identity += "-" + pre
	}
	return identity
}

// sameVersion reports whether two version strings name the same release
// under versionIdentity: metadata and zero padding ignored, prerelease kept.
func sameVersion(a, b string) bool {
	return versionIdentity(a) == versionIdentity(b)
}

// detectSmapiVersions resolves the on-disk SMAPI version; LastRun stays ""
// here (ApplyLogHeader owns the log fallback). Precedence is assembly, then
// the UniqueID-gated bundled manifests. Sources compare by versionIdentity
// (build metadata and zero padding ignored, prerelease kept) while display
// keeps the original strings. Any real disagreement — manifests among
// themselves, a manifest Version against its own MinimumApiVersion, or
// assembly against manifest — names its sources in Conflict and trusts none
// (Resolved "").
func detectSmapiVersions(gameDir fs.FS) Versions {
	v := Versions{}
	if product, file := peVersionStrings(gameDir, "StardewModdingAPI.dll"); product != "" {
		v.Assembly = product
	} else {
		v.Assembly = file
	}
	mans := bundledManifestVersions(gameDir)
	for _, m := range mans {
		if m.minApi != "" && m.minApi != m.version {
			v.Conflict = "manifest " + m.dir + " Version " + strconv.Quote(m.version) +
				" vs MinimumApiVersion " + strconv.Quote(m.minApi)
			return v
		}
	}
	switch {
	case len(mans) == 0:
		// No bundled-manifest version observed.
	case len(mans) == 1:
		v.Manifest = mans[0].version
	default:
		agree := true
		for _, m := range mans[1:] {
			if !sameVersion(m.version, mans[0].version) {
				agree = false
				break
			}
		}
		if !agree {
			parts := make([]string, len(mans))
			for i, m := range mans {
				parts[i] = "manifest " + m.dir + " " + strconv.Quote(m.version)
			}
			v.Conflict = strings.Join(parts, " vs ")
			return v
		}
		v.Manifest = mans[0].version
	}
	if v.Assembly != "" && v.Manifest != "" && !sameVersion(v.Assembly, v.Manifest) {
		v.Conflict = "assembly " + strconv.Quote(v.Assembly) + " vs manifest " + strconv.Quote(v.Manifest)
		return v
	}
	if v.Assembly != "" {
		v.Resolved = v.Assembly
	} else {
		v.Resolved = v.Manifest
	}
	return v
}

// logHeaderPattern matches the SMAPI intro line written by
// LogManager.LogIntro: "SMAPI {ApiVersion} with Stardew Valley
// {GameVersion} on {FriendlyOS}". The trailing "on" anchors the game
// version so a truncated line never parses.
var logHeaderPattern = regexp.MustCompile(`SMAPI\s+(\S+)\s+with\s+Stardew\s+Valley\s+(\S+)\s+on\b`)

// ReadLogHeader parses the SMAPI intro line out of SMAPI-latest.txt in
// logDir (the ErrorLogs folder, outside the game dir). A missing or
// unparsable log reports ok=false and never errors: the last run is a
// fallback source, never a refusal.
func ReadLogHeader(logDir fs.FS) (smapi, game string, ok bool) {
	if logDir == nil {
		return "", "", false
	}
	raw, err := fs.ReadFile(logDir, "SMAPI-latest.txt")
	if err != nil {
		return "", "", false
	}
	m := logHeaderPattern.FindSubmatch(raw)
	if m == nil {
		return "", "", false
	}
	return string(m[1]), string(m[2]), true
}

// ApplyLogHeader folds a parsed log header into the report. The SMAPI
// version is recorded as LastRun and resolves only when the on-disk sources
// are empty — it never overwrites what the folder says, and never clears a
// conflict. The game version falls back to the log only when the game DLL
// yielded nothing, flagged by GameFromLog.
func (r *Report) ApplyLogHeader(smapi, game string) {
	if r == nil {
		return
	}
	if smapi != "" {
		r.Version.LastRun = smapi
		if r.Version.Resolved == "" && r.Version.Conflict == "" {
			r.Version.Resolved = smapi
		}
	}
	if game != "" && r.GameVersion == "" {
		r.GameVersion = game
		r.GameFromLog = true
	}
}
