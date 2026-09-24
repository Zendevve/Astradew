// Package logging provides Astradew's structured application logger.
//
// The logger writes JSON-lines records to astradew.log for diagnostics and
// human-readable records to the console, redacts secret-bearing attributes,
// propagates correlation fields through context, and bounds disk use with
// size/file-count capped rotation. It depends only on the standard library.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// logFileName is the base name of the active log file.
	logFileName = "astradew.log"
	// redactedValue replaces secret-bearing attribute values.
	redactedValue = "[redacted]"
	// defaultMaxBytes caps a single log file at 1 MiB.
	defaultMaxBytes = 1 << 20
	// defaultMaxFiles caps the number of retained log files.
	defaultMaxFiles = 5
)

// ctxKey is the context key type for correlation fields. It is unexported so
// only this package's typed helpers can set or read these values.
type ctxKey string

const (
	keyOperation   ctxKey = "operation"
	keyTask        ctxKey = "task_id"
	keyTransaction ctxKey = "transaction_id"
	keyProfile     ctxKey = "profile_id"
	keyModUniqueID ctxKey = "mod_unique_id"
	keyProvider    ctxKey = "provider"
)

// contextFields maps context keys to their PRD log field names, in emission order.
var contextFields = []struct {
	key   ctxKey
	field string
}{
	{keyOperation, "operation"},
	{keyTask, "task_id"},
	{keyTransaction, "transaction_id"},
	{keyProfile, "profile_id"},
	{keyModUniqueID, "mod_unique_id"},
	{keyProvider, "provider"},
}

func withField(ctx context.Context, key ctxKey, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, key, id)
}

// ContextWithTask returns ctx carrying the task ID (logged as task_id).
func ContextWithTask(ctx context.Context, taskID string) context.Context {
	return withField(ctx, keyTask, taskID)
}

// WithOperationID returns ctx carrying the operation ID (logged as operation).
// An operation ID set at the top of a call tree appears on every nested
// record logged with a derived context.
func WithOperationID(ctx context.Context, operationID string) context.Context {
	return withField(ctx, keyOperation, operationID)
}

// ContextWithTransaction returns ctx carrying the mod transaction ID.
func ContextWithTransaction(ctx context.Context, transactionID string) context.Context {
	return withField(ctx, keyTransaction, transactionID)
}

// ContextWithProfile returns ctx carrying the profile ID.
func ContextWithProfile(ctx context.Context, profileID string) context.Context {
	return withField(ctx, keyProfile, profileID)
}

// ContextWithModUniqueID returns ctx carrying the mod UniqueID.
func ContextWithModUniqueID(ctx context.Context, modUniqueID string) context.Context {
	return withField(ctx, keyModUniqueID, modUniqueID)
}

// ContextWithProvider returns ctx carrying the provider name.
func ContextWithProvider(ctx context.Context, provider string) context.Context {
	return withField(ctx, keyProvider, provider)
}

// attrsFromContext extracts the correlation attributes carried by ctx.
func attrsFromContext(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	var out []slog.Attr
	for _, f := range contextFields {
		if v, ok := ctx.Value(f.key).(string); ok && v != "" {
			out = append(out, slog.String(f.field, v))
		}
	}
	return out
}

// sensitiveSubstrings are matched case-insensitively against attribute keys;
// a match redacts the value. "auth" intentionally covers "authorization" and
// its variants; over-redaction is the safe direction for secrets.
var sensitiveSubstrings = []string{
	"token",
	"secret",
	"password",
	"passwd",
	"authorization",
	"auth",
	"credential",
}

// sensitiveKey reports whether an attribute key carries secret content.
func sensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveSubstrings {
		if strings.Contains(k, s) {
			return true
		}
	}
	// api[_-]?key, case-insensitive: matches apiKey, api_key, api-key.
	return strings.Contains(k, "apikey") ||
		strings.Contains(k, "api_key") ||
		strings.Contains(k, "api-key")
}

