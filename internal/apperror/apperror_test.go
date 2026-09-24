package apperror

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNewHasCodeMessageAndDefaults(t *testing.T) {
	err := New(CodeProbeFailure, "probe failure")
	if err.Code != CodeProbeFailure {
		t.Fatalf("Code = %q, want %q", err.Code, CodeProbeFailure)
	}
	if err.Message != "probe failure" {
		t.Fatalf("Message = %q, want %q", err.Message, "probe failure")
	}
	if err.Details != "" {
		t.Fatalf("Details = %q, want empty", err.Details)
	}
	if err.Recoverable {
		t.Fatal("Recoverable = true, want false")
	}
}

func TestNewRecoverableCarriesDetails(t *testing.T) {
	err := NewRecoverable(CodeProbeFailure, "probe failure", "demo probe")
	if err.Code != CodeProbeFailure || err.Message != "probe failure" {
		t.Fatalf("got %+v, want code/message preserved", err)
	}
	if err.Details != "demo probe" {
		t.Fatalf("Details = %q, want %q", err.Details, "demo probe")
	}
	if !err.Recoverable {
		t.Fatal("Recoverable = false, want true")
	}
}

func TestErrorPrefixesMessageWithCode(t *testing.T) {
	if got := New(CodeProbeFailure, "probe failure").Error(); got != "PROBE_FAILURE: probe failure" {
		t.Fatalf("Error() = %q, want %q", got, "PROBE_FAILURE: probe failure")
	}
	if got := (*AppError)(nil).Error(); got != "" {
		t.Fatalf("nil Error() = %q, want empty", got)
	}
}

// The frontend binds to these JSON keys, so the wire shape is asserted here
// rather than assumed.
func TestAppErrorMarshalsToTheContractKeys(t *testing.T) {
	encoded, err := json.Marshal(NewRecoverable(CodeProbeFailure, "probe failure", "demo probe"))
	if err != nil {
		t.Fatalf("marshalling AppError: %v", err)
	}
	const want = `{"code":"PROBE_FAILURE","message":"probe failure","details":"demo probe","recoverable":true}`
	if string(encoded) != want {
		t.Fatalf("AppError JSON = %s, want %s", encoded, want)
	}
}

// The code must survive the marshalling boundary intact: what MarshalError
// emits for the error is what the TypeScript rejection's cause decodes to.
func TestCodeSurvivesMarshalBoundary(t *testing.T) {
	before := NewRecoverable(CodeProbeFailure, "probe failure", "demo probe")

	raw := MarshalError(before)
	if raw == nil {
		t.Fatal("MarshalError returned nil for *AppError")
	}
	var after AppError
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatalf("unmarshalling marshalled error: %v", err)
	}
	if after.Code != before.Code {
		t.Fatalf("code out = %q, want code in %q", after.Code, before.Code)
	}
	if after != *before {
		t.Fatalf("round trip = %+v, want %+v", after, *before)
	}
}

func TestMarshalErrorFallsBackForForeignErrors(t *testing.T) {
	if raw := MarshalError(errors.New("boom")); raw != nil {
		t.Fatalf("MarshalError(foreign) = %s, want nil so Wails uses its default marshaller", raw)
	}
	if raw := MarshalError(nil); raw != nil {
		t.Fatalf("MarshalError(nil) = %s, want nil", raw)
	}
}
