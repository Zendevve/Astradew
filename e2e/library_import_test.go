package e2e

// Library import tracers: the phase-3 acceptance proof for issue #61. One
// tracer imports internal/archive's SingleMod fixture through the real Library
// surface and asserts the install preview's DOM; the other imports a traversal
// fixture and asserts the typed refusal copy. Both drive the built application
// the harness in e2e_test.go builds, and both prove the inspection left no
// stage behind under the application's temp root.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zendevve/astradew/internal/approot"
	"github.com/Zendevve/astradew/internal/archive"
)

// Fixed localhost ports keep the run deterministic, as in e2e_test.go: 19311
// to 19314 belong to the older tracers.
const (
	// libraryImportPort serves the preview tracer: one launch, one import,
	// one preview read live and once more after the route round trip.
	libraryImportPort = 19315
	// importRefusalPort serves the traversal-refusal tracer: the same flow
	// with an archive the extraction policy refuses.
	importRefusalPort = 19316
)

// TestProductionAppPreviewsImportedArchive is issue #61's happy-path tracer.
// It writes the SingleMod fixture to a real .zip, launches the built
// application with an isolated data home, attaches over CDP, drives the
// Library the way a player drives it — "Import archive…", then the sheet's
// "Choose .zip archive…", with the native dialog answered by the one stub
// below — and asserts the install preview the Go inspection produced is
// rendered: the fixture's manifest Name and UniqueID as the unit's identity,
// one unit counted, the name of the very file the dialog was answered with,
// and the honest inspect-only note. It then leaves the route and returns, so
// the row-driven reopen path renders the same preview, and finally proves the
// inspection's stage is gone from the temp root.
func TestProductionAppPreviewsImportedArchive(t *testing.T) {
	binary := buildApp(t)
	dataHome := t.TempDir()

	// The fixture is a real file on the real filesystem: the application opens
	// it from the OS, so nothing about the inspection is simulated.
	zipPath := filepath.Join(t.TempDir(), "astradew-e2e-single-mod.zip")
	if err := archive.SingleMod().WriteTo(zipPath); err != nil {
		t.Fatalf("writing the SingleMod fixture to %s: %v", zipPath, err)
	}

	proc := launchApp(t, binary, dataHome, t.TempDir(), libraryImportPort)
	defer killApp(t, proc, libraryImportPort)
	target := waitForPageTarget(t, libraryImportPort)
	assertRenderedText(t, target, "Astradew", "Version 0.0.0")

	if err := navigate(t, target, "#/library"); err != nil {
		t.Fatalf("navigating to #/library: %v", err)
	}
	installDialogStub(t, target, zipPath)
	driveLibraryImport(t, target)

	// The live path: the run's terminal event makes the sheet re-read
	// Inspection and render the preview the Go inspection produced.
	wants := []string{
		// The fixture manifest's Name and UniqueID (SingleMod carries
		// Mushymato.FishZones in both), rendered as the unit's heading and
		// its Unique ID fact.
		"Mushymato.FishZones",
		"Mod units (1)",
		// The file the dialog stub answered with: proof that the path this
		// tracer fed the dialog reached the Go inspection, which read it.
		"astradew-e2e-single-mod.zip",
		// The honest inspect-only note the preview state owns.
		"Nothing is installed yet — inspect only.",
	}
	assertRenderedPageText(t, target, nil, wants...)

	// The task row is the truth (ADR 0006): leaving the route unmounts the
	// sheet, and returning re-reads RecentTasks and reopens the newest
	// archive.inspect row, so the same preview must render from the row alone.
	if err := navigate(t, target, "#/health"); err != nil {
		t.Fatalf("navigating away from #/library: %v", err)
	}
	if err := navigate(t, target, "#/library"); err != nil {
		t.Fatalf("navigating back to #/library: %v", err)
	}
	assertRenderedPageText(t, target, nil, wants...)

	// The extraction staged this archive and the run removed its stage.
	assertNoStageLeft(t, dataHome, true)
}

