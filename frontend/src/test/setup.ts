import { vi } from "vitest";

/**
 * Test seam: replaces the generated Wails bindings with a stub so routes
 * render without the desktop runtime. The default stub resolves a fixed
 * identity; individual tests override ApplicationService.Info to resolve a
 * different identity or reject on demand.
 */
vi.mock("../../bindings/github.com/Zendevve/astradew/internal/app", () => ({
  ApplicationService: { Info: vi.fn(), ProbeFailure: vi.fn() },
}));

import { ApplicationService } from "../../bindings/github.com/Zendevve/astradew/internal/app";

vi.mocked(ApplicationService.Info).mockResolvedValue({
  name: "Astradew",
  version: "0.0.0-test",
});
