package app

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Zendevve/astradew/internal/apperror"
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

// ProbeFailure must report a zero Info plus a PROBE_FAILURE error so the
// frontend has a typed code to branch on.
func TestProbeFailureReturnsZeroInfoAndCodedError(t *testing.T) {
	info, err := New("Astradew", "1.4.2").ProbeFailure()
	if info != (Info{}) {
		t.Fatalf("ProbeFailure Info = %+v, want zero Info", info)
	}
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("ProbeFailure err = %#v, want *apperror.AppError", err)
	}
	if appErr.Code != apperror.CodeProbeFailure {
		t.Fatalf("ProbeFailure code = %q, want %q", appErr.Code, apperror.CodeProbeFailure)
	}
}

// The code must survive the real boundary mechanism: the ProbeFailure error
// is routed through the bound method's marshalError hook (the per-service
// MarshalError path), whose output is what lands on the TypeScript
// rejection's cause. Code out must equal code in.
func TestProbeFailureCodeSurvivesBoundaryMarshaller(t *testing.T) {
	_ = application.New(application.Options{})
	bindings := application.NewBindings(nil, nil)
	svc := New("Astradew", "1.4.2")
	if err := bindings.Add(application.NewServiceWithOptions(svc, application.ServiceOptions{
		MarshalError: apperror.MarshalError,
	})); err != nil {
		t.Fatalf("bindings.Add() error = %v", err)
	}

	bound := bindings.Get(&application.CallOptions{
		MethodName: "github.com/Zendevve/astradew/internal/app.ApplicationService.ProbeFailure",
	})
	if bound == nil {
		t.Fatal("bound ProbeFailure method not found")
	}

	_, err := bound.Call(context.TODO(), nil)
	var callErr *application.CallError
	if !errors.As(err, &callErr) {
		t.Fatalf("Call err = %#v, want *application.CallError", err)
	}
	if callErr.Kind != application.RuntimeError {
		t.Fatalf("CallError.Kind = %q, want RuntimeError", callErr.Kind)
	}

	cause, marshalErr := json.Marshal(callErr.Cause)
	if marshalErr != nil {
		t.Fatalf("marshalling CallError cause: %v", marshalErr)
	}
	var decoded apperror.AppError
	if err := json.Unmarshal(cause, &decoded); err != nil {
		t.Fatalf("decoding cause %s: %v", cause, err)
	}
	if decoded.Code != apperror.CodeProbeFailure {
		t.Fatalf("code out = %q, want code in %q", decoded.Code, apperror.CodeProbeFailure)
	}
}