// redactAttr returns the attribute with secret-bearing values replaced.
func redactAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		subs := make([]any, 0, len(v.Group()))
		for _, sub := range v.Group() {
			subs = append(subs, redactAttr(sub))
		}
		return slog.Group(a.Key, subs...)
	}
	if sensitiveKey(a.Key) {
		return slog.String(a.Key, redactedValue)
	}
	return a
}

func redactAttrs(attrs []slog.Attr) []slog.Attr {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = redactAttr(a)
	}
	return out
}

// redactRecord rebuilds r with every attribute passed through redactAttr.
func redactRecord(r slog.Record) slog.Record {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return out
}

// redactHandler wraps a handler so secret-bearing attributes never reach it.
type redactHandler struct {
	next slog.Handler
}

func (h *redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *redactHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.next.Handle(ctx, redactRecord(r))
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &redactHandler{next: h.next.WithAttrs(redactAttrs(attrs))}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{next: h.next.WithGroup(name)}
}

// fanoutHandler delivers each record to both the JSON-lines file handler and
// the human-readable console handler.
type fanoutHandler struct {
	file    slog.Handler
	console slog.Handler
}

func (h *fanoutHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.file.Enabled(ctx, l) || h.console.Enabled(ctx, l)
}

func (h *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	if err := h.file.Handle(ctx, r); err != nil {
		_ = h.console.Handle(ctx, r)
		return err
	}
	return h.console.Handle(ctx, r)
}

func (h *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &fanoutHandler{
		file:    h.file.WithAttrs(attrs),
		console: h.console.WithAttrs(attrs),
	}
}

func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	return &fanoutHandler{
		file:    h.file.WithGroup(name),
		console: h.console.WithGroup(name),
	}
}

// rotatingWriter is an io.WriteCloser that caps disk use: the active file is
// rotated before it would exceed maxBytes, and at most maxFiles files are
// retained (astradew.log plus astradew.1.log .. astradew.(maxFiles-1).log),
// so disk use stays under maxFiles*maxBytes.
type rotatingWriter struct {
	mu       sync.Mutex
	dir      string
	base     string
	maxBytes int64
	maxFiles int
	f        *os.File
	size     int64
}

