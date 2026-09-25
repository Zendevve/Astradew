import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import App from "../App";
import { ApplicationService } from "../../bindings/github.com/Zendevve/astradew/internal/app";

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
const pathsMock = vi.mocked(ApplicationService.Paths);
const healthMock = vi.mocked(ApplicationService.Health);
const recentTasksMock = vi.mocked(ApplicationService.RecentTasks);
const settingsMock = vi.mocked(ApplicationService.Settings);
const installsMock = vi.mocked(ApplicationService.GameInstalls);
const addInstallMock = vi.mocked(ApplicationService.AddGameInstall);
const setPrimaryMock = vi.mocked(ApplicationService.SetPrimaryGameInstall);

const dataRoot = "C:\\Users\\test\\AppData\\Local\\Astradew";
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
const healthyReport = {
  name: "Astradew",
  version: "0.0.0",
  dataRoot,
  directories: [],
  database: {
    path: `${dataRoot}\\database\\astradew.db`,
    version: 2,
    healthy: true,
    state: "open",
  },
  initialisation: [],
  findings: [],
  unavailable: [
    { name: "Game detection", status: "unavailable", reason: "not yet implemented in this phase" },
    { name: "SMAPI detection", status: "unavailable", reason: "not yet implemented in this phase" },
    { name: "Mod health", status: "unavailable", reason: "not yet implemented in this phase" },
  ],
  game: null,
  smapi: null,
};
const defaultSettings = [
  { name: "theme", kind: "string", value: "system", isDefault: true, description: "Colour scheme preference: system, light, or dark." },
  { name: "ui-scale", kind: "int", value: 100, isDefault: true, description: "Interface scale in percent, from 50 to 200." },
  { name: "check-updates-on-start", kind: "bool", value: true, isDefault: true, description: "Check for mod updates when Astradew starts." },
];

function typedRejection(code: string, details: string) {
  return Object.assign(new Error("call failed"), {
    cause: { code, message: code, details, recoverable: true },
  });
}

beforeEach(() => {
  infoMock.mockResolvedValue({ name: "Astradew", version: "0.0.0" });
  pathsMock.mockResolvedValue(dataPaths);
  healthMock.mockResolvedValue(healthyReport);
  recentTasksMock.mockResolvedValue([]);
  settingsMock.mockResolvedValue(defaultSettings);
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
  window.location.hash = "#/settings";
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  window.location.hash = "";
});

