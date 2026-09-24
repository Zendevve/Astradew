import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import App from "../App";
import { ApplicationService, type Info } from "../../bindings/github.com/Zendevve/astradew/internal/app";
import { ROUTES } from "../pages/routes";

// Guarantees the bindings stub even if this file runs without the setup
// file's global mock; same factory, so behaviour is identical.
vi.mock("../../bindings/github.com/Zendevve/astradew/internal/app", () => ({
  ApplicationService: {
    Info: vi.fn(),
    ProbeFailure: vi.fn(),
    Paths: vi.fn(),
    Health: vi.fn(),
    NoteLoggerCreated: vi.fn(),
    Task: vi.fn(),
    RecentTasks: vi.fn(),
    Settings: vi.fn(),
    GetSetting: vi.fn(),
    SetSetting: vi.fn(),
    GameInstalls: vi.fn(),
    AddGameInstall: vi.fn(),
    SetPrimaryGameInstall: vi.fn(),
  },
}));

const infoMock = vi.mocked(ApplicationService.Info);
const probeMock = vi.mocked(ApplicationService.ProbeFailure);
const pathsMock = vi.mocked(ApplicationService.Paths);
const healthMock = vi.mocked(ApplicationService.Health);
const recentTasksMock = vi.mocked(ApplicationService.RecentTasks);
const settingsMock = vi.mocked(ApplicationService.Settings);
const setSettingMock = vi.mocked(ApplicationService.SetSetting);
const installsMock = vi.mocked(ApplicationService.GameInstalls);
const addInstallMock = vi.mocked(ApplicationService.AddGameInstall);
const setPrimaryMock = vi.mocked(ApplicationService.SetPrimaryGameInstall);
const dataRoot = "C:\\Users\\test\\AppData\\Local\\Astradew";
const healthyReport = {
  name: "Astradew",
  version: "0.0.0",
  dataRoot,
  directories: [
    { name: "Root", path: dataRoot, writable: true },
    { name: "Database", path: `${dataRoot}\\database`, writable: true },
    { name: "Library", path: `${dataRoot}\\library`, writable: true },
    { name: "Profiles", path: `${dataRoot}\\profiles`, writable: true },
    { name: "Backups", path: `${dataRoot}\\backups`, writable: true },
    { name: "Cache", path: `${dataRoot}\\cache`, writable: true },
    { name: "Logs", path: `${dataRoot}\\logs`, writable: true },
    { name: "Temporary files", path: `${dataRoot}\\temp`, writable: true },
  ],
  database: {
    path: `${dataRoot}\\database\\astradew.db`,
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
};
const defaultSettings = [
  { name: "theme", kind: "string", value: "system", isDefault: true, description: "Colour scheme preference: system, light, or dark." },
  { name: "ui-scale", kind: "int", value: 100, isDefault: true, description: "Interface scale in percent, from 50 to 200." },
  { name: "check-updates-on-start", kind: "bool", value: true, isDefault: true, description: "Check for mod updates when Astradew starts." },
];
const dataPaths = {
  root: dataRoot,
  database: `${dataRoot}\\database`,
  library: `${dataRoot}\\library`,
  profiles: `${dataRoot}\\profiles`,
  backups: `${dataRoot}\\backups`,
  cache: `${dataRoot}\\cache`,
  logs: `${dataRoot}\\logs`,
  temp: `${dataRoot}\\temp`,
};
// resetAllMocks wipes stub implementations, and App calls Paths on every
// mount while the Health view calls Health and the Settings view calls
// Settings, so each test starts with resolving stubs; individual tests
// override them to reject on demand.
beforeEach(() => {
  pathsMock.mockResolvedValue(dataPaths);
  healthMock.mockResolvedValue(healthyReport);
  recentTasksMock.mockResolvedValue([]);
  settingsMock.mockResolvedValue(defaultSettings);
  setSettingMock.mockResolvedValue(undefined);
  installsMock.mockResolvedValue([]);
  addInstallMock.mockResolvedValue({
    id: 1,
    path: "C:\\Games\\Stardew Valley",
    source: "manual",
    smapiExePath: null,
    gameVersion: null,
    smapiVersion: null,
    smapiState: "absent",
    isPrimary: true,
  });
  setPrimaryMock.mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  window.location.hash = "";
});

describe("application shell", () => {
  it("every route renders its honest empty state", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });

    for (const route of ROUTES) {
      window.location.hash = `#${route.path}`;
      const view = render(<App />);
      const heading = await view.findByRole("heading", {
        name: route.heading,
      });
      expect(heading.textContent).toBe(route.heading);
      expect(view.getByText(route.empty)).not.toBeNull();
      view.unmount();
    }
    expect(ROUTES).toHaveLength(8);
  });

  it("primary nav reaches every route by keyboard", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    window.location.hash = "#/";
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const nav = screen.getByRole("navigation", { name: "Primary" });
    const links = within(nav).getAllByRole("link");
    expect(links.map((link) => link.textContent)).toEqual(
      ROUTES.map((route) => route.label),
    );

    // Keyboard-alone reachability: Tab from the body lands on each route
    // link in order, each a real focusable anchor with the right target.
    await user.tab(); // skip link
    for (const route of ROUTES) {
      await user.tab();
      expect(document.activeElement?.tagName).toBe("A");
      expect(document.activeElement?.textContent).toBe(route.label);
      expect(document.activeElement?.getAttribute("href")).toBe(
        `#${route.path}`,
      );
    }

    // Following each link's target renders its page and marks it current.
    for (const route of ROUTES) {
      window.location.hash = `#${route.path}`;
      window.dispatchEvent(new Event("hashchange"));
      const heading = await screen.findByRole("heading", {
        name: route.heading,
      });
      expect(heading.textContent).toBe(route.heading);
      expect(
        within(nav)
          .getByRole("link", { name: route.label })
          .getAttribute("aria-current"),
      ).toBe("page");
    }
  });

  it("non-functional top-bar controls are disabled with their reasons", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    render(<App />);
    await screen.findByText("Astradew");

    const launch = screen.getByRole("button", {
      name: "Launch",
    }) as HTMLButtonElement;
    expect(launch.disabled).toBe(true);
    expect(launch.getAttribute("aria-describedby")).toBe("launch-reason");
    const launchReason = document.getElementById("launch-reason");
    expect(launchReason?.textContent).toContain("no game is configured");

    expect(
      (screen.getByLabelText("Profile") as HTMLSelectElement).disabled,
    ).toBe(true);
    expect(screen.getByText(/profiles are unavailable/i)).not.toBeNull();
    expect(
      (screen.getByLabelText("Search") as HTMLInputElement).disabled,
    ).toBe(true);
    expect(screen.getByText(/search is unavailable/i)).not.toBeNull();
  });

  it("identity says it is reading, then shows the backend name and version", async () => {
    // Executor form: the tsconfig lib is ES2020, which has no
    // Promise.withResolvers types. The stub resolves a native promise
    // where the bindings type CancellablePromise; cast through never so
    // the test seam never imports the wails runtime.
    let resolveIdentity!: (info: Info) => void;
    const deferred = new Promise<Info>((resolve) => {
      resolveIdentity = resolve;
    });
    infoMock.mockReturnValue(deferred as never);
    render(<App />);
    expect(screen.getByText(/reading application identity/i)).not.toBeNull();

    resolveIdentity({ name: "Astradew", version: "9.9.9" });
    expect((await screen.findByText("Astradew")).textContent).toBe("Astradew");
    expect(screen.getByText("Version 9.9.9")).not.toBeNull();
  });

  it("identity failure shows an alert reporting the refusal", async () => {
    infoMock.mockRejectedValue(new Error("connection refused"));
    render(<App />);
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("connection refused");
  });
});

