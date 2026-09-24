package settings

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/approot"
	"github.com/Zendevve/astradew/internal/store"
)

// openService opens a real migrated database under a temp root and returns a
// Service over its handle. The database is real so persistence reads live
// state; the store is closed by cleanup.
func openService(t *testing.T) (*Service, *store.Store, approot.Paths) {
	t.Helper()
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db, err := store.Open(paths)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db.DB()), db, paths
}

// reopen closes db and reopens the same database file, so a test can prove a
// write survives a full close/reopen round trip.
func reopen(t *testing.T, db *store.Store, paths approot.Paths) *store.Store {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := store.Open(paths)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	return reopened
}

// codeOf extracts the typed code from an error, failing the test when the
// error carries none.
func codeOf(t *testing.T, err error) apperror.Code {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error %#v carries no typed code", err)
	}
	return appErr.Code
}

// Every registry declaration resolves to its Go default before anything is
// written — no rows, no special cases.
func TestGetReturnsDefaultWhenUnset(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.TODO()
	for _, decl := range Registry {
		got, err := svc.Get(ctx, decl.Name)
		if err != nil {
			t.Fatalf("Get(%q) error = %v", decl.Name, err)
		}
		if !jsonEqual(got, decl.Default) {
			t.Fatalf("Get(%q) = %#v, want default %#v", decl.Name, got, decl.Default)
		}
	}
}

// A write survives close/reopen: set, close, reopen, read back.
func TestSetPersistsAcrossReopen(t *testing.T) {
	svc, db, paths := openService(t)
	ctx := context.TODO()
	if err := svc.Set(ctx, "theme", json.RawMessage(`"dark"`)); err != nil {
		t.Fatalf("Set(theme) error = %v", err)
	}
	if err := svc.Set(ctx, "ui-scale", json.RawMessage(`150`)); err != nil {
		t.Fatalf("Set(ui-scale) error = %v", err)
	}
	if err := svc.Set(ctx, "check-updates-on-start", json.RawMessage(`false`)); err != nil {
		t.Fatalf("Set(check-updates-on-start) error = %v", err)
	}

	reopened := reopen(t, db, paths)
	after := New(reopened.DB())
	for key, want := range map[string]any{"theme": "dark", "ui-scale": 150, "check-updates-on-start": false} {
		got, err := after.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get(%q) after reopen error = %v", key, err)
		}
		if !jsonEqual(got, want) {
			t.Fatalf("Get(%q) after reopen = %#v, want %#v", key, got, want)
		}
	}
}

// Addressing a key no build declares refuses with SETTING_UNKNOWN — never a
// default, never a crash — on both Get and Set.
func TestUnknownKeyRefusesOnGetAndSet(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.TODO()
	if _, err := svc.Get(ctx, "no-such-setting"); codeOf(t, err) != apperror.CodeSettingUnknown {
		t.Fatalf("Get(unknown) code = %v, want SETTING_UNKNOWN", err)
	}
	if err := svc.Set(ctx, "no-such-setting", json.RawMessage(`1`)); codeOf(t, err) != apperror.CodeSettingUnknown {
		t.Fatalf("Set(unknown) code = %v, want SETTING_UNKNOWN", err)
	}
}

// Invalid values refuse with recoverable SETTING_INVALID and the stored value
// stays exactly what it was: read before, attempt the write, read after.
func TestInvalidSetRejectedWithOldValueIntact(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.TODO()
	if err := svc.Set(ctx, "theme", json.RawMessage(`"dark"`)); err != nil {
		t.Fatalf("Set(theme) error = %v", err)
	}
	before, err := svc.Get(ctx, "theme")
	if err != nil {
		t.Fatalf("Get(theme) error = %v", err)
	}

	invalid := map[string]json.RawMessage{
		"theme":                  json.RawMessage(`"sepia"`),
		"ui-scale":               json.RawMessage(`500`),
		"check-updates-on-start": json.RawMessage(`"yes"`),
	}
	for key, raw := range invalid {
		beforeValue, err := svc.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get(%q) error = %v", key, err)
		}
		setErr := svc.Set(ctx, key, raw)
		if codeOf(t, setErr) != apperror.CodeSettingInvalid {
			t.Fatalf("Set(%q, %s) code = %v, want SETTING_INVALID", key, raw, setErr)
		}
		var appErr *apperror.AppError
		if !errors.As(setErr, &appErr) || !appErr.Recoverable {
			t.Fatalf("Set(%q) error %#v is not recoverable", key, setErr)
		}
		after, err := svc.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get(%q) error = %v", key, err)
		}
		if !jsonEqual(after, beforeValue) {
			t.Fatalf("Get(%q) after rejected Set = %#v, want preserved %#v", key, after, beforeValue)
		}
	}
	if after, _ := svc.Get(ctx, "theme"); !jsonEqual(after, before) {
		t.Fatalf("Get(theme) after rejected Set = %#v, want preserved %#v", after, before)
	}
}

