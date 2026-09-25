// Package manifest parses SMAPI manifest.json files into typed Mod Units.
//
// It is resilient by construction: a malformed optional field degrades the
// unit to a partial verdict with a warning instead of dropping the unit or
// panicking, while missing or malformed required identity fails validation.
// Parsing runs in three stages — JSON decoding, field interpretation, and
// semantic validation — each with its own error kind. Unknown fields are
// preserved verbatim for round-trip. The package is pure: it takes bytes,
// performs no I/O, and introduces no new module dependencies.
package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Verdict is the parser's judgment of one manifest.
type Verdict string

const (
	// VerdictValid means every field parsed and validated.
	VerdictValid Verdict = "valid"
	// VerdictPartial means required identity holds but an optional field
	// is malformed; the unit survives with warnings.
	VerdictPartial Verdict = "partial"
	// VerdictInvalid means the file is not JSON, a value cannot be
	// interpreted at all, or required identity is missing or malformed.
	VerdictInvalid Verdict = "invalid"
)

// Stage is which parsing stage produced a FieldError.
type Stage string

const (
	// StageSyntax means the file is not a JSON object at all.
	StageSyntax Stage = "syntax"
	// StageInterpretation means a present value has the wrong shape.
	StageInterpretation Stage = "interpretation"
	// StageValidation means the interpreted values fail identity rules, or
	// deviate from the published manifest schema. Schema deviations are
	// warnings: they degrade the verdict to partial without dropping the unit.
	StageValidation Stage = "validation"
)

// FieldError is one diagnosed problem with a manifest.
type FieldError struct {
	Field   string // manifest field, "manifest" for whole-file rules, "" for syntax
	Stage   Stage
	Message string
}

// Dependency is one entry of a manifest's Dependencies list.
type Dependency struct {
	UniqueID       string
	MinimumVersion string // "" means no floor
	IsRequired     bool
}

// ContentPackFor is the host link a Content Pack declares.
type ContentPackFor struct {
	UniqueID       string
	MinimumVersion string // "" means no floor
}

// Manifest is the interpreted content of one manifest.json.
type Manifest struct {
	Name               string
	Author             string // tolerated absent: SMAPI loads such mods
	Version            string // display form, exactly as authored
	Description        string // tolerated absent: SMAPI loads such mods
	UniqueID           string
	EntryDll           string
	MinimumApiVersion  string
	MinimumGameVersion string
	VersionUnresolved  bool // Version is the %ProjectVersion% build placeholder
	UpdateKeys         []string
	Dependencies       []Dependency
	ContentPackFor     *ContentPackFor
	Extra              map[string]any             // unknown fields, decoded for callers
	ExtraRaw           map[string]json.RawMessage // unknown fields, verbatim bytes for round-trip
	Raw                []byte                     // whole file verbatim, for round-trip
}