// TestProductionAppRefusesTraversalArchive is issue #61's refusal tracer: the
// same launched application, the same Library flow, and an archive holding one
// entry that would land outside the inspection's stage. The extraction policy
// runs before any stage exists, so the run fails with the typed
// ARCHIVE_PATH_TRAVERSAL refusal, and the sheet must render that code's copy,
// the code itself, and the offending entry's name from the backend's details —
// and, again, leave no stage behind.
func TestProductionAppRefusesTraversalArchive(t *testing.T) {
	binary := buildApp(t)
	dataHome := t.TempDir()

	zipPath := filepath.Join(t.TempDir(), "astradew-e2e-traversal.zip")
	if err := archive.NewFixture().File("../escape.txt", "x").WriteTo(zipPath); err != nil {
		t.Fatalf("writing the traversal fixture to %s: %v", zipPath, err)
	}

	proc := launchApp(t, binary, dataHome, t.TempDir(), importRefusalPort)
	defer killApp(t, proc, importRefusalPort)
	target := waitForPageTarget(t, importRefusalPort)
	assertRenderedText(t, target, "Astradew", "Version 0.0.0")

	if err := navigate(t, target, "#/library"); err != nil {
		t.Fatalf("navigating to #/library: %v", err)
	}
	installDialogStub(t, target, zipPath)
	driveLibraryImport(t, target)

	// A refusal is one of the states the sheet ends in: the failed run's
	// terminal event makes it re-read Inspection, whose typed failure carries
	// the code, so the sheet renders the code's own copy plus the code text.
	// The entry name comes from the backend's details, which is what proves
	// the refusal is about this archive's entry and not a look-alike.
	assertRenderedPageText(t, target, nil,
		"This archive is malformed or hostile: it holds an entry that would land outside the inspection's temporary folder. Astradew refused the whole archive and wrote nothing. Get a fresh copy from a source you trust.",
		"Refusal code: ARCHIVE_PATH_TRAVERSAL",
		"../escape.txt",
	)

	// The policy pass refused this archive before any stage existed, so the
	// inspection area must never have been created at all.
	assertNoStageLeft(t, dataHome, false)
}

// installDialogStub replaces the page's global fetch with a wrapper that
// answers exactly one runtime call — the native file dialog — and passes every
// other call through to the original fetch untouched.
//
// The .zip picker is the only step of this flow that cannot be automated: it is
// a native OS dialog, so no DOM driver can dismiss it and no synthesised input
// can choose a file in it. Everything else stays real — the inspection itself
// runs in the Go process, the task row is written to the store, progress and
// terminal events travel through the runtime's event transport, and the sheet
// renders them — so this is the one and only stub in the tracers.
//
// On desktop the Wails runtime transports every call as an HTTP POST to
// /wails/runtime (frontend/node_modules/@wailsio/runtime/dist/runtime.js:127)
// carrying {object, method, args}; Dialogs.OpenFile is object 5 (runtime.js:44,
// objectNames.Dialog) and method 4 (dist/dialogs.js:22,62). The wrapper matches
// that pair alone and answers it the way the product's own transport answers a
// string result: HTTP 200 whose body is the raw value and whose Content-Type is
// not JSON, which the runtime resolves through response.text()
// (dist/runtime.js:151-155) — exactly what the Go HTTP transport writes for a
// string result (pkg/application/transport_http.go:348-359). The {ok, text}
// envelope is deliberately not used here: that envelope belongs to the custom
// transport, which is installed only when window.wails.invokeAsync exists, i.e.
// on Android (dist/runtime.js:184-205). Answering with it on desktop would hand
// ImportSheet an object where it checks for a string, and the sheet would treat
// the dialog as cancelled.
func installDialogStub(t *testing.T, debuggerURL, path string) {
	t.Helper()

	expr := fmt.Sprintf(`(() => {
	if (window.__astradewDialogStub) {
		return 'the file dialog stub was already installed';
	}
	const passthrough = window.fetch.bind(window);
	window.__astradewDialogStub = true;
	window.fetch = (input, init) => {
		let call = null;
		try {
			call = init && typeof init.body === 'string' ? JSON.parse(init.body) : null;
		} catch (error) {
			call = null;
		}
		if (call && call.object === 5 && call.method === 4) {
			return Promise.resolve(new Response(%s, { status: 200, headers: { 'Content-Type': 'text/plain' } }));
		}
		return passthrough(input, init);
	};
	return 'ok';
})()`, quoteJS(path))

	result, err := evaluateInPage(t, debuggerURL, expr)
	if err != nil {
		t.Fatalf("installing the file dialog stub: %v", err)
	}
	if result != "ok" {
		t.Fatalf("installing the file dialog stub reported %q; want \"ok\"", result)
	}
}

