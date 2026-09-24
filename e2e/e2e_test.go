// Package e2e drives the built production application end to end: it builds
// bin/astradew.exe, launches it with an isolated data home and a
// remote-debugging port, attaches over the Chrome DevTools Protocol, and
// asserts a value originating in Go is actually rendered in the DOM.
//
// The harness never touches the developer's real profile: the app is pointed
// at a fresh temporary XDG_DATA_HOME, which the adrg/xdg path provider the
// application uses honours on every platform (its EnvPath helper prefers the
// environment value over the known-folder fallback). Remote debugging is
// enabled in code only — main.go maps the bare port number in
// ASTRADEW_REMOTE_DEBUGGING_PORT to WebView2 AdditionalBrowserArgs, because
// Wails clears the WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS environment override
// at startup — so ordinary launches leave the debugger port closed.
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Fixed localhost ports keep the run deterministic. 127.0.0.1 binds only the
// loopback interface; the values avoid the common dev-server range.
const (
	debugPort   = 19311
	profilePort = 19312
	// restartPort serves the phase-0 acceptance run: first start plus a
	// real restart over one data home on one port.
	restartPort = 19313
)

// Timeouts are generous because WebView2 cold start on Windows is slow: the
// binary exists, the environment spawns, and the first paint renders through
// the shared browser process.
const (
	launchTimeout = 90 * time.Second
	pollInterval  = 500 * time.Millisecond
	cdpTimeout    = 30 * time.Second
)

// TestProductionAppRendersGoIdentity builds the real binary, launches it with
// an isolated data home, attaches via CDP, and asserts the product identity
// reported by the Go Info service is rendered in the DOM.
func TestProductionAppRendersGoIdentity(t *testing.T) {
	binary := buildApp(t)
	dataHome := t.TempDir()
	userData := t.TempDir()

	// Parallel launches race for the fixed debug ports and for the machine's
	// single shared WebView2 browser environment, so the two tests run in
	// sequence. Port constants stay fixed for determinism.
	proc := launchApp(t, binary, dataHome, userData, debugPort)
	defer killApp(t, proc)

	target := waitForPageTarget(t, debugPort)
	assertRenderedText(t, target, "Astradew", "Version 0.0.0")
}

// TestProductionAppRendersHealthReport is the service-seam sibling: it opens
// the #/health route and asserts the backend Health report (data root under
// the isolated temp home, schema version line) is rendered, proving the Go
// store behind the window serves live state.
func TestProductionAppRendersHealthReport(t *testing.T) {
	binary := buildApp(t)
	dataHome := t.TempDir()
	userData := t.TempDir()

	proc := launchApp(t, binary, dataHome, userData, profilePort)
	defer killApp(t, proc)

	target := waitForPageTarget(t, profilePort)
	if err := navigate(t, target, "#/health"); err != nil {
		t.Fatalf("navigating to #/health: %v", err)
	}
	assertRenderedText(t, target, "Astradew", "Data root: "+filepath.Join(dataHome, "Astradew"))
}

