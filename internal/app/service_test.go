package app

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/Zendevve/astradew/internal/buildinfo"
)

func TestServiceReportsTheIdentityItWasConstructedWith(t *testing.T) {
	tests := []struct {
		name        string
		productName string
		version     string
	}{
		{name: "release build", productName: "Astradew", version: "1.4.2"},
		{name: "development build", productName: "Astradew", version: "0.0.0-dev"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := New(test.productName, test.version).Info()
			if got.Name != test.productName || got.Version != test.version {
				t.Fatalf("Info() = %+v, want {Name:%q Version:%q}", got, test.productName, test.version)
			}
		})
	}
}

// The generated frontend bindings read the JSON keys of Info. Renaming a field
// silently empties the window, so the wire shape is asserted here rather than
// assumed.
func TestInfoMarshalsToTheFieldNamesTheFrontendBindsTo(t *testing.T) {
	encoded, err := json.Marshal(New("Astradew", "1.4.2").Info())
	if err != nil {
		t.Fatalf("marshalling Info: %v", err)
	}

	const want = `{"name":"Astradew","version":"1.4.2"}`
	if string(encoded) != want {
		t.Fatalf("Info JSON = %s, want %s", encoded, want)
	}
}

// A build whose version is empty or unparseable shows a meaningless version in
// the window, so the version compiled into this build must be a real one.
func TestBuildVersionIsSemantic(t *testing.T) {
	info := New(buildinfo.Name, buildinfo.Version).Info()

	if info.Name != "Astradew" {
		t.Errorf("buildinfo.Name = %q, want %q", info.Name, "Astradew")
	}

	semver := regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	if !semver.MatchString(info.Version) {
		t.Errorf("buildinfo.Version = %q, want a semantic version", info.Version)
	}
}