describe("game installs group", () => {
  it("renders the group with empty state and disabled Detect", async () => {
    render(<App />);
    await screen.findByText("Astradew");

    const group = await screen.findByRole("region", { name: "Game installs" });
    expect(within(group).getByText("No game configured yet. Run Detect now or choose the game folder.")).not.toBeNull();
    const detect = within(group).getByRole("button", { name: "Detect now" });
    expect(detect.hasAttribute("disabled")).toBe(true);
    expect(within(group).getByText("Auto-discovery lands in a later ticket.")).not.toBeNull();
    expect(within(group).getByLabelText("Choose game folder…")).not.toBeNull();
  });

  it("adds via text entry and re-reads the list", async () => {
    installsMock.mockResolvedValue([]);
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const group = await screen.findByRole("region", { name: "Game installs" });
    const input = within(group).getByLabelText("Choose game folder…");
    await user.type(input, "C:\\Games\\Stardew Valley");
    await user.click(within(group).getByRole("button", { name: "Add" }));
    expect(addInstallMock).toHaveBeenCalledWith("C:\\Games\\Stardew Valley");
    expect(installsMock.mock.calls.length).toBeGreaterThan(1);
  });

  it.each([
    ["GAME_NOT_FOUND", "Stardew Valley.dll is missing", "Choose the folder containing"],
    ["GAME_LEGACY", "too old", "Update the game"],
    ["GAME_INVALID", "unreadable: bad sector", "Verify game files"],
  ] as Array<[string, string, string]>)("refusal %s shows recovery copy", async (code, details, copy) => {
    addInstallMock.mockRejectedValueOnce(typedRejection(code, details));
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");
    const group = await screen.findByRole("region", { name: "Game installs" });
    await user.type(within(group).getByLabelText("Choose game folder…"), "C:\\Games\\Stardew Valley");
    await user.click(within(group).getByRole("button", { name: "Add" }));
    const alert = await within(group).findByRole("alert");
    expect(alert.textContent).toContain(code);
    expect(alert.textContent).toContain(copy);
  });

  it("installer-bundle refusal keys off the machine marker, not the sentence", async () => {
    addInstallMock.mockRejectedValueOnce(
      typedRejection("GAME_NOT_FOUND", "SMAPI_INSTALLER_BUNDLE: a rewritten human sentence that must still map"),
    );
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");
    const group = await screen.findByRole("region", { name: "Game installs" });
    await user.type(within(group).getByLabelText("Choose game folder…"), "C:\\bundle");
    await user.click(within(group).getByRole("button", { name: "Add" }));
    const alert = await within(group).findByRole("alert");
    expect(alert.textContent).toContain("run it instead");
  });

  it("primary card shows path, source, unknown versions, entry point and state", async () => {
    installsMock.mockResolvedValue([
      {
        id: 1,
        path: "C:\\Games\\Stardew Valley",
        source: "manual",
        smapiExePath: "StardewModdingAPI.exe",
        gameVersion: null,
        smapiVersion: null,
        smapiState: "complete",
        isPrimary: true,
      },
    ]);
    render(<App />);
    await screen.findByText("Astradew");
    const group = await screen.findByRole("region", { name: "Game installs" });
    expect(within(group).getByText("Path: C:\\Games\\Stardew Valley")).not.toBeNull();
    expect(within(group).getByText("Source: manual")).not.toBeNull();
    expect(within(group).getByText("Game version: version unknown")).not.toBeNull();
    expect(within(group).getByText("SMAPI entry point: StardewModdingAPI.exe")).not.toBeNull();
    expect(within(group).getByText(/SMAPI: complete/)).not.toBeNull();
    expect(within(group).getByText("Primary")).not.toBeNull();
  });
  it("primary card shows real detected versions", async () => {
    installsMock.mockResolvedValue([
      {
        id: 1,
        path: "C:\\Games\\Stardew Valley",
        source: "manual",
        smapiExePath: "StardewModdingAPI.exe",
        gameVersion: "1.6.15",
        smapiVersion: "4.1.10",
        smapiState: "complete",
        isPrimary: true,
      },
    ]);
    render(<App />);
    await screen.findByText("Astradew");
    const group = await screen.findByRole("region", { name: "Game installs" });
    expect(within(group).getByText("Game version: 1.6.15")).not.toBeNull();
    expect(within(group).getByText("SMAPI: complete 4.1.10")).not.toBeNull();
  });

  it("use-this switches primary through the explicit choice", async () => {
    installsMock.mockResolvedValue([
      {
        id: 1,
        path: "C:\\Games\\First",
        source: "manual",
        smapiExePath: null,
        gameVersion: null,
        smapiVersion: null,
        smapiState: "absent",
        isPrimary: true,
      },
      {
        id: 2,
        path: "C:\\Games\\Second",
        source: "manual",
        smapiExePath: null,
        gameVersion: null,
        smapiVersion: null,
        smapiState: "absent",
        isPrimary: false,
      },
    ]);
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");
    const group = await screen.findByRole("region", { name: "Game installs" });
    await user.click(within(group).getByRole("button", { name: "Use this" }));
    expect(setPrimaryMock).toHaveBeenCalledWith(2);
  });
});

describe("dashboard copy", () => {
  it("dashboard empty copy links to Settings with one picker flow", async () => {
    window.location.hash = "#/";
    render(<App />);
    await screen.findByText("Astradew");
    const main = await screen.findByRole("main");
    const link = await within(main).findByRole("link", { name: "Settings" });
    expect(link.getAttribute("href")).toBe("#/settings");
  });
});
