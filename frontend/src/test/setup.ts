import { vi } from "vitest";

/**
 * Test seam: replaces the generated Wails bindings with a stub so routes
 * render without the desktop runtime. The default stub resolves a fixed
 * identity; individual tests override ApplicationService.Info to resolve a
 * different identity or reject on demand.
 */
vi.mock("../../bindings/github.com/Zendevve/astradew/internal/app", () => ({
  ApplicationService: {
    Info: vi.fn(),
    ProbeFailure: vi.fn(),
    Paths: vi.fn(),
    Health: vi.fn(),
    NoteLoggerCreated: vi.fn(),
  },
}));

import { ApplicationService } from "../../bindings/github.com/Zendevve/astradew/internal/app";

vi.mocked(ApplicationService.Info).mockResolvedValue({
  name: "Astradew",
  version: "0.0.0-test",
});
vi.mocked(ApplicationService.Paths).mockResolvedValue({
  root: "C:\\Users\\test\\AppData\\Local\\Astradew",
  database: "C:\\Users\\test\\AppData\\Local\\Astradew\\database",
  library: "C:\\Users\\test\\AppData\\Local\\Astradew\\library",
  profiles: "C:\\Users\\test\\AppData\\Local\\Astradew\\profiles",
  backups: "C:\\Users\\test\\AppData\\Local\\Astradew\\backups",
  cache: "C:\\Users\\test\\AppData\\Local\\Astradew\\cache",
  logs: "C:\\Users\\test\\AppData\\Local\\Astradew\\logs",
  temp: "C:\\Users\\test\\AppData\\Local\\Astradew\\temp",
});
vi.mocked(ApplicationService.Health).mockResolvedValue({
  name: "Astradew",
  version: "0.0.0-test",
  dataRoot: "C:\\Users\\test\\AppData\\Local\\Astradew",
  directories: [
    { name: "Root", path: "C:\\Users\\test\\AppData\\Local\\Astradew", writable: true },
    { name: "Database", path: "C:\\Users\\test\\AppData\\Local\\Astradew\\database", writable: true },
    { name: "Library", path: "C:\\Users\\test\\AppData\\Local\\Astradew\\library", writable: true },
    { name: "Profiles", path: "C:\\Users\\test\\AppData\\Local\\Astradew\\profiles", writable: true },
    { name: "Backups", path: "C:\\Users\\test\\AppData\\Local\\Astradew\\backups", writable: true },
    { name: "Cache", path: "C:\\Users\\test\\AppData\\Local\\Astradew\\cache", writable: true },
    { name: "Logs", path: "C:\\Users\\test\\AppData\\Local\\Astradew\\logs", writable: true },
    { name: "Temporary files", path: "C:\\Users\\test\\AppData\\Local\\Astradew\\temp", writable: true },
  ],
  database: {
    path: "C:\\Users\\test\\AppData\\Local\\Astradew\\database\\astradew.db",
    version: 1,
    healthy: true,
    state: "open",
  },
  initialisation: [
    { name: "Resolve application data root", ok: true, message: "resolved" },
    { name: "Open database", ok: true, message: "opened at schema version 1" },
  ],
  findings: [],
  unavailable: [
    { name: "Game detection", status: "unavailable", reason: "not yet implemented in this phase" },
    { name: "SMAPI detection", status: "unavailable", reason: "not yet implemented in this phase" },
    { name: "Mod health", status: "unavailable", reason: "not yet implemented in this phase" },
  ],
});
