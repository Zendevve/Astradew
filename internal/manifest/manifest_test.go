package manifest

import (
	"strings"
	"testing"
)

func validCodeMod(extra string) string {
	return `{
		"Name": "Lookup Anything",
		"Author": "Pathoschild",
		"Version": "1.52.0",
		"MinimumApiVersion": "4.3.1",
		"MinimumGameVersion": "1.6.15",
		"Description": "View metadata about anything by pressing a button.",
		"UniqueID": "Pathoschild.LookupAnything",
		"EntryDll": "LookupAnything.dll",
		"UpdateKeys": ["Nexus:541"]` + extra + `
	}`
}

func TestParseValidCodeMod(t *testing.T) {
	m, v, errs := Parse([]byte(validCodeMod("")))
	if v != VerdictValid {
		t.Fatalf("verdict = %q, want valid (errs=%v)", v, errs)
	}
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	if m.Name != "Lookup Anything" || m.Author != "Pathoschild" || m.Version != "1.52.0" {
		t.Fatalf("identity fields = %+v", m)
	}
	if m.UniqueID != "Pathoschild.LookupAnything" || m.EntryDll != "LookupAnything.dll" {
		t.Fatalf("id/entry = %+v", m)
	}
	if len(m.UpdateKeys) != 1 || m.UpdateKeys[0] != "Nexus:541" {
		t.Fatalf("update keys = %v", m.UpdateKeys)
	}
	if m.MinimumApiVersion != "4.3.1" || m.MinimumGameVersion != "1.6.15" {
		t.Fatalf("minimums = %+v", m)
	}
	if len(m.Extra) != 0 {
		t.Fatalf("extra = %v, want empty", m.Extra)
	}
}

func TestParseValidContentPack(t *testing.T) {
	data := `{
		"Name": "Stardew Valley Expanded",
		"Author": "FlashShifter",
		"Version": "1.15.11",
		"Description": "An expansive fanmade mod.",
		"UniqueID": "FlashShifter.StardewValleyExpandedCP",
		"ContentPackFor": {"UniqueID": "Pathoschild.ContentPatcher"},
		"Dependencies": [
			{"UniqueID": "FlashShifter.SVE-FTM", "IsRequired": true},
			{"UniqueID": "MoreFish", "IsRequired": false}
		],
		"UpdateKeys": ["Nexus:???"]
	}`
	m, v, errs := Parse([]byte(data))
	if v != VerdictValid {
		t.Fatalf("verdict = %q, want valid (errs=%v)", v, errs)
	}
	if m.ContentPackFor == nil || m.ContentPackFor.UniqueID != "Pathoschild.ContentPatcher" {
		t.Fatalf("host = %+v", m.ContentPackFor)
	}
	if len(m.Dependencies) != 2 {
		t.Fatalf("deps = %+v", m.Dependencies)
	}
	if m.Dependencies[0].UniqueID != "FlashShifter.SVE-FTM" || !m.Dependencies[0].IsRequired {
		t.Fatalf("dep0 = %+v", m.Dependencies[0])
	}
	if m.Dependencies[1].UniqueID != "MoreFish" || m.Dependencies[1].IsRequired {
		t.Fatalf("dep1 = %+v", m.Dependencies[1])
	}
	if len(m.UpdateKeys) != 1 || m.UpdateKeys[0] != "Nexus:???" {
		t.Fatalf("placeholder key not preserved verbatim: %v", m.UpdateKeys)
	}
}