// driveLibraryImport clicks "Import archive…" and then the sheet's
// "Choose .zip archive…", through the interface a player uses. The action is
// disabled until the section's RecentTasks read lands (the section owns that
// reason text), and the sheet renders its choose action only once React has
// committed the idle body, so both steps poll inside the page and report the
// page text they saw if the deadline passes. The dialog call the second click
// makes is answered by installDialogStub.
func driveLibraryImport(t *testing.T, debuggerURL string) {
	t.Helper()

	expr := `(() => new Promise((resolve) => {
		const deadline = Date.now() + 25000;
		const find = (label) => Array.from(document.querySelectorAll('button')).find((node) => node.textContent === label);
		const diagnose = (reason) => reason + '; page text: ' + document.body.innerText.slice(0, 800);
		let opened = false;
		const attempt = () => {
			if (!opened) {
				const action = find('Import archive…');
				if (action && !action.disabled) {
					opened = true;
					action.click();
				} else if (Date.now() > deadline) {
					resolve(diagnose(action ? 'the import action stayed disabled' : 'the import action never appeared'));
					return;
				}
				setTimeout(attempt, 250);
				return;
			}
			const choose = find('Choose .zip archive…');
			if (choose && !choose.disabled) {
				choose.click();
				resolve('ok');
				return;
			}
			if (Date.now() > deadline) {
				resolve(diagnose(choose ? 'the sheet\'s choose action stayed disabled' : 'the sheet never offered its choose action'));
				return;
			}
			setTimeout(attempt, 250);
		};
		attempt();
	}))()`

	result, err := evaluateInPage(t, debuggerURL, expr)
	if err != nil {
		t.Fatalf("driving the Library import through the interface: %v", err)
	}
	if result != "ok" {
		t.Fatalf("Library import driver reported %q; want \"ok\"", result)
	}
}

// assertNoStageLeft polls until the inspection area under the application's
// temp root holds no entry, and fails naming what it still holds otherwise.
// The area is <XDG_DATA_HOME>/Astradew/temp/inspect: archive.StageDir keeps one
// inspection's stage as a direct child of it, and the run removes that stage on
// every path — success, failure, cancellation, and a panic inside the
// extraction (internal/app/inspect.go's deferred removal) — so an entry left
// here is exactly the leak issue #61 forbids.
//
// staged reports whether this run reached the staging step. Extraction creates
// the area (prepareStage) and removes only the stage inside it, so after a
// staged run the area must still be there and empty — which is also what proves
// this check watches the path the application actually stages under, rather
// than an empty path that would pass vacuously. A refusal that the policy pass
// answers before any stage exists must leave the area absent altogether: it
// refused before writing anything anywhere.
func assertNoStageLeft(t *testing.T, dataHome string, staged bool) {
	t.Helper()

	tempRoot := filepath.Join(dataHome, approot.AppDirName, approot.DirTemp)
	// The area is the parent of any stage path. Taking it from StageDir rather
	// than spelling the level's name here keeps the one owner of that name —
	// it is unexported in internal/archive on purpose — the authority.
	stageArea := filepath.Dir(archive.StageDir(tempRoot, "inspection"))

	deadline := time.Now().Add(cdpTimeout)
	for {
		entries, err := os.ReadDir(stageArea)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if !staged {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("the inspection area %s never appeared, so this tracer is not watching the path the application stages under", stageArea)
			}
		case err != nil:
			if time.Now().After(deadline) {
				t.Fatalf("reading the inspection area %s: %v", stageArea, err)
			}
		case len(entries) > 0:
			if time.Now().After(deadline) {
				names := make([]string, 0, len(entries))
				for _, entry := range entries {
					names = append(names, entry.Name())
				}
				t.Fatalf("the run left inspection stage(s) under %s: %s", stageArea, strings.Join(names, ", "))
			}
		case staged:
			return
		default:
			if time.Now().After(deadline) {
				t.Fatalf("the refused run created the inspection area %s even though it staged nothing", stageArea)
			}
		}
		time.Sleep(pollInterval)
	}
}