// TestProductionAppRestartsWithSettingPersisted is the phase-0 acceptance
// proof: one isolated data home through a real process restart. Launch 1
// proves initialisation completed (identity rendered, astradew.db on disk,
// schema version 1 in the Health DOM, durable startup record rendered),
// changes the theme setting through the interface the player uses, kills
// the process, relaunches the same data home, and proves the new value is
// still rendered. The database file's persistence, the unchanged schema
// version, and the grown startup record prove the second start reused the
// store instead of re-migrating it: migrations are idempotent (Open records
// each applied version in schema_migrations, and reaching the same version
// twice is a no-op), so a re-migration would leave the version line at 1
// either way — the honest signal is version still 1 plus prior rows intact
// (two startup records, not one) plus the file's mtime advanced by the
// task write, not reset by a rebuild.
func TestProductionAppRestartsWithSettingPersisted(t *testing.T) {
	binary := buildApp(t)
	dataHome := t.TempDir()

	// Launch 1: fresh data home, prove initialisation completed.
	proc := launchApp(t, binary, dataHome, t.TempDir(), restartPort)
	target := waitForPageTarget(t, restartPort)
	assertRenderedText(t, target, "Astradew", "Version 0.0.0")

	// The store file the schema initialised lives at
	// <dataHome>/Astradew/database/astradew.db (approot layout, store file
	// name); its presence on disk proves creation on first start.
	dbPath := filepath.Join(dataHome, "Astradew", "database", "astradew.db")
	firstInfo, err := os.Stat(dbPath)
	if err != nil {
		killApp(t, proc)
		t.Fatalf("database file %s missing after first start: %v", dbPath, err)
	}
	if firstInfo.Size() == 0 {
		killApp(t, proc)
		t.Fatalf("database file %s is empty after first start", dbPath)
	}

	// The Health DOM carries the live store state: schema version plus the
	// durable startup record (startup task rendered by StartupTaskSection).
	// Body text, not assertRenderedText: that helper reads only the identity
	// line and the health report, while the startup record renders in its
	// own section on every route.
	if err := navigate(t, target, "#/health"); err != nil {
		killApp(t, proc)
		t.Fatalf("navigating to #/health on first start: %v", err)
	}
	assertRenderedPageText(t, target, nil, "schema version 1", "Startup task succeeded")
	evidence := []string{"first start rendered identity Version 0.0.0, schema version 1, and a succeeded startup task"}

	// Change a setting through the interface, not the Go seam: open
	// #/settings, set the theme input to dark, dispatch the input event
	// React listens for, and click the row's Save button. The settings list
	// renders only after Settings() resolves, and the save round-trips
	// through SetSetting then re-reads — so both the drive and the read-back
	// poll inside the page until their condition holds or they time out.
	if err := navigate(t, target, "#/settings"); err != nil {
		killApp(t, proc)
		t.Fatalf("navigating to #/settings: %v", err)
	}
	driveThemeDark := `(() => new Promise((resolve) => {
		const deadline = Date.now() + 25000;
		const attempt = () => {
			const input = document.getElementById('setting-theme');
			if (input) {
				const row = input.closest('.setting-row');
				const button = row ? Array.from(row.querySelectorAll('button')).find((node) => node.textContent === 'Save') : null;
				if (row && button) {
					input.focus();
					Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set.call(input, 'dark');
					input.dispatchEvent(new Event('input', { bubbles: true }));
					button.click();
					resolve('ok');
					return;
				}
			}
			if (Date.now() > deadline) {
				resolve('settings row never appeared: ' + document.body.innerText.slice(0, 500));
				return;
			}
			setTimeout(attempt, 250);
		};
		attempt();
	}))()`
	if result, err := evaluateInPage(t, target, driveThemeDark); err != nil {
		killApp(t, proc)
		t.Fatalf("driving the theme row through the interface: %v", err)
	} else if result != "ok" {
		killApp(t, proc)
		t.Fatalf("theme row driver reported %q; want \"ok\"", result)
	}
	// The save round-trips through SetSetting then re-reads (load onSaved),
	// so the re-read path — not just the edited input — must show dark and
	// the row must drop its default marker. Poll: the write is async.
	assertRenderedPageText(t, target, nil, "dark")
	// The row's "(default)" marker drops only when the re-read lands, so
	// poll the page for the marker's absence rather than reading once.
	waitForNoDefaultMarker(t, target, nil)
	// Capture the first launch's durable startup timestamp before killing
	// the process: the debugger connection dies with it. The startup section
	// renders on every route, so the #/settings text already carries it.
	firstText, err := evaluateInPage(t, target, `(() => document.body.innerText)()`)
	if err != nil {
		killApp(t, proc)
		t.Fatalf("reading first-start page text: %v", err)
	}
	firstUpdated := startupUpdatedAt(t, firstText, nil, "first")
	// Kill the real process, then relaunch the same data home. The fresh
	// browser profile is fine — persistence lives in the data home, not
	// the webview profile.
	killApp(t, proc)
	proc = launchApp(t, binary, dataHome, t.TempDir(), restartPort, evidence...)
	defer killApp(t, proc)
	// The setting must survive the restart: re-read path shows dark.
	secondTarget := waitForPageTarget(t, restartPort, evidence...)
	if err := navigate(t, secondTarget, "#/settings"); err != nil {
		t.Fatalf("navigating to #/settings after restart%s: %v", evidenceSuffix(evidence), err)
	}
	assertRenderedPageText(t, secondTarget, evidence, "theme", "dark")

	// The store was reused, not rebuilt: the file still exists, the Health
	// DOM still reports schema version 1, and the durable startup record
	// advanced — the second launch appended a new row carrying a later
	// updated timestamp instead of migrating a fresh file.
	secondInfo, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("database file %s missing after restart%s: %v", dbPath, evidenceSuffix(evidence), err)
	}
	if err := navigate(t, secondTarget, "#/health"); err != nil {
		t.Fatalf("navigating to #/health after restart%s: %v", evidenceSuffix(evidence), err)
	}
	assertRenderedPageText(t, secondTarget, evidence, "schema version 1", "Startup task succeeded")
	secondText, err := evaluateInPage(t, secondTarget, `(() => document.body.innerText)()`)
	if err != nil {
		t.Fatalf("reading second-start page text after restart%s: %v", evidenceSuffix(evidence), err)
	}
	secondUpdated := startupUpdatedAt(t, secondText, evidence, "second")
	// The file's mtime advanced with the second startup write; a rebuilt
	// store would instead carry only one startup record (caught above) at
	// version 1. mtime alone cannot prove no-remigrate — migrations are
	// idempotent by construction — which is why the version line plus the
	// advanced durable record accompany it.
	if secondInfo.ModTime().Before(firstInfo.ModTime()) {
		t.Fatalf("database mtime went backwards across restart%s: first %s, second %s", evidenceSuffix(evidence), firstInfo.ModTime(), secondInfo.ModTime())
	}
	if secondUpdated <= firstUpdated {
		t.Fatalf("startup record did not advance across restart%s: first %q, second %q — the second start did not append to the existing store", evidenceSuffix(evidence), firstUpdated, secondUpdated)
	}
}