describe("health view", () => {
  it("health route renders real backend values in every section", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    window.location.hash = "#/health";
    render(<App />);
    await screen.findByText("Astradew");

    const dataSection = await screen.findByRole("region", {
      name: "Data locations",
    });
    expect(healthMock).toHaveBeenCalledTimes(1);
    for (const dir of healthyReport.directories) {
      expect(within(dataSection).getByText(dir.path)).not.toBeNull();
    }
    expect(within(dataSection).getAllByText("Writable.")).toHaveLength(
      healthyReport.directories.length,
    );

    const dbSection = screen.getByRole("region", { name: "Database" });
    expect(dbSection.textContent).toContain(
      healthyReport.database.path,
    );
    expect(dbSection.textContent).toContain("schema version 1");

    const initSection = screen.getByRole("region", {
      name: "Initialisation",
    });
    expect(initSection.textContent).toContain("Resolve application data root");
    expect(initSection.textContent).toContain("Open database");

    const unavailableSection = screen.getByRole("region", {
      name: "Not yet available",
    });
    for (const entry of healthyReport.unavailable) {
      const item = within(unavailableSection).getByText(
        new RegExp(entry.name),
      );
      expect(item.textContent).toContain("unavailable");
      expect(item.textContent).not.toMatch(/healthy/i);
    }
    expect(screen.queryByRole("region", { name: "Findings" })).toBeNull();
  });

  it("broken directory shows the backend action text", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    const blocked = `${dataRoot}\\cache`;
    healthMock.mockResolvedValue({
      ...healthyReport,
      directories: healthyReport.directories.map((dir) =>
        dir.path === blocked ? { ...dir, writable: false } : dir,
      ),
      findings: [
        {
          severity: "error",
          what: `directory "Cache" at ${blocked} is not usable`,
          why: "Astradew keeps Cache data there",
          action: `Fix permissions on ${blocked} and restart`,
        },
      ],
    });
    window.location.hash = "#/health";
    render(<App />);
    await screen.findByText("Astradew");

    const findings = await screen.findByRole("region", { name: "Findings" });
    expect(findings.textContent).toContain(blocked);
    expect(findings.textContent).toContain("Fix permissions on");
    const dataSection = screen.getByRole("region", {
      name: "Data locations",
    });
    expect(dataSection.textContent).toContain("Not writable.");
  });

  it("says it is reading, then shows an alert on backend refusal", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    let rejectHealth!: (reason: unknown) => void;
    const deferred = new Promise<never>((_, reject) => {
      rejectHealth = reject;
    });
    healthMock.mockReturnValue(deferred as never);
    window.location.hash = "#/health";
    render(<App />);
    await screen.findByText("Astradew");
    expect(screen.getByText(/reading health/i)).not.toBeNull();

    rejectHealth(new Error("connection refused"));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Health is not available");
    expect(alert.textContent).toContain("connection refused");
  });

  it("unavailable database reports state instead of healthy", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    healthMock.mockResolvedValue({
      ...healthyReport,
      database: {
        path: "",
        version: 0,
        healthy: false,
        state: "unavailable: service constructed without a database handle",
      },
    });
    window.location.hash = "#/health";
    render(<App />);
    await screen.findByText("Astradew");

    const dbSection = await screen.findByRole("region", { name: "Database" });
    expect(dbSection.textContent).toContain("unavailable");
    expect(dbSection.textContent).not.toMatch(/schema version 1/);
  });
});