// Parse interprets one manifest.json file. It never panics; every failure
// mode is a verdict plus errors. Raw is always the input bytes verbatim.
func Parse(data []byte) (Manifest, Verdict, []FieldError) {
	m := Manifest{Extra: map[string]any{}, ExtraRaw: map[string]json.RawMessage{}, Raw: append([]byte(nil), data...)}

	var raw map[string]json.RawMessage
	decodeErr := json.Unmarshal(data, &raw)
	if decodeErr != nil {
		// SMAPI retries JSON failures with curly quotes normalized to
		// straight quotes before giving up; mirror that leniency.
		fixed := bytes.ReplaceAll(bytes.ReplaceAll(data, []byte("“"), []byte(`"`)), []byte("”"), []byte(`"`))
		if !bytes.Equal(fixed, data) {
			decodeErr = json.Unmarshal(fixed, &raw)
		}
	}
	if decodeErr != nil || raw == nil {
		msg := "manifest is not a JSON object"
		if decodeErr != nil {
			msg = fmt.Sprintf("invalid JSON: %v. This doesn't seem to be valid JSON.", decodeErr)
		}
		return m, VerdictInvalid, []FieldError{{Stage: StageSyntax, Message: msg}}
	}

	// Case-insensitive lookup, deterministic on duplicate keys.
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lower := make(map[string]json.RawMessage, len(raw))
	for _, k := range keys {
		lower[strings.ToLower(k)] = raw[k]
	}
	known := map[string]bool{
		"name": true, "author": true, "version": true, "description": true,
		"uniqueid": true, "entrydll": true, "contentpackfor": true,
		"minimumapiversion": true, "minimumgameversion": true,
		"dependencies": true, "updatekeys": true,
	}
	for _, k := range keys {
		if known[strings.ToLower(k)] {
			continue
		}
		m.ExtraRaw[k] = append([]byte(nil), raw[k]...)
		var v any
		if err := json.Unmarshal(raw[k], &v); err != nil {
			continue
		}
		m.Extra[k] = v
	}

	var warns, fails []FieldError
	warn := func(field, msg string) {
		warns = append(warns, FieldError{Field: field, Stage: StageInterpretation, Message: msg})
	}
	fail := func(field, msg string) {
		fails = append(fails, FieldError{Field: field, Stage: StageValidation, Message: msg})
	}
	get := func(name string) (json.RawMessage, bool) {
		v, ok := lower[name]
		return v, ok
	}
	isNull := func(rm json.RawMessage) bool {
		return string(bytes.TrimSpace(rm)) == "null"
	}

	// Plain text fields. A wrong-typed value warns and counts as absent.
	// Field names in errors use the manifest's canonical casing.
	text := func(field string) string {
		rm, ok := get(strings.ToLower(field))
		if !ok || isNull(rm) {
			return ""
		}
		var s string
		if err := json.Unmarshal(rm, &s); err != nil {
			warn(field, fmt.Sprintf("field '%s' must be a string", field))
			return ""
		}
		return normalizeText(s)
	}

	m.Name = sanitizeName(text("Name"))
	m.Author = text("Author")
	m.Description = text("Description")
	m.UniqueID = text("UniqueID")
	m.EntryDll = text("EntryDll")

	// Version: required; empty and all-zero count as missing, malformed
	versionMissing := true
	if rm, ok := get("version"); ok && !isNull(rm) {
		val, unresolved, missing, msg := interpretVersion(rm)
		switch {
		case msg != "":
			fails = append(fails, FieldError{Field: "Version", Stage: StageInterpretation, Message: fmt.Sprintf("field 'Version' has %s", msg)})
		case missing || isZeroVersion(val):
		default:
			versionMissing = false
			m.Version, m.VersionUnresolved = val, unresolved
		}
	}
	// Optional version floors: malformed values warn and are dropped.
	m.MinimumApiVersion = optionalVersion(get, isNull, warn, "MinimumApiVersion")
	m.MinimumGameVersion = optionalVersion(get, isNull, warn, "MinimumGameVersion")
	// Content-pack host: object form per SMAPI; the legacy string form is
	// coerced with a warning (explicit deviation: SMAPI rejects it).
	hostPresent := false
	if rm, ok := get("contentpackfor"); ok && !isNull(rm) {
		var legacy string
		if err := json.Unmarshal(rm, &legacy); err == nil {
			warn("ContentPackFor", "field 'ContentPackFor' as a string is legacy: coerced to {UniqueID}")
			uid := normalizeText(legacy)
			hostPresent = true
			host := &ContentPackFor{}
			switch {
			case uid == "":
				fail("ContentPackFor", "manifest declares ContentPackFor without its required UniqueID field.")
			case !isSlug(uid):
				fail("ContentPackFor", fmt.Sprintf("manifest has an invalid UniqueID field '%s' in ContentPackFor (IDs must only contain letters, numbers, underscores, periods, or hyphens).", uid))
			default:
				host.UniqueID = uid
			}
			m.ContentPackFor = host
		} else {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(rm, &obj); err != nil || obj == nil {
				warn("ContentPackFor", "field 'ContentPackFor' must be an object")
			} else {
				hostPresent = true
				host := &ContentPackFor{}
				uid, uok := objectText(obj, "uniqueid")
				switch {
				case !uok || uid == "":
					fail("ContentPackFor", "manifest declares ContentPackFor without its required UniqueID field.")
				case !isSlug(uid):
					fail("ContentPackFor", fmt.Sprintf("manifest has an invalid UniqueID field '%s' in ContentPackFor (IDs must only contain letters, numbers, underscores, periods, or hyphens).", uid))
				default:
					host.UniqueID = uid
				}
				nestedMinimum(obj, "ContentPackFor", &host.MinimumVersion, warn, isNull)
				m.ContentPackFor = host
			}
		}
	}

	// Dependencies: optional, but a non-array value fails the whole manifest.
	if rm, ok := get("dependencies"); ok && !isNull(rm) {
		var arr []json.RawMessage
		if err := json.Unmarshal(rm, &arr); err != nil {
			fails = append(fails, FieldError{Field: "Dependencies", Stage: StageInterpretation, Message: "field 'Dependencies' must be an array"})
		} else {
			items := arr
			for i, it := range items {
				if isNull(it) {
					fail("Dependencies", "manifest has a null entry under Dependencies.")
					continue
				}
				var obj map[string]json.RawMessage
				if err := json.Unmarshal(it, &obj); err != nil || obj == nil {
					warn("Dependencies", fmt.Sprintf("Dependencies entry %d must be an object: dropped", i))
					continue
				}
				uid, uok := objectText(obj, "uniqueid")
				if !uok || uid == "" {
					fail("Dependencies", "manifest has a Dependencies entry with no UniqueID field.")
					continue
				}
				if !isSlug(uid) {
					fail("Dependencies", fmt.Sprintf("manifest has a Dependencies entry with an invalid UniqueID field '%s' (IDs must only contain letters, numbers, underscores, periods, or hyphens).", uid))
					continue
				}
				dep := Dependency{UniqueID: uid, IsRequired: true}
				nestedMinimumIndexed(obj, "Dependencies", i, &dep.MinimumVersion, warn, isNull)
				if iv, iok := objectValue(obj, "isrequired"); iok && !isNull(iv) {
					var b bool
					if err := json.Unmarshal(iv, &b); err != nil {
						warn("Dependencies", fmt.Sprintf("Dependencies entry %d field 'IsRequired' must be a boolean: defaulted to true", i))
					} else {
						dep.IsRequired = b
					}
				}
				m.Dependencies = append(m.Dependencies, dep)
			}
		}
	}

	// Update keys: optional; non-strings warn and are skipped, blanks filter
	// silently, everything else is preserved verbatim (trimmed).
	if rm, ok := get("updatekeys"); ok && !isNull(rm) {
		var items []json.RawMessage
		if err := json.Unmarshal(rm, &items); err != nil {
			warn("UpdateKeys", "field 'UpdateKeys' must be an array")
		} else {
			for i, it := range items {
				var s string
				if err := json.Unmarshal(it, &s); err != nil {
					warn("UpdateKeys", fmt.Sprintf("UpdateKeys entry %d must be a string: dropped", i))
					continue
				}
				if t := strings.TrimSpace(s); t != "" {
					m.UpdateKeys = append(m.UpdateKeys, t)
				}
			}
		}
	}

	// Validation: required identity, then cross-field rules.
	var missing []string
	if m.Name == "" {
		missing = append(missing, "Name")
	}
	if versionMissing {
		missing = append(missing, "Version")
	}
	if m.UniqueID == "" {
		missing = append(missing, "UniqueID")
	}
	if len(missing) > 0 {
		fail("manifest", fmt.Sprintf("manifest is missing required fields (%s).", strings.Join(missing, ", ")))
	}
	if m.UniqueID != "" && !isSlug(m.UniqueID) {
		fail("UniqueID", "manifest specifies an invalid ID (IDs must only contain letters, numbers, underscores, periods, or hyphens).")
	}
	hasEntry, hasHost := m.EntryDll != "", hostPresent
	switch {
	case hasEntry && hasHost:
		fail("manifest", "manifest sets both EntryDll and ContentPackFor, which are mutually exclusive.")
	case !hasEntry && !hasHost:
		fail("manifest", "manifest has no EntryDll or ContentPackFor field; must specify one.")
	case hasEntry && invalidFilename(m.EntryDll):
		fail("EntryDll", fmt.Sprintf("manifest has invalid filename '%s' for the EntryDll field.", m.EntryDll))
	case hasEntry && !schemaEntryDLL(m.EntryDll):
		// Runtime-tolerated but schema-invalid (the published pattern is
		// stricter than SMAPI's Path.GetInvalidFileNameChars rule): keep the
		// unit, record the deviation.
		warns = append(warns, FieldError{
			Field: "EntryDll", Stage: StageValidation,
			Message: fmt.Sprintf("manifest has EntryDll '%s', which doesn't match the manifest schema pattern %s (SMAPI still loads it).", m.EntryDll, entryDllSchemaPattern),
		})
	}

	switch {
	case len(fails) > 0:
		return m, VerdictInvalid, append(warns, fails...)
	case len(warns) > 0:
		return m, VerdictPartial, warns
	default:
		return m, VerdictValid, nil
	}
}