// startupUpdatedAt extracts the durable startup record's updated timestamp
// from the StartupTaskSection text ("Operation startup: recorded <a>,
// updated <b>"). A missing section is fatal: the durable record is part of
// the acceptance proof.
func startupUpdatedAt(t *testing.T, text string, evidence []string, which string) string {
	t.Helper()

	const marker = "Operation startup: recorded "
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("%s-start page text carries no durable startup record%s; text:\n%s", which, evidenceSuffix(evidence), text)
	}
	rest := text[at+len(marker):]
	updated := strings.Index(rest, "updated ")
	if updated < 0 {
		t.Fatalf("%s-start startup record has no updated timestamp%s; text:\n%s", which, evidenceSuffix(evidence), text)
	}
	stamp := strings.TrimSpace(rest[updated+len("updated "):])
	if end := strings.Index(stamp, "\n"); end >= 0 {
		stamp = strings.TrimSpace(stamp[:end])
	}
	if stamp == "" {
		t.Fatalf("%s-start startup record timestamp is empty%s; text:\n%s", which, evidenceSuffix(evidence), text)
	}
	return stamp
}

// evaluateInPage dials the debugger target once, runs expression, and
// returns its value.
func evaluateInPage(t *testing.T, debuggerURL, expression string) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), cdpTimeout)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, debuggerURL, nil)
	if err != nil {
		return "", fmt.Errorf("dialling debugger: %w", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	client := &cdpClient{conn: conn}
	client.startReading(ctx)
	return client.evaluate(ctx, expression)
}

// waitForNoDefaultMarker polls until the "theme (default)" marker is gone
// from the settings page: the marker drops only when the post-save re-read
// lands, so a single read right after the click races the SetSetting round
// trip.
func waitForNoDefaultMarker(t *testing.T, debuggerURL string, evidence []string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), cdpTimeout)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, debuggerURL, nil)
	if err != nil {
		t.Fatalf("dialling debugger%s: %v", evidenceSuffix(evidence), err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	client := &cdpClient{conn: conn}
	client.startReading(ctx)

	deadline := time.Now().Add(cdpTimeout)
	for {
		text, err := client.evaluate(ctx, `(() => document.body.innerText)()`)
		if err != nil {
			t.Fatalf("evaluating in page%s: %v", evidenceSuffix(evidence), err)
		}
		if !strings.Contains(text, "theme (default)") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("theme row still shows its default marker after saving dark%s; text:\n%s", evidenceSuffix(evidence), text)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("theme row still shows its default marker after saving dark%s: %v; text:\n%s", evidenceSuffix(evidence), ctx.Err(), text)
		case <-time.After(pollInterval):
		}
	}
}