// Rows this build's registry does not declare survive a GetAll/Set round
// trip byte-untouched: the package never reads, validates, or rewrites them.
func TestUnrecognisedRowsPreservedByteUntouched(t *testing.T) {
	svc, db, _ := openService(t)
	ctx := context.TODO()
	foreignKey, foreignValue := "future-setting", `{"nested":["json"]}`
	if _, err := db.DB().ExecContext(ctx, "INSERT INTO settings(key, value) VALUES(?, ?)", foreignKey, foreignValue); err != nil {
		t.Fatalf("inserting foreign row: %v", err)
	}

	if _, err := svc.GetAll(ctx); err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	if err := svc.Set(ctx, "theme", json.RawMessage(`"light"`)); err != nil {
		t.Fatalf("Set(theme) error = %v", err)
	}
	if _, err := svc.GetAll(ctx); err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}

	var got string
	if err := db.DB().QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", foreignKey).Scan(&got); err != nil {
		t.Fatalf("reading foreign row: %v", err)
	}
	if got != foreignValue {
		t.Fatalf("foreign row = %q, want byte-identical %q", got, foreignValue)
	}
}

// GetAll lists every declaration with its current value and whether it is
// still the default — EXCEPT the primary-install pointer, whose only editor
// is the installs chooser. Get still reads the pointer.
func TestGetAllListsRegistryWithCurrentValues(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.TODO()
	if err := svc.Set(ctx, "theme", json.RawMessage(`"dark"`)); err != nil {
		t.Fatalf("Set(theme) error = %v", err)
	}
	all, err := svc.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	if len(all) != len(Registry)-1 {
		t.Fatalf("GetAll() returned %d values, want %d registry entries minus the pointer key", len(all), len(Registry)-1)
	}
	byName := make(map[string]Value, len(all))
	for _, value := range all {
		byName[value.Name] = value
	}
	if _, found := byName[PrimaryGameInstallIDKey]; found {
		t.Fatalf("GetAll() lists %q, want it excluded: the chooser is its only editor", PrimaryGameInstallIDKey)
	}
	theme := byName["theme"]
	if !jsonEqual(theme.Value, "dark") || theme.IsDefault {
		t.Fatalf("theme = %#v, want dark non-default", theme)
	}
	scale := byName["ui-scale"]
	if !jsonEqual(scale.Value, 100) || !scale.IsDefault {
		t.Fatalf("ui-scale = %#v, want 100 default", scale)
	}
}

// The pointer key is readable through Get, preserved untouched by GetAll/Set
// round trips on other keys, and validates >= 1 on Set.
func TestPrimaryPointerReadableButExcluded(t *testing.T) {
	svc, db, _ := openService(t)
	ctx := context.TODO()
	got, err := svc.Get(ctx, PrimaryGameInstallIDKey)
	if err != nil {
		t.Fatalf("Get(pointer) error = %v", err)
	}
	if !jsonEqual(got, 0) {
		t.Fatalf("Get(pointer) = %#v, want unset default 0", got)
	}
	if err := svc.Set(ctx, "theme", json.RawMessage(`"dark"`)); err != nil {
		t.Fatalf("Set(theme) error = %v", err)
	}
	if _, err := svc.GetAll(ctx); err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	got, err = svc.Get(ctx, PrimaryGameInstallIDKey)
	if err != nil {
		t.Fatalf("Get(pointer) after round trip error = %v", err)
	}
	if !jsonEqual(got, 0) {
		t.Fatalf("Get(pointer) after round trip = %#v, want untouched 0", got)
	}
	if err := svc.Set(ctx, PrimaryGameInstallIDKey, json.RawMessage(`0`)); codeOf(t, err) != apperror.CodeSettingInvalid {
		t.Fatalf("Set(pointer, 0) code = %v, want SETTING_INVALID", err)
	}
	if err := svc.Set(ctx, PrimaryGameInstallIDKey, json.RawMessage(`3`)); err != nil {
		t.Fatalf("Set(pointer, 3) error = %v", err)
	}
	var stored string
	if err := db.DB().QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", PrimaryGameInstallIDKey).Scan(&stored); err != nil {
		t.Fatalf("reading pointer row: %v", err)
	}
	if stored != "3" {
		t.Fatalf("pointer row = %q, want %q", stored, "3")
	}
}

// jsonEqual compares through canonical JSON so int decodes and bool/string
// values compare by content, not Go type.
func jsonEqual(a, b any) bool {
	encodedA, err := json.Marshal(a)
	if err != nil {
		return false
	}
	encodedB, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(encodedA) == string(encodedB)
}

// The constraint the ticket pins: every stored value is a JSON scalar —
// string, bool, or int — so decoding to a driver string is always enough.
func TestStoredValuesAreJSONScalars(t *testing.T) {
	svc, db, _ := openService(t)
	ctx := context.TODO()
	if err := svc.Set(ctx, "theme", json.RawMessage(`"dark"`)); err != nil {
		t.Fatalf("Set(theme) error = %v", err)
	}
	rows, err := db.DB().QueryContext(ctx, "SELECT key, value FROM settings")
	if err != nil {
		t.Fatalf("querying settings: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			t.Fatalf("scanning row: %v", err)
		}
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatalf("row %q value %q is not JSON: %v", key, raw, err)
		}
		switch value.(type) {
		case string, bool, float64:
		default:
			t.Fatalf("row %q value %q is not a JSON scalar", key, raw)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating rows: %v", err)
	}
}
