// This file holds the refusal table: one case per way an archive can be turned
// away, each asserting the typed code, the subject its details name, and — for
// every single case — that the refusal left nothing behind. Tests assert the
// code and the named entry, never the message text, because the message is
// copy and the code is the contract.
package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zendevve/astradew/internal/apperror"
)

// The subjects a case can name: an entry's raw name, the source archive path,
// or the stage path, substituted by the runner.
const (
	sourceSubject = "<source>"
	stageSubject  = "<stage>"
)

// PRD section 73 audit. Every required regression case and the case below that
// pins it — all of them cases of TestExtractRefusals unless another test is
// named — so a green run is traceable back to the PRD list:
//
//	../../escape              -> "nested dot-dot segment" (two levels, "/")
//	                             and "dot-dot segment" (one level)
//	..\..\escape              -> "backslash dot-dot traversal"
//	C:\escape                 -> "backslash drive-absolute path"
//	                             ("drive-absolute path" pins the "/" spelling)
//	\\server\share            -> "UNC path"
//	absolute Unix path        -> "absolute path"
//	symlink outside temp root -> "symlink entry", whose target is
//	                             ../../etc/passwd; the refusal is wholesale,
//	                             so no link is created anywhere
//	archive bomb              -> "archive over the ratio limit",
//	                             "ratio limit is not widened by a lying
//	                             compressed size" (the rule reads bytes, not
//	                             the declaration), "archive over the
//	                             expanded-byte limit", and the byte/entry
//	                             limit cases beside them
//	very long path            -> "over-long path" and "over-deep path"
//	reserved Windows filename -> "reserved device name", "reserved device name
//	                             with an extension", "reserved console name"
//	mixed slash traversal     -> "mixed slash traversal"
//	Unicode path confusion    -> "case collision under simple folding" (the
//	                             sigma folding cycle) and "non-UTF-8 name";
//	                             normalization-only collisions are
//	                             deliberately not detected — checkNames leaves
//	                             those to the host — so no refusal case exists
//	                             for them here and none is invented.
//
// The isolation half each traversal case also owes is proved by the runner for
// every case (assertOnlyNew over the parent listing plus the source hash) and
// named for the hostile case by TestExtractRefusalWritesNothingOutsideTheStage.
func TestExtractRefusals(t *testing.T) {
	// archiveOf is the common build: one fixture written to disk.
	archiveOf := func(fixture *Fixture) func(t *testing.T) string {
		return func(t *testing.T) string { return writeFixture(t, fixture) }
	}
	cases := []struct {
		name     string
		build    func(t *testing.T) string
		limits   Limits
		prepare  func(t *testing.T, stage string)
		wantCode apperror.Code
		want     string
		also     string
	}{
		{
			name:     "dot-dot segment",
			build:    archiveOf(NewFixture().File("../escape.txt", "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     "../escape.txt",
		},
		{
			name:     "nested dot-dot segment",
			build:    archiveOf(NewFixture().File("Mod/../../escape.txt", "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     "Mod/../../escape.txt",
		},
		{
			name:     "absolute path",
			build:    archiveOf(NewFixture().File("/etc/passwd", "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     "/etc/passwd",
		},
		{
			name:     "drive-absolute path",
			build:    archiveOf(NewFixture().File("C:/Windows/x.txt", "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     "C:/Windows/x.txt",
		},
		{
			name:     "drive-relative path",
			build:    archiveOf(NewFixture().File("C:x.txt", "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     "C:x.txt",
		},
		{
			name:     "UNC path",
			build:    archiveOf(NewFixture().File(`\\server\share\x.txt`, "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     `\\server\share\x.txt`,
		},
		{
			name:     "device path",
			build:    archiveOf(NewFixture().File(`\\?\C:\x.txt`, "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     `\\?\C:\x.txt`,
		},
		{
			name:     "backslash separator",
			build:    archiveOf(NewFixture().File(`Mod\sub\x.txt`, "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     `Mod\sub\x.txt`,
		},
		{
			// PRD section 73 `..\..\escape`: the Windows spelling of the
			// nested dot-dot escape. Every backslash name is refused, so this
			// pins that the escape is refused in that spelling too, and that
			// the refusal names the entry as written.
			name:     "backslash dot-dot traversal",
			build:    archiveOf(NewFixture().File(`..\..\escape.txt`, "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     `..\..\escape.txt`,
		},
		{
			// PRD section 73 `C:\escape`: the backslash spelling of the drive
			// escape. The backslash rule fires before the drive check, which
			// is the order namePolicy documents; the code and subject a user
			// sees are the same either way.
			name:     "backslash drive-absolute path",
			build:    archiveOf(NewFixture().File(`C:\escape.txt`, "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     `C:\escape.txt`,
		},
		{
			// PRD section 73 "mixed slash traversal": a forward-slash prefix
			// with a backslash dot-dot segment — the shape a tool that
			// normalised one separator but not the other would produce.
			name:     "mixed slash traversal",
			build:    archiveOf(NewFixture().File(`Mod/..\escape.txt`, "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     `Mod/..\escape.txt`,
		},
		{
			name:     "empty component",
			build:    archiveOf(NewFixture().File("Mod//x.txt", "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     "Mod//x.txt",
		},
		{
			name:     "dot component",
			build:    archiveOf(NewFixture().File("Mod/./x.txt", "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     "Mod/./x.txt",
		},
		{
			name:     "empty name",
			build:    archiveOf(NewFixture().File("", "x")),
			wantCode: apperror.CodeArchivePathTraversal,
			want:     `entry ""`,
		},
		{
			name:     "symlink entry",
			build:    archiveOf(NewFixture().Symlink("Mod/link", "../../etc/passwd")),
			wantCode: apperror.CodeArchiveLinkEntry,
			want:     "Mod/link",
		},
		{
			name:     "encrypted entry",
			build:    archiveOf(NewFixture().Encrypted("Mod/secret.txt", "x")),
			wantCode: apperror.CodeArchiveEncrypted,
			want:     "Mod/secret.txt",
		},
		{
			name: "case collision",
			build: archiveOf(NewFixture().
				File("Mod/Readme.txt", "x").
				File("Mod/README.TXT", "y")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/README.TXT",
			also:     "Mod/Readme.txt",
		},
		{
			// Greek capital sigma, final sigma, and small sigma are one folding
			// cycle: strings.ToLower keeps the first two apart, so this case is
			// what proves the policy folds rather than lowercases.
			name: "case collision under simple folding",
			build: archiveOf(NewFixture().
				File("Mod/\u03a3.txt", "x").
				File("Mod/\u03c2.txt", "y")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/\u03c2.txt",
			also:     "Mod/\u03a3.txt",
		},
		{
			name: "exact duplicate",
			build: archiveOf(NewFixture().
				File("Mod/same.txt", "x").
				File("Mod/same.txt", "y")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/same.txt",
		},
		{
			name: "file that is also a directory",
			build: archiveOf(NewFixture().
				File("Mod/thing", "x").
				File("Mod/thing/inner.txt", "y")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/thing",
			also:     "Mod/thing/inner.txt",
		},
		{
			// On a case-insensitive host the two folders are one, so the
			// archive extracts into two different trees depending on the
			// platform, and one of the reported paths would not exist.
			name: "directory spelled two ways",
			build: archiveOf(NewFixture().
				File("Mod/Assets/first.txt", "one").
				File("Mod/assets/second.txt", "two")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/assets/second.txt",
			also:     "Mod/Assets/first.txt",
		},
		{
			name: "file that is a directory in another case",
			build: archiveOf(NewFixture().
				File("Mod/THING/inner.txt", "y").
				File("Mod/thing", "x")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/thing",
			also:     "Mod/THING/inner.txt",
		},
		{
			name:     "reserved device name",
			build:    archiveOf(NewFixture().File("NUL", "x")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "NUL",
		},
		{
			name:     "reserved device name with an extension",
			build:    archiveOf(NewFixture().File("Mod/CON.txt", "x")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/CON.txt",
		},
		{
			name:     "reserved console name",
			build:    archiveOf(NewFixture().File("Mod/CONIN$", "x")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/CONIN$",
		},
		{
			name:     "illegal character",
			build:    archiveOf(NewFixture().File("Mod/na<me.txt", "x")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/na<me.txt",
		},
		{
			name:     "colon in a component",
			build:    archiveOf(NewFixture().File("Mod/na:me.txt", "x")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/na:me.txt",
		},
		{
			name:     "trailing dot",
			build:    archiveOf(NewFixture().File("Mod/name.", "x")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/name.",
		},
		{
			name:     "trailing space",
			build:    archiveOf(NewFixture().File("Mod/name ", "x")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/name ",
		},
		{
			// A component longer than 255 bytes is always longer than the
			// default 240-byte path budget, so the path limit would fire
			// first; the component limit is reached by raising the path
			// budget, which is how a user with long paths would configure it.
			name:     "over-long component",
			build:    archiveOf(NewFixture().File("Mod/"+strings.Repeat("a", 256), "x")),
			limits:   Limits{MaxPathBytes: 4096},
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/" + strings.Repeat("a", 256),
		},
		{
			name:     "NUL byte in a name",
			build:    archiveOf(NewFixture().File("Mod/bad\x00name.txt", "x")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     `Mod/bad\x00name.txt`,
		},
		{
			// A CP437 byte, the shape a legacy zip tool writes: Windows would
			// silently replace it with U+FFFD, macOS would refuse to create
			// the file, and the stage's fs.FS seam could not name it.
			name:     "non-UTF-8 name",
			build:    archiveOf(NewFixture().File("Mod/caf\xe9.txt", "x")),
			wantCode: apperror.CodeArchiveNameInvalid,
			want:     "Mod/caf\xe9.txt",
		},
		{
			name:     "over-long path",
			build:    archiveOf(NewFixture().File("Mod/"+strings.Repeat("a", 130)+"/"+strings.Repeat("b", 130)+".txt", "x")),
			wantCode: apperror.CodeArchiveLimitExceeded,
			want:     "Mod/" + strings.Repeat("a", 130),
		},
		{
			name: "over-deep path",
			build: archiveOf(NewFixture().
				File(strings.Repeat("d/", 33)+"x.txt", "x")),
			wantCode: apperror.CodeArchiveLimitExceeded,
			want:     strings.Repeat("d/", 33) + "x.txt",
		},
		{
			name: "unknown compression method",
			build: func(t *testing.T) string {
				fixture := NewFixture().File("Mod/Mod.dll", "MZ")
				return writeArchive(t, patchEntryMethod(t, fixture.Bytes(), "Mod/Mod.dll", 99))
			},
			wantCode: apperror.CodeArchiveCorrupt,
			want:     "Mod/Mod.dll",
		},
		{
			name: "directory entry carrying data",
			build: func(t *testing.T) string {
				fixture := NewFixture().Dir("Mod/empty")
				return writeArchive(t, patchEntryDeclaredSize(t, fixture.Bytes(), "Mod/empty/", 4))
			},
			wantCode: apperror.CodeArchiveCorrupt,
			want:     "Mod/empty/",
		},
		{
			name: "CRC mismatch",
			build: func(t *testing.T) string {
				fixture := NewFixture().Stored("Mod/Mod.dll", "MZ payload")
				return writeArchive(t, flipStoredByte(t, fixture.Bytes(), "Mod/Mod.dll"))
			},
			wantCode: apperror.CodeArchiveCorrupt,
			want:     "Mod/Mod.dll",
		},
		{
			name: "short read against the declared size",
			build: func(t *testing.T) string {
				fixture := NewFixture().Stored("Mod/Mod.dll", "MZ")
				return writeArchive(t, patchEntryDeclaredSize(t, fixture.Bytes(), "Mod/Mod.dll", 12))
			},
			wantCode: apperror.CodeArchiveCorrupt,
			want:     "Mod/Mod.dll",
		},
		{
			name:     "missing source",
			build:    func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing.zip") },
			wantCode: apperror.CodeArchiveUnreadable,
			want:     "missing.zip",
		},
		{
			name:     "source is a directory",
			build:    func(t *testing.T) string { return t.TempDir() },
			wantCode: apperror.CodeArchiveUnreadable,
			want:     sourceSubject,
		},
		{
			name:     "archive over the byte limit",
			build:    archiveOf(SingleMod()),
			limits:   Limits{MaxArchiveBytes: 64},
			wantCode: apperror.CodeArchiveLimitExceeded,
			want:     sourceSubject,
		},
		{
			name: "archive over the expanded-byte limit",
			build: archiveOf(NewFixture().
				File("Mod/first.txt", strings.Repeat("a", 800)).
				File("Mod/second.txt", strings.Repeat("b", 800))),
			limits:   Limits{MaxExpandedBytes: 1024},
			wantCode: apperror.CodeArchiveLimitExceeded,
			want:     "Mod/second.txt",
		},
		{
			name: "archive over the entry-count limit",
			build: archiveOf(NewFixture().
				File("Mod/a.txt", "x").
				File("Mod/b.txt", "x").
				File("Mod/c.txt", "x")),
			limits:   Limits{MaxEntries: 2},
			wantCode: apperror.CodeArchiveLimitExceeded,
			want:     sourceSubject,
		},
		{
			name:     "archive over the ratio limit",
			build:    archiveOf(NewFixture().File("Mod/bomb.txt", strings.Repeat("a", 4<<20))),
			limits:   Limits{MaxRatio: 2},
			wantCode: apperror.CodeArchiveLimitExceeded,
			want:     "Mod/bomb.txt",
		},
		{
			name: "ratio limit is not widened by a lying compressed size",
			build: func(t *testing.T) string {
				// The same 4 MiB of zeros as the case above, with the
				// central directory claiming the entry occupies 400 MiB of
				// the archive. An allowance computed from that declaration
				// would be 400 MiB of expansion; the ratio is a fact about
				// bytes, so the run must refuse exactly as it does when the
				// declaration is honest.
				data := NewFixture().File("Mod/bomb.txt", strings.Repeat("a", 4<<20)).Bytes()
				return writeArchive(t, patchEntryCompressedSize(t, data, "Mod/bomb.txt", 400<<20))
			},
			wantCode: apperror.CodeArchiveLimitExceeded,
			want:     "Mod/bomb.txt",
		},
		{
			name:     "non-empty stage",
			build:    archiveOf(SingleMod()),
			prepare:  func(t *testing.T, stage string) { writeInto(t, stage, "keep.txt", "mine") },
			wantCode: apperror.CodeInspectionFailed,
			want:     stageSubject,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tempRoot := t.TempDir()
			stage := StageDir(tempRoot, "refused")
			if tc.prepare != nil {
				tc.prepare(t, stage)
			}
			before := listing(t, filepath.Dir(stage))
			srcPath := tc.build(t)
			beforeHash := fileSHA256(t, srcPath)

			_, err := Extract(t.Context(), srcPath, stage, tc.limits, nil)

			subject := tc.want
			switch subject {
			case sourceSubject:
				subject = srcPath
			case stageSubject:
				subject = stage
			}
			refusal := refusalOf(t, err, tc.wantCode, subject)
			if tc.also != "" && !strings.Contains(refusal.Details, tc.also) {
				t.Fatalf("details = %q, want it to name %q as well", refusal.Details, tc.also)
			}
			// Nothing may appear beside the stage, and the source must be
			// exactly the bytes it was before the run.
			assertOnlyNew(t, filepath.Dir(stage), before)
			if beforeHash != "" && fileSHA256(t, srcPath) != beforeHash {
				t.Fatalf("the refused run changed its source archive")
			}
		})
	}
}

// TestExtractRefusalWritesNothingOutsideTheStage is the isolation proof for the
// hostile case: a traversal archive is refused, and the directory the stage
// would have lived in is exactly as empty as it started.
func TestExtractRefusalWritesNothingOutsideTheStage(t *testing.T) {
	srcPath := writeFixture(t, NewFixture().
		File("Mod/manifest.json", "{}").
		File("../escaped.txt", "x"))
	tempRoot := t.TempDir()
	stage := StageDir(tempRoot, "refused")

	_, err := Extract(t.Context(), srcPath, stage, DefaultLimits(), nil)
	refusalOf(t, err, apperror.CodeArchivePathTraversal, "../escaped.txt")

	if got := listing(t, tempRoot); len(got) != 0 {
		t.Fatalf("temp root = %v, want the refused run to leave nothing at all", got)
	}
	if _, statErr := os.Stat(stage); !os.IsNotExist(statErr) {
		t.Fatalf("the refused stage still exists (stat err = %v), want it never created", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(tempRoot, "escaped.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("the traversal target exists (stat err = %v), want nothing written outside the stage", statErr)
	}
}

// TestExtractRefusalsLeaveNoPartialStage covers the other half of the cleanup
// contract: a refusal that happens after the stage exists — an entry whose data
// fails to read — still takes the whole partial tree back out, so a failed
// inspection cannot leave a half-written mod behind.
func TestExtractRefusalsLeaveNoPartialStage(t *testing.T) {
	fixture := NewFixture().
		File("Mod/first.txt", "written before the failure").
		Stored("Mod/second.dll", "MZ payload")
	srcPath := writeArchive(t, flipStoredByte(t, fixture.Bytes(), "Mod/second.dll"))
	tempRoot := t.TempDir()
	stage := StageDir(tempRoot, "refused")

	_, err := Extract(t.Context(), srcPath, stage, DefaultLimits(), nil)
	refusalOf(t, err, apperror.CodeArchiveCorrupt, "Mod/second.dll")

	if _, statErr := os.Stat(stage); !os.IsNotExist(statErr) {
		t.Fatalf("the refused stage still exists (stat err = %v), want the partial tree removed", statErr)
	}
	assertOnlyNew(t, tempRoot, nil, "inspect")
	if got := listing(t, filepath.Dir(stage)); len(got) != 0 {
		t.Fatalf("the inspection area = %v, want the partial stage removed and nothing else", got)
	}
}

// writeInto creates dir and one file inside it — including the file's parent
// directories, which the sweep tests need — for the cases that need a stage
// that is already occupied.
func writeInto(t *testing.T, dir, name, body string) {
	t.Helper()
	target := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("preparing %q: %v", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		t.Fatalf("writing %q: %v", target, err)
	}
}