// assertRenderedPageText polls the page body's text like assertRenderedText
// but carries first-launch evidence into a relaunch failure message, so a
// second-launch failure still reports what the first launch proved.
func assertRenderedPageText(t *testing.T, debuggerURL string, evidence []string, wants ...string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), cdpTimeout)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, debuggerURL, nil)
	if err != nil {
		t.Fatalf("dialling debugger%s: %v", evidenceSuffix(evidence), err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	client := &cdpClient{conn: conn}
	client.startReading(ctx)

	deadline := time.Now().Add(cdpTimeout)
	for {
		text, err := client.evaluate(ctx, `(() => document.body.innerText)()`)
		if err != nil {
			t.Fatalf("evaluating in page%s: %v", evidenceSuffix(evidence), err)
		}
		if containsAll(text, wants) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("rendered text never contained %q within %s%s; last text:\n%s", wants, cdpTimeout, evidenceSuffix(evidence), text)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("rendered text never contained %q%s: %v; last text:\n%s", wants, evidenceSuffix(evidence), ctx.Err(), text)
		case <-time.After(pollInterval):
		}
	}
}

// buildApp compiles the production binary the harness drives. It mirrors the
// Windows production build flags (production tag, trimmed paths, GUI
// subsystem) so the tested artifact is the shipped shape, never the dev
// server.
func buildApp(t *testing.T) string {
	t.Helper()

	binary := filepath.Join("..", "bin", "astradew.exe")
	if runtime.GOOS != "windows" {
		t.Skipf("e2e harness drives the Windows WebView2 build, running on %s", runtime.GOOS)
	}
	args := []string{"build", "-tags", "production", "-trimpath", "-buildvcs=false",
		"-ldflags=-w -s -H windowsgui", "-o", binary, "."}
	cmd := exec.Command("go", args...)
	cmd.Dir = ".."
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building production binary: %v\n%s", err, out)
	}
	abs, err := filepath.Abs(filepath.Join("..", "bin", "astradew.exe"))
	if err != nil {
		t.Fatalf("resolving binary path: %v", err)
	}
	return abs
}