// isZeroVersion reports whether v is all zero components ("0", "0.0",
// "0.0.0", "0.0.0.0"), which SMAPI treats as a missing version.
func isZeroVersion(v string) bool {
	core, _, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if p != "0" {
			return false
		}
	}
	return true
}

// optionalVersion interprets an optional version floor: absent and blank
// mean no floor, malformed values warn and are dropped.
func optionalVersion(get func(string) (json.RawMessage, bool), isNull func(json.RawMessage) bool, warn func(string, string), field string) string {
	lower := strings.ToLower(field)
	rm, ok := get(lower)
	if !ok || isNull(rm) {
		return ""
	}
	val, _, missing, msg := interpretVersion(rm)
	if msg != "" {
		warn(field, fmt.Sprintf("field '%s' has %s: dropped", field, msg))
		return ""
	}
	if missing {
		return ""
	}
	return val
}

// nestedMinimum reads an optional MinimumVersion property from a nested
// object (content-pack host): absent and blank mean no floor, malformed
// values warn and are dropped.
func nestedMinimum(obj map[string]json.RawMessage, field string, dst *string, warn func(string, string), isNull func(json.RawMessage) bool) {
	mv, ok := objectValue(obj, "minimumversion")
	if !ok || isNull(mv) {
		return
	}
	val, _, missing, msg := interpretVersion(mv)
	if msg != "" {
		warn(field, fmt.Sprintf("invalid MinimumVersion '%s': dropped", strings.TrimSpace(string(mv))))
	} else if !missing {
		*dst = val
	}
}

