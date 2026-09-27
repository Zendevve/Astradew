// This file holds the one test that crosses both Phase 3 seams: a synthesized
// archive goes through the real extractor into a real stage, and the staged
// tree is classified. Each seam's own suite pins its own contract; this test
// pins that the two compose — that the names the extractor writes are the names
// the scanner reads, and that the corpus shapes arrive as the units and
// findings the preview promises. The fixtures are the archive package's own, so
// a shape the extractor's tests pin as written is the shape these assertions
// classify.
package inspect

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Zendevve/astradew/internal/archive"
)

// TestInspectOverExtractedCorpus extracts each named corpus shape into a real
// stage and asserts the preview it produces. The option mirrors what the
// service passes on this host; the fixtures spell manifest.json the
// conventional way, so the value is inert here and the expectations hold on
// every platform.
func TestInspectOverExtractedCorpus(t *testing.T) {
	tests := []struct {
		name            string
		fixture         *archive.Fixture
		wantPaths       []string
		wantKinds       []Kind
		wantInstallable bool
	}{
		{
			name:            "single mod",
			fixture:         archive.SingleMod(),
			wantPaths:       []string{"FishZones"},
			wantKinds:       []Kind{KindCodeMod},
			wantInstallable: true,
		},
		{
			name:            "version-suffixed wrapper container",
			fixture:         archive.Wrapper(),
			wantPaths:       []string{"FishZones-12345-0-3-2/FishZones"},
			wantKinds:       []Kind{KindCodeMod},
			wantInstallable: true,
		},
		{
			name:            "multi-mod bundle",
			fixture:         archive.MultiMod(),
			wantPaths:       []string{"Stardew Valley Expanded/StardewValleyExpanded", "Stardew Valley Expanded/[CP] Stardew Valley Expanded", "Stardew Valley Expanded/[FTM] Stardew Valley Expanded"},
			wantKinds:       []Kind{KindCodeMod, KindContentPack, KindContentPack},
			wantInstallable: true,
		},
		{
			name:            "content pack",
			fixture:         archive.ContentPack(),
			wantPaths:       []string{"[CP] FishZones"},
			wantKinds:       []Kind{KindContentPack},
			wantInstallable: true,
		},
		{
			name:            "legacy XNB content",
			fixture:         archive.LegacyXnb(),
			wantPaths:       []string{},
			wantKinds:       []Kind{},
			wantInstallable: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srcPath := filepath.Join(t.TempDir(), "archive.zip")
			if err := test.fixture.WriteTo(srcPath); err != nil {
				t.Fatalf("writing the fixture archive: %v", err)
			}
			stage := archive.StageDir(t.TempDir(), "inspection")
			result, err := archive.Extract(context.Background(), srcPath, stage, archive.DefaultLimits(), nil)
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}

			got := Inspect(os.DirFS(result.StagePath), Options{CaseInsensitivePaths: true})
			if paths := unitPaths(got); !slices.Equal(paths, test.wantPaths) {
				t.Fatalf("unit paths = %+v, want %+v", paths, test.wantPaths)
			}
			kinds := make([]Kind, 0, len(got.Units))
			for _, unit := range got.Units {
				kinds = append(kinds, unit.Kind)
			}
			if !slices.Equal(kinds, test.wantKinds) {
				t.Fatalf("unit kinds = %+v, want %+v", kinds, test.wantKinds)
			}
			if got.Installable != test.wantInstallable {
				t.Fatalf("package installable = %v, want %v (findings %+v)", got.Installable, test.wantInstallable, got.Findings)
			}
		})
	}
}

// TestInspectOverExtractedBundleKeepsTheWrappersDetail pins the detail the
// extraction round trip must not lose: a multi-mod bundle's wrappers stay, its
// content packs keep their host links, and the manifest the extractor wrote is
// the manifest the preview reports.
func TestInspectOverExtractedBundleKeepsTheWrappersDetail(t *testing.T) {
	srcPath := filepath.Join(t.TempDir(), "archive.zip")
	if err := archive.MultiMod().WriteTo(srcPath); err != nil {
		t.Fatalf("writing the fixture archive: %v", err)
	}
	stage := archive.StageDir(t.TempDir(), "inspection")
	result, err := archive.Extract(context.Background(), srcPath, stage, archive.DefaultLimits(), nil)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	got := Inspect(os.DirFS(result.StagePath), Options{})
	if len(got.Findings) != 0 {
		t.Fatalf("findings = %+v, want none for the canonical multi-unit bundle", got.Findings)
	}
	if len(got.Duplicates) != 0 {
		t.Fatalf("duplicates = %+v, want none", got.Duplicates)
	}
	pack := got.Units[1]
	if pack.ContentPackFor != "Pathoschild.ContentPatcher" {
		t.Fatalf("pack host = %q, want the extracted manifest's ContentPackFor", pack.ContentPackFor)
	}
	if pack.EntryDll != "" || pack.Version != "1.0.0" || pack.UniqueID != "FlashShifter.SVE.CP" {
		t.Fatalf("pack = %+v, want the identity and version its manifest declares", pack)
	}
	if pack.SystemMod {
		t.Fatalf("pack.SystemMod = true, want false")
	}
}