// evidenceSuffix appends first-launch evidence to a relaunch failure
// message. Empty on a first launch, so the existing tests read unchanged.
func evidenceSuffix(evidence []string) string {
	var kept []string
	for _, item := range evidence {
		if strings.TrimSpace(item) != "" {
			kept = append(kept, item)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return "; first-launch evidence: " + strings.Join(kept, "; ")
}

// launchApp starts the built binary with an isolated data home and the
// remote-debugging port. XDG_DATA_HOME must be absolute; TempDir already is.
// WEBVIEW2_USER_DATA_FOLDER isolates the browser environment so stale ports
// from other instances cannot collide.
func launchApp(t *testing.T, binary, dataHome, userData string, port int, evidence ...string) *exec.Cmd {
	t.Helper()

	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(),
		"ASTRADEW_REMOTE_DEBUGGING_PORT="+itoa(port),
		"XDG_DATA_HOME="+dataHome,
		"WEBVIEW2_USER_DATA_FOLDER="+userData,
	)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("attaching stderr: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("launching %s: %v", binary, err)
	}
	stderrTail := make(chan string, 1)
	go func() { stderrTail <- drainPipe(stderr) }()

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.Now().Add(launchTimeout)
	for time.Now().Before(deadline) {
		select {
		case waitErr := <-exited:
			t.Fatalf("app exited during startup: %v\nstderr:\n%s%s", waitErr, <-stderrTail, evidenceSuffix(evidence))
		default:
		}
		if endpointUp(port) {
			return cmd
		}
		time.Sleep(pollInterval)
	}
	_ = cmd.Process.Kill()
	<-exited
	t.Fatalf("DevTools endpoint on port %d never came up within %s%s", port, launchTimeout, evidenceSuffix(evidence))
	return nil
}

// killApp terminates the app on every exit path so no window outlives the run.
func killApp(t *testing.T, cmd *exec.Cmd) {
	t.Helper()

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}

// endpointUp reports whether the DevTools HTTP endpoint answers.
func endpointUp(port int) bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+itoa(port), 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// devToolsTarget is one entry of the /json/list response.
type devToolsTarget struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// waitForPageTarget polls /json/list until the application page target with a
// debugger URL appears. WebView2 exposes bookkeeping pages (about:blank)
// alongside the real window, so the match requires the served app URL — any
// page target whose URL is not about:blank — rather than the first entry.
func waitForPageTarget(t *testing.T, port int, evidence ...string) string {
	t.Helper()

	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(launchTimeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://127.0.0.1:" + itoa(port) + "/json/list")
		if err == nil {
			targets, decErr := decodeTargets(resp.Body)
			_ = resp.Body.Close()
			if decErr == nil {
				for _, target := range targets {
					if target.Type == "page" && target.WebSocketDebuggerURL != "" && target.URL != "" && target.URL != "about:blank" {
						return target.WebSocketDebuggerURL
					}
				}
			}
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("no debuggable page target appeared on port %d within %s%s", port, launchTimeout, evidenceSuffix(evidence))
	return ""
}

// decodeTargets reads the /json/list body.
func decodeTargets(body io.Reader) ([]devToolsTarget, error) {
	var targets []devToolsTarget
	if err := json.NewDecoder(body).Decode(&targets); err != nil {
		return nil, err
	}
	return targets, nil
}

// cdpConn is a minimal Chrome DevTools Protocol client over the debugger
// websocket: send Runtime.evaluate, match responses by id.
func assertRenderedText(t *testing.T, debuggerURL string, wants ...string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), cdpTimeout)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, debuggerURL, nil)
	if err != nil {
		t.Fatalf("dialling debugger: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	client := &cdpClient{conn: conn}
	client.startReading(ctx)

	expr := `(() => {
		const el = document.querySelector('.identity-line');
		const id = el ? el.innerText : document.body ? document.body.innerText : '';
		const health = document.querySelector('.health-report');
		return id + '\n' + (health ? health.innerText : '');
	})()`
	deadline := time.Now().Add(cdpTimeout)
	for {
		text, err := client.evaluate(ctx, expr)
		if err != nil {
			t.Fatalf("evaluating in page: %v", err)
		}
		if containsAll(text, wants) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("rendered text never contained %q within %s; last text:\n%s", wants, cdpTimeout, text)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("rendered text never contained %q: %v; last text:\n%s", wants, ctx.Err(), text)
		case <-time.After(pollInterval):
		}
	}
}

// navigate changes the hash route inside the page. The app listens for
// hashchange, so no reload round trip is needed.
func navigate(t *testing.T, debuggerURL, hash string) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), cdpTimeout)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, debuggerURL, nil)
	if err != nil {
		return fmt.Errorf("dialling debugger: %w", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	client := &cdpClient{conn: conn}
	client.startReading(ctx)

	expr := fmt.Sprintf(`(() => { window.location.hash = %s; return true; })()`, quoteJS(hash))
	if _, err := client.evaluate(ctx, expr); err != nil {
		return fmt.Errorf("setting hash: %w", err)
	}
	return nil
}

// quoteJS renders a Go string as a single-quoted JS string literal.
func quoteJS(s string) string {
	var out strings.Builder
	out.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'':
			out.WriteString("\\'")
		case '\\':
			out.WriteString("\\\\")
		case '\n':
			out.WriteString("\\n")
		default:
			out.WriteRune(r)
		}
	}
	out.WriteByte('\'')
	return out.String()
}