// nestedMinimumIndexed is nestedMinimum for dependency entries, naming the
// entry index in warnings.
func nestedMinimumIndexed(obj map[string]json.RawMessage, field string, i int, dst *string, warn func(string, string), isNull func(json.RawMessage) bool) {
	mv, ok := objectValue(obj, "minimumversion")
	if !ok || isNull(mv) {
		return
	}
	val, _, missing, msg := interpretVersion(mv)
	if msg != "" {
		warn(field, fmt.Sprintf("Dependencies entry %d has invalid MinimumVersion: dropped", i))
	} else if !missing {
		*dst = val
	}
}

// interpretVersion reads a version as a string or a structured object.
// It returns the display value, whether it is the unresolved build
// placeholder, whether it counts as missing, and an error message.
func interpretVersion(rm json.RawMessage) (val string, unresolved, missing bool, msg string) {
	var s string
	if err := json.Unmarshal(rm, &s); err == nil {
		t := strings.TrimSpace(s)
		switch {
		case t == "":
			return "", false, true, ""
		case t == "%ProjectVersion%":
			return t, true, false, ""
		case !validVersionString(t):
			return "", false, false, fmt.Sprintf("invalid version '%s': want like 1.2, 1.2.30, or 1.2.30-beta", t)
		default:
			return t, false, false, ""
		}
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(rm, &obj); err != nil || obj == nil {
		if strings.TrimSpace(string(rm)) == "null" {
			return "", false, true, ""
		}
		return "", false, false, "invalid version: must be a version string or object"
	}
	lower := map[string]json.RawMessage{}
	for k, v := range obj {
		lower[strings.ToLower(k)] = v
	}
	// SMAPI's SemanticVersionConverter reads each component with
	// ValueIgnoreCase<int>, which yields 0 for an absent key (and throws for a
	// present-but-unconvertible one), so an omitted component defaults to 0.
	num := func(name string) (int, bool) {
		v, ok := lower[name]
		if !ok {
			return 0, true
		}
		var n float64
		if err := json.Unmarshal(v, &n); err != nil || n != float64(int(n)) || n < 0 {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return 0, false
			}
			i, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil || i < 0 {
				return 0, false
			}
			return i, true
		}
		return int(n), true
	}
	major, ok := num("majorversion")
	if !ok {
		return "", false, false, "invalid version object: MajorVersion must be a non-negative integer"
	}
	minor, ok := num("minorversion")
	if !ok {
		return "", false, false, "invalid version object: MinorVersion must be a non-negative integer"
	}
	patch, ok := num("patchversion")
	if !ok {
		return "", false, false, "invalid version object: PatchVersion must be a non-negative integer"
	}
	out := fmt.Sprintf("%d.%d.%d", major, minor, patch)
	if v, ok := lower["prereleasetag"]; ok && strings.TrimSpace(string(v)) != "null" {
		var pre string
		if err := json.Unmarshal(v, &pre); err != nil || strings.TrimSpace(pre) == "" {
			return "", false, false, "invalid version object: PrereleaseTag must be a non-empty string"
		}
		out += "-" + strings.TrimSpace(pre)
	}
	if !validVersionString(out) {
		return "", false, false, fmt.Sprintf("invalid version object '%s'", out)
	}
	return out, false, false, ""
}