func newRotatingWriter(dir, base string, maxBytes int64, maxFiles int) (*rotatingWriter, error) {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if maxFiles <= 0 {
		maxFiles = defaultMaxFiles
	}
	w := &rotatingWriter{dir: dir, base: base, maxBytes: maxBytes, maxFiles: maxFiles}
	f, err := os.OpenFile(w.path(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	w.f = f
	if st, err := f.Stat(); err == nil {
		w.size = st.Size()
	}
	return w, nil
}

func (w *rotatingWriter) path() string {
	return filepath.Join(w.dir, w.base)
}

func (w *rotatingWriter) rotated(i int) string {
	ext := filepath.Ext(w.base)
	stem := strings.TrimSuffix(w.base, ext)
	return filepath.Join(w.dir, fmt.Sprintf("%s.%d%s", stem, i, ext))
}

// rotate closes the active file, shifts older files down, and opens a fresh
// active file. Callers must hold w.mu.
func (w *rotatingWriter) rotate() error {
	if err := w.f.Close(); err != nil {
		return err
	}
	if w.maxFiles <= 1 {
		f, err := os.OpenFile(w.path(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		w.f = f
		w.size = 0
		return nil
	}
	_ = os.Remove(w.rotated(w.maxFiles - 1))
	for i := w.maxFiles - 2; i >= 1; i-- {
		if _, err := os.Stat(w.rotated(i)); err == nil {
			_ = os.Rename(w.rotated(i), w.rotated(i+1))
		}
	}
	_ = os.Rename(w.path(), w.rotated(1))
	f, err := os.OpenFile(w.path(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	w.f = f
	w.size = 0
	return nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.maxBytes > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}

// config holds Logger construction options.
type config struct {
	maxBytes int64
	maxFiles int
	console  io.Writer
	level    slog.Level
}

// Option customizes a Logger built by New.
type Option func(*config)

// WithMaxBytes caps a single log file at n bytes before rotation.
func WithMaxBytes(n int64) Option {
	return func(c *config) { c.maxBytes = n }
}

// WithMaxFiles caps the number of retained log files (active plus rotated).
func WithMaxFiles(n int) Option {
	return func(c *config) { c.maxFiles = n }
}

// WithConsole directs the human-readable output to w instead of os.Stderr.
func WithConsole(w io.Writer) Option {
	return func(c *config) { c.console = w }
}

// WithLevel sets the minimum enabled level for both outputs.
func WithLevel(l slog.Level) Option {
	return func(c *config) { c.level = l }
}

// Logger is Astradew's structured application logger.
type Logger struct {
	log *slog.Logger
	out *rotatingWriter
}

// New creates dir if needed, opens astradew.log inside it for JSON-lines
// output, and pairs it with human-readable console output on os.Stderr
// (or the WithConsole writer). The app-data root does not exist yet, so the
// directory is taken as a parameter; callers pass a temp dir in tests.
func New(dir string, opts ...Option) (*Logger, error) {
	cfg := config{
		maxBytes: defaultMaxBytes,
		maxFiles: defaultMaxFiles,
		console:  os.Stderr,
		level:    slog.LevelInfo,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.console == nil {
		cfg.console = io.Discard
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	out, err := newRotatingWriter(dir, logFileName, cfg.maxBytes, cfg.maxFiles)
	if err != nil {
		return nil, err
	}
	fileHandler := &redactHandler{
		next: slog.NewJSONHandler(out, &slog.HandlerOptions{Level: cfg.level}),
	}
	consoleHandler := &redactHandler{
		next: slog.NewTextHandler(cfg.console, &slog.HandlerOptions{Level: cfg.level}),
	}
	return &Logger{
		log: slog.New(&fanoutHandler{file: fileHandler, console: consoleHandler}),
		out: out,
	}, nil
}

// Log emits a record at level, inheriting correlation fields from ctx so code
// that never names them still propagates them.
func (l *Logger) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	if attrs := attrsFromContext(ctx); len(attrs) > 0 {
		all := make([]any, 0, len(attrs)+len(args))
		for _, a := range attrs {
			all = append(all, a)
		}
		l.log.Log(ctx, level, msg, append(all, args...)...)
		return
	}
	l.log.Log(ctx, level, msg, args...)
}

// Debug emits a debug record inheriting ctx correlation fields.
func (l *Logger) Debug(ctx context.Context, msg string, args ...any) {
	l.Log(ctx, slog.LevelDebug, msg, args...)
}

// Info emits an info record inheriting ctx correlation fields.
func (l *Logger) Info(ctx context.Context, msg string, args ...any) {
	l.Log(ctx, slog.LevelInfo, msg, args...)
}

// Warn emits a warn record inheriting ctx correlation fields.
func (l *Logger) Warn(ctx context.Context, msg string, args ...any) {
	l.Log(ctx, slog.LevelWarn, msg, args...)
}

// Error emits an error record inheriting ctx correlation fields.
func (l *Logger) Error(ctx context.Context, msg string, args ...any) {
	l.Log(ctx, slog.LevelError, msg, args...)
}

// WithContext returns a *slog.Logger with ctx's correlation fields attached,
// for code that logs repeatedly under one context.
func (l *Logger) WithContext(ctx context.Context) *slog.Logger {
	attrs := attrsFromContext(ctx)
	anyAttrs := make([]any, 0, len(attrs))
	for _, a := range attrs {
		anyAttrs = append(anyAttrs, a)
	}
	return l.log.With(anyAttrs...)
}

// Slog exposes the underlying *slog.Logger for stdlib interop.
func (l *Logger) Slog() *slog.Logger {
	return l.log
}

// Close flushes and closes the log file.
func (l *Logger) Close() error {
	return l.out.Close()
}