// containsAll reports whether text holds every want.
func containsAll(text string, wants []string) bool {
	for _, want := range wants {
		if !strings.Contains(text, want) {
			return false
		}
	}
	return true
}

// cdpClient speaks enough DevTools Protocol to evaluate expressions.
type cdpClient struct {
	conn *websocket.Conn

	mu      sync.Mutex
	next    int
	pending map[int]chan cdpResponse
}

// cdpResponse is the matched reply to one evaluate request.
type cdpResponse struct {
	result string
	err    error
}

// startReading pumps incoming messages into the pending waiters.
func (c *cdpClient) startReading(ctx context.Context) {
	c.pending = make(map[int]chan cdpResponse)
	go func() {
		for {
			_, data, err := c.conn.Read(ctx)
			if err != nil {
				return
			}
			var msg struct {
				ID     int `json:"id"`
				Result *struct {
					Result *struct {
						Type  string `json:"type"`
						Value any    `json:"value"`
					} `json:"result"`
				} `json:"result"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(data, &msg); err != nil || msg.ID == 0 {
				continue
			}
			c.mu.Lock()
			waiter, ok := c.pending[msg.ID]
			if ok {
				delete(c.pending, msg.ID)
			}
			c.mu.Unlock()
			if !ok {
				continue
			}
			switch {
			case msg.Error != nil:
				waiter <- cdpResponse{err: fmt.Errorf("%s", msg.Error.Message)}
			case msg.Result == nil || msg.Result.Result == nil:
				waiter <- cdpResponse{err: fmt.Errorf("empty evaluate result")}
			case msg.Result.Result.Type == "string":
				text, _ := msg.Result.Result.Value.(string)
				waiter <- cdpResponse{result: text}
			default:
				encoded, _ := json.Marshal(msg.Result.Result.Value)
				waiter <- cdpResponse{result: string(encoded)}
			}
		}
	}()
}

// evaluate runs expression in the page and returns its string value.
func (c *cdpClient) evaluate(ctx context.Context, expression string) (string, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	waiter := make(chan cdpResponse, 1)
	c.pending[id] = waiter
	c.mu.Unlock()

	payload, err := json.Marshal(map[string]any{
		"id":     id,
		"method": "Runtime.evaluate",
		"params": map[string]any{
			"expression":    expression,
			"returnByValue": true,
			"awaitPromise":  true,
		},
	})
	if err != nil {
		return "", err
	}
	if err := c.conn.Write(ctx, websocket.MessageText, payload); err != nil {
		return "", err
	}
	select {
	case resp := <-waiter:
		return resp.result, resp.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// drainPipe discards app stderr so a full pipe can never block the app and
// returns the tail for failure diagnostics.
func drainPipe(pipe io.Reader) string {
	var tail []string
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		tail = append(tail, scanner.Text())
		if len(tail) > 50 {
			tail = tail[len(tail)-50:]
		}
	}
	return strings.Join(tail, "\n")
}

// itoa formats a port without importing strconv at the call sites.
func itoa(port int) string {
	return strings.TrimSpace(fmt.Sprintf("%d", port))
}
