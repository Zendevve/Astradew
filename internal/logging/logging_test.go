package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openTestLogger builds a Logger writing into a temp dir with console output
// captured to buf.
func openTestLogger(t *testing.T, dir string, buf *bytes.Buffer, opts ...Option) *Logger {
	t.Helper()
	opts = append([]Option{WithConsole(buf)}, opts...)
	l, err := New(dir, opts...)
	if err != nil {
		t.Fatalf("New(%q) error = %v", dir, err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// readJSONLines parses every non-empty line of path as a JSON object.
func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out []map[string]any
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %q is not JSON: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func lastRecord(t *testing.T, path string) map[string]any {
	t.Helper()
	recs := readJSONLines(t, path)
	if len(recs) == 0 {
		t.Fatalf("no records in %s", path)
	}
	return recs[len(recs)-1]
}

func logPath(dir string) string { return filepath.Join(dir, logFileName) }

func TestNewCreatesDirAndWritesJSONLinesPlusConsole(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	var console bytes.Buffer
	openTestLogger(t, dir, &console).Info(context.Background(), "hello world", "k", "v")

	rec := lastRecord(t, logPath(dir))
	if rec["msg"] != "hello world" {
		t.Fatalf("file msg = %v, want %q", rec["msg"], "hello world")
	}
	if rec["k"] != "v" {
		t.Fatalf("file attr k = %v, want %q", rec["k"], "v")
	}
	if _, ok := rec["time"]; !ok {
		t.Fatal("file record has no time field; not slog JSON-lines")
	}
	if _, ok := rec["level"]; !ok {
		t.Fatal("file record has no level field; not slog JSON-lines")
	}
	if got := console.String(); !strings.Contains(got, "hello world") {
		t.Fatalf("console output missing message: %q", got)
	}
}

func TestContextCorrelationPropagation(t *testing.T) {
	dir := t.TempDir()
	var console bytes.Buffer
	l := openTestLogger(t, dir, &console)

	ctx := context.Background()
	ctx = WithOperationID(ctx, "op-1")
	ctx = ContextWithTask(ctx, "task-7")
	ctx = ContextWithTransaction(ctx, "txn-9")
	ctx = ContextWithProfile(ctx, "profile-a")
	ctx = ContextWithModUniqueID(ctx, "Author.Mod")
	ctx = ContextWithProvider(ctx, "Nexus")

	l.Info(ctx, "correlated")

	rec := lastRecord(t, logPath(dir))
	for field, want := range map[string]string{
		"operation":      "op-1",
		"task_id":        "task-7",
		"transaction_id": "txn-9",
		"profile_id":     "profile-a",
		"mod_unique_id":  "Author.Mod",
		"provider":       "Nexus",
	} {
		if rec[field] != want {
			t.Fatalf("field %s = %v, want %q (record %v)", field, rec[field], want, rec)
		}
	}
}

func TestNestedOperationIDAppearsOnNestedRecords(t *testing.T) {
	dir := t.TempDir()
	var console bytes.Buffer
	l := openTestLogger(t, dir, &console)

	top := WithOperationID(context.Background(), "op-nested")
	// Nested code adds its own fields without naming the operation again.
	nested := ContextWithTransaction(top, "txn-child")
	nested = ContextWithProfile(nested, "profile-child")
	l.Info(nested, "nested work")

	rec := lastRecord(t, logPath(dir))
	if rec["operation"] != "op-nested" {
		t.Fatalf("nested record lost operation id: %v", rec)
	}
	if rec["transaction_id"] != "txn-child" {
		t.Fatalf("nested record lost transaction id: %v", rec)
	}
	if rec["profile_id"] != "profile-child" {
		t.Fatalf("nested record lost profile id: %v", rec)
	}
}

func TestWithContextLoggerInheritsFields(t *testing.T) {
	dir := t.TempDir()
	var console bytes.Buffer
	l := openTestLogger(t, dir, &console)

	ctx := ContextWithProfile(WithOperationID(context.Background(), "op-wc"), "p-wc")
	l.WithContext(ctx).Info("child log")

	rec := lastRecord(t, logPath(dir))
	if rec["operation"] != "op-wc" || rec["profile_id"] != "p-wc" {
		t.Fatalf("WithContext record missing inherited fields: %v", rec)
	}
}

func TestLogMethodLevels(t *testing.T) {
	dir := t.TempDir()
	var console bytes.Buffer
	l := openTestLogger(t, dir, &console, WithLevel(slog.LevelDebug))

	ctx := WithOperationID(context.Background(), "op-lvl")
	l.Log(ctx, slog.LevelWarn, "warn here", "x", 1)
	l.Debug(ctx, "dbg here")
	l.Warn(ctx, "warn2")
	l.Error(ctx, "err here")

	recs := readJSONLines(t, logPath(dir))
	if len(recs) != 4 {
		t.Fatalf("got %d records, want 4", len(recs))
	}
	for _, r := range recs {
		if r["operation"] != "op-lvl" {
			t.Fatalf("Log-level record lost ctx fields: %v", r)
		}
	}
}

func TestSecretRedaction(t *testing.T) {
	dir := t.TempDir()
	var console bytes.Buffer
	l := openTestLogger(t, dir, &console)

	secrets := map[string]string{
		"apiKey":        "sk-secret-APIKEY-111",
		"api_key":       "sk-secret-APIKEY-222",
		"api-key":       "sk-secret-APIKEY-333",
		"token":         "sk-secret-TOKEN-444",
		"secret":        "sk-secret-SECRET-555",
		"password":      "sk-secret-PASSWORD-666",
		"passwd":        "sk-secret-PASSWD-777",
		"authorization": "sk-secret-AUTHZ-888",
		"auth":          "sk-secret-AUTH-999",
		"credential":    "sk-secret-CRED-000",
		"Password":      "sk-secret-CASE-abc",
		"X-Auth-Token":  "sk-secret-MIXED-def",
	}
	args := make([]any, 0, len(secrets)*2)
	for k, v := range secrets {
		args = append(args, k, v)
	}
	l.Info(context.Background(), "login attempt", args...)
	// Grouped secrets must be redacted too.
	l.Info(context.Background(), "grouped", slog.Group("cfg", slog.String("password", "sk-secret-GROUPED-xyz")))

	raw, err := os.ReadFile(logPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	fileText := string(raw)
	consoleText := console.String()
	for _, s := range secrets {
		if strings.Contains(fileText, s) {
			t.Fatalf("secret %q reached the log file", s)
		}
		if strings.Contains(consoleText, s) {
			t.Fatalf("secret %q reached the console output", s)
		}
	}
	if strings.Contains(fileText, "sk-secret-GROUPED-xyz") {
		t.Fatal("grouped secret reached the log file")
	}
	if strings.Contains(consoleText, "sk-secret-GROUPED-xyz") {
		t.Fatal("grouped secret reached the console output")
	}
	if !strings.Contains(fileText, redactedValue) {
		t.Fatalf("file has no %q marker; redaction did not emit", redactedValue)
	}

	// Non-secret values must survive redaction.
	recs := readJSONLines(t, logPath(dir))
	if recs[0]["apiKey"] != redactedValue {
		t.Fatalf("apiKey not redacted in file: %v", recs[0]["apiKey"])
	}

	dir2 := t.TempDir()
	var console2 bytes.Buffer
	l2 := openTestLogger(t, dir2, &console2)
	l2.Info(context.Background(), "plain", "provider", "Nexus", "operation", "install")
	rec2 := lastRecord(t, logPath(dir2))
	if rec2["provider"] != "Nexus" || rec2["operation"] != "install" {
		t.Fatalf("non-secret attrs altered: %v", rec2)
	}
}

// TestRotationBoundedDiskUse writes past the cap and asserts disk use stays
// bounded by MaxFiles*MaxBytes with at most MaxFiles files retained.
func TestRotationBoundedDiskUse(t *testing.T) {
	dir := t.TempDir()
	var console bytes.Buffer
	const maxBytes = 1024
	const maxFiles = 3
	l := openTestLogger(t, dir, &console, WithMaxBytes(maxBytes), WithMaxFiles(maxFiles))

	pad := strings.Repeat("p", 64)
	for i := range 300 {
		l.Info(context.Background(), "rotation pressure", "i", i, "pad", pad)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > maxFiles {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("retained %d files %v, want at most %d", len(entries), names, maxFiles)
	}
	var total int64
	for _, e := range entries {
		st, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		total += st.Size()
	}
	if bound := int64(maxFiles * maxBytes); total > bound {
		t.Fatalf("disk use %d bytes exceeds bound %d (files=%d)", total, bound, len(entries))
	}
	if _, err := os.Stat(logPath(dir)); err != nil {
		t.Fatalf("active log missing after rotation: %v", err)
	}
	// Records must still be valid JSON-lines after rotation.
	if recs := readJSONLines(t, logPath(dir)); len(recs) == 0 {
		t.Fatal("active log has no parseable records after rotation")
	}
}