func TestParseRequiredIdentityInvalid(t *testing.T) {
	cases := []struct {
		name string
		data func() string
		want string
	}{
		{"missing name", func() string {
			return `{"Author":"A","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll"}`
		}, "missing required fields"},
		{"missing version", func() string {
			return `{"Name":"N","Author":"A","UniqueID":"A.B","EntryDll":"A.dll"}`
		}, "missing required fields"},
		{"zero version", func() string {
			return `{"Name":"N","Author":"A","Version":"0.0.0","UniqueID":"A.B","EntryDll":"A.dll"}`
		}, "missing required fields"},
		{"missing unique id", func() string {
			return `{"Name":"N","Author":"A","Version":"1.0.0","EntryDll":"A.dll"}`
		}, "missing required fields"},
		{"bad slug", func() string {
			return `{"Name":"N","Author":"A","Version":"1.0.0","UniqueID":"Bad ID!","EntryDll":"A.dll"}`
		}, "invalid ID"},
		{"both entry and host", func() string {
			return `{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll","ContentPackFor":{"UniqueID":"H.H"}}`
		}, "mutually exclusive"},
		{"neither entry nor host", func() string {
			return `{"Name":"N","Version":"1.0.0","UniqueID":"A.B"}`
		}, "must specify one"},
		{"host without id", func() string {
			return `{"Name":"N","Version":"1.0.0","UniqueID":"A.B","ContentPackFor":{}}`
		}, "without its required UniqueID"},
		{"entry with path", func() string {
			return `{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"sub/dir.dll"}`
		}, "invalid filename"},
		{"garbage version", func() string {
			return `{"Name":"N","Version":"abc","UniqueID":"A.B","EntryDll":"A.dll"}`
		}, "invalid version"},
		{"numeric version", func() string {
			return `{"Name":"N","Version":123,"UniqueID":"A.B","EntryDll":"A.dll"}`
		}, "Version"},
		{"numeric name", func() string {
			return `{"Name":123,"Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll"}`
		}, "Name"},
		{"null dep entry", func() string {
			return `{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll","Dependencies":[null]}`
		}, "null entry"},
		{"dep missing id", func() string {
			return `{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll","Dependencies":[{"IsRequired":true}]}`
		}, "no UniqueID"},
		{"deps not array", func() string {
			return `{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll","Dependencies":"x"}`
		}, "must be an array"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, v, errs := Parse([]byte(c.data()))
			if v != VerdictInvalid {
				t.Fatalf("verdict = %q, want invalid (errs=%v)", v, errs)
			}
			found := false
			for _, e := range errs {
				if strings.Contains(e.Message, c.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no error containing %q in %v", c.want, errs)
			}
		})
	}
}
func TestParseMalformedOptionalPartial(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"object description", `{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll","Description":{"x":1}}`},
		{"string update keys", `{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll","UpdateKeys":"Nexus:1"}`},
		{"numeric minimum", `{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll","MinimumApiVersion":123}`},
		{"string dep item", `{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll","Dependencies":["oops"]}`},
		{"bad dep floor", `{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll","Dependencies":[{"UniqueID":"H.H","MinimumVersion":"abc"}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, v, errs := Parse([]byte(c.data))
			if v != VerdictPartial {
				t.Fatalf("verdict = %q, want partial (errs=%v)", v, errs)
			}
			if m.Name != "N" || m.UniqueID != "A.B" {
				t.Fatalf("valid fields did not survive: %+v", m)
			}
			if len(errs) == 0 {
				t.Fatal("want at least one warning")
			}
		})
	}
}

func TestParseUnknownFieldsPreserved(t *testing.T) {
	data := validCodeMod(`,"Foo": {"a":1}, "Bar": "x"`)
	raw := []byte(data)
	m, v, _ := Parse(raw)
	if v != VerdictValid {
		t.Fatalf("verdict = %q, want valid", v)
	}
	if string(m.Raw) != data {
		t.Fatal("raw bytes not preserved verbatim")
	}
	if _, ok := m.Extra["Foo"]; !ok {
		t.Fatalf("Foo missing from extra: %v", m.Extra)
	}
	if _, ok := m.Extra["Bar"]; !ok {
		t.Fatalf("Bar missing from extra: %v", m.Extra)
	}
	if string(m.ExtraRaw["Foo"]) != `{"a":1}` {
		t.Fatalf("Foo verbatim = %q, want original bytes", m.ExtraRaw["Foo"])
	}
	if string(m.ExtraRaw["Bar"]) != `"x"` {
		t.Fatalf("Bar verbatim = %q, want original bytes", m.ExtraRaw["Bar"])
	}
}

func TestParseCurlyQuotesRetried(t *testing.T) {
	data := "{“Name”: “N”, “Version”: “1.0.0”, “UniqueID”: “A.B”, “EntryDll”: “A.dll”}"
	m, v, errs := Parse([]byte(data))
	if v != VerdictValid {
		t.Fatalf("verdict = %q, want valid (errs=%v)", v, errs)
	}
	if m.Name != "N" || m.UniqueID != "A.B" {
		t.Fatalf("fields = %+v", m)
	}
}

func TestParseCaseInsensitiveAndNormalized(t *testing.T) {
	data := `{
		"name": "  [My]\nMod\rX  ",
		"author": " A ",
		"version": "1.0.0",
		"uniqueid": "a.b",
		"entrydll": "A.dll"
	}`
	m, v, errs := Parse([]byte(data))
	if v != VerdictValid {
		t.Fatalf("verdict = %q, want valid (errs=%v)", v, errs)
	}
	if m.Name != "(My) Mod X" {
		t.Fatalf("name = %q, want bracket rewrite + newline fold", m.Name)
	}
	if m.Author != "A" || m.UniqueID != "a.b" || m.EntryDll != "A.dll" {
		t.Fatalf("fields = %+v", m)
	}
}

func TestParseStagesDistinguishable(t *testing.T) {
	_, v, errs := Parse([]byte(`not json`))
	if v != VerdictInvalid || len(errs) != 1 || errs[0].Stage != StageSyntax {
		t.Fatalf("syntax case = %q %v", v, errs)
	}
	_, v, errs = Parse([]byte(`{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll","Description":{}}`))
	if v != VerdictPartial {
		t.Fatalf("interpretation case verdict = %q", v)
	}
	found := false
	for _, e := range errs {
		if e.Stage == StageInterpretation {
			found = true
		}
	}
	if !found {
		t.Fatalf("no interpretation-stage error in %v", errs)
	}
	_, v, errs = Parse([]byte(`{"Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll"}`))
	if v != VerdictInvalid {
		t.Fatalf("validation case verdict = %q", v)
	}
	found = false
	for _, e := range errs {
		if e.Stage == StageValidation {
			found = true
		}
	}
	if !found {
		t.Fatalf("no validation-stage error in %v", errs)
	}
}

func TestParseProjectVersionMarker(t *testing.T) {
	data := `{"Name":"N","Version":"%ProjectVersion%","UniqueID":"A.B","EntryDll":"A.dll"}`
	m, v, errs := Parse([]byte(data))
	if v != VerdictValid {
		t.Fatalf("verdict = %q, want valid (errs=%v)", v, errs)
	}
	if !m.VersionUnresolved || m.Version != "%ProjectVersion%" {
		t.Fatalf("marker not recorded: %+v", m)
	}
}

func TestParseAuthorDescriptionAbsent(t *testing.T) {
	data := `{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll"}`
	m, v, errs := Parse([]byte(data))
	if v != VerdictValid {
		t.Fatalf("verdict = %q, want valid (errs=%v)", v, errs)
	}
	if m.Author != "" || m.Description != "" {
		t.Fatalf("absent optionals = %+v", m)
	}
}

func TestParseUpdateKeysBlanksFiltered(t *testing.T) {
	data := `{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll","UpdateKeys":["", "  ", "Nexus:541"]}`
	m, v, _ := Parse([]byte(data))
	if v != VerdictValid {
		t.Fatalf("verdict = %q, want valid (blanks filter silently)", v)
	}
	if len(m.UpdateKeys) != 1 || m.UpdateKeys[0] != "Nexus:541" {
		t.Fatalf("keys = %v", m.UpdateKeys)
	}
}

func FuzzParse(f *testing.F) {
	seeds := []string{
		`{"Name":"N","Version":"1.0.0","UniqueID":"A.B","EntryDll":"A.dll"}`,
		`not json`,
		`{}`,
		`[]`,
		`{"Name":123}`,
		" engines\xff\xfe broken",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		m, v, _ := Parse(data)
		switch v {
		case VerdictValid, VerdictPartial, VerdictInvalid:
		default:
			t.Fatalf("bad verdict %q", v)
		}
		if string(m.Raw) != string(data) {
			t.Fatal("raw not preserved")
		}
	})
}