// validVersionString reports whether s is a SMAPI-style version: one to four
// numeric components without leading zeros, plus an optional prerelease tag.
func validVersionString(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) {
			return false
		}
	}
	core, pre, _ := strings.Cut(s, "-")
	if pre == "" && strings.Contains(s, "-") {
		return false
	}
	parts := strings.Split(core, ".")
	if len(parts) < 1 || len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// entryDllSchemaPattern is the published manifest schema's EntryDll pattern.
// It is frozen to the schema, ASCII-only and case-sensitive, and is stricter
// than the runtime filename rule SMAPI actually enforces.
const entryDllSchemaPattern = `^[a-zA-Z0-9_.-]+\.dll$`

// entryDllSchemaPatternRE is entryDllSchemaPattern compiled once.
var entryDllSchemaPatternRE = regexp.MustCompile(entryDllSchemaPattern)

// schemaEntryDLL reports whether name matches entryDllSchemaPattern. The
// verdict comes from invalidFilename (SMAPI's runtime rule); this predicate
// only decides whether the schema deviation is worth a warning.
func schemaEntryDLL(name string) bool {
	return entryDllSchemaPatternRE.MatchString(name)
}

// invalidFilename reports whether name carries path or control characters,
// the runtime rule SMAPI itself enforces for the entry assembly.
func invalidFilename(name string) bool {
	if strings.ContainsAny(name, `/\:*?"<>|`) {
		return true
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// isSlug reports whether s is a valid Unique ID: Unicode letters and digits
// plus underscore, period, or hyphen.
func isSlug(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r == '_' || r == '.' || r == '-':
		case unicode.IsLetter(r) || unicode.IsDigit(r):
		default:
			return false
		}
	}
	return true
}

// normalizeText trims surrounding space and folds newlines to spaces.
func normalizeText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.ReplaceAll(s, "\n", " ")
}

// sanitizeName mirrors SMAPI's log-safety rewrite of brackets in mod names.
func sanitizeName(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "[", "("), "]", ")")
}

// objectText reads a case-insensitive string property from a JSON object.
func objectText(obj map[string]json.RawMessage, name string) (string, bool) {
	v, ok := objectValue(obj, name)
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", false
	}
	return normalizeText(s), true
}

// objectValue reads a case-insensitive property from a JSON object.
func objectValue(obj map[string]json.RawMessage, name string) (json.RawMessage, bool) {
	for k, v := range obj {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return nil, false
}