describe("failure probe", () => {
  it("idle shows the honest not-run state", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    render(<App />);
    await screen.findByText("Astradew");

    const diagnostics = screen.getByRole("region", { name: "Diagnostics" });
    expect(
      within(diagnostics).getByText("Failure probe not run."),
    ).not.toBeNull();
    expect(
      (
        within(diagnostics).getByRole("button", {
          name: "Run failure probe",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
    expect(probeMock).not.toHaveBeenCalled();
  });

  it("click runs the probe and a typed rejection shows the received code", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    probeMock.mockRejectedValue(
      Object.assign(new Error("call failed"), {
        cause: {
          code: "PROBE_FAILURE",
          message: "probe failure",
          details: "demo probe",
          recoverable: true,
        },
      }),
    );
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    await user.click(
      screen.getByRole("button", { name: "Run failure probe" }),
    );
    expect(probeMock).toHaveBeenCalledTimes(1);
    const status = await screen.findByText(/Probe failed with PROBE_FAILURE/);
    expect(status.textContent).toContain("probe failure");
  });

  it("rejection without a typed code shows the unexpected state", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    probeMock.mockRejectedValue(new Error("boom"));
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    await user.click(
      screen.getByRole("button", { name: "Run failure probe" }),
    );
    const status = await screen.findByText(/Probe result unexpected/);
    expect(status.textContent).toContain("boom");
  });

  it("probe resolution shows unexpected-succeeded", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    probeMock.mockResolvedValue({ name: "", version: "" });
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    await user.click(
      screen.getByRole("button", { name: "Run failure probe" }),
    );
    const status = await screen.findByText(/Probe unexpectedly succeeded/);
    expect(status.textContent).toContain("Probe result unexpected");
  });
});

describe("settings view", () => {
  it("settings route renders defaults from the backend list", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    window.location.hash = "#/settings";
    render(<App />);
    await screen.findByText("Astradew");

    const section = await screen.findByRole("region", { name: "Application settings" });
    expect(settingsMock).toHaveBeenCalledTimes(1);
    for (const setting of defaultSettings) {
      expect(within(section).getByText(setting.description)).not.toBeNull();
      expect(
        within(section).getByLabelText(new RegExp(setting.name)),
      ).not.toBeNull();
    }
    expect(
      within(section).getAllByText(/\(default\)/),
    ).toHaveLength(defaultSettings.length);
  });

  it("edit calls SetSetting and re-reads the list", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    window.location.hash = "#/settings";
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const theme = await screen.findByLabelText(/theme/);
    await user.clear(theme);
    await user.type(theme, "dark");
    await user.click(screen.getAllByRole("button", { name: "Save" })[0]);
    expect(setSettingMock).toHaveBeenCalledWith("theme", "dark");
    await screen.findByRole("region", { name: "Application settings" });
    expect(settingsMock).toHaveBeenCalledTimes(2);
  });

  it("invalid rejection shows the typed code in an alert", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    setSettingMock.mockRejectedValue(
      Object.assign(new Error("call failed"), {
        cause: {
          code: "SETTING_INVALID",
          message: "invalid value for setting",
          details: "setting",
          recoverable: true,
        },
      }),
    );
    window.location.hash = "#/settings";
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const scale = await screen.findByLabelText(/ui-scale/);
    await user.clear(scale);
    await user.type(scale, "500");
    await user.click(screen.getAllByRole("button", { name: "Save" })[1]);
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("SETTING_INVALID");
  });

  it("says it is reading, then shows an alert on backend refusal", async () => {
    infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
    let rejectSettings!: (reason: unknown) => void;
    const deferred = new Promise<never>((_, reject) => {
      rejectSettings = reject;
    });
    settingsMock.mockReturnValue(deferred as never);
    window.location.hash = "#/settings";
    render(<App />);
    await screen.findByText("Astradew");
    expect(screen.getByText(/reading settings/i)).not.toBeNull();

    rejectSettings(new Error("connection refused"));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Settings are not available");
    expect(alert.textContent).toContain("connection refused");
  });
});
