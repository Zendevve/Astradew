import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";

import App from "../App";
import {
  ApplicationService,
  type InspectionView,
  type Preview,
} from "../../bindings/github.com/Zendevve/astradew/internal/app";
import { FindingKind, Kind } from "../../bindings/github.com/Zendevve/astradew/internal/inspect";
import { Stage, Verdict } from "../../bindings/github.com/Zendevve/astradew/internal/manifest/models";
import { Status, type Task } from "../../bindings/github.com/Zendevve/astradew/internal/tasks/models";

// The runtime is not imported anywhere in src/ except the import sheet: it
// subscribes through Events and opens the native dialog through Dialogs. The
// stub records every subscription so a test can fire the live hint an
// inspection would have emitted.
const runtime = vi.hoisted(() => {
  type Handler = (event: { name: string; data: unknown }) => void;
  const listeners = new Map<string, Set<Handler>>();
  const openFile = vi.fn();
  return { listeners, openFile };
});

vi.mock("@wailsio/runtime", () => ({
  Events: {
    On(name: string, handler: (event: { name: string; data: unknown }) => void) {
      const set = runtime.listeners.get(name) ?? new Set();
      set.add(handler);
      runtime.listeners.set(name, set);
      return () => {
        set.delete(handler);
      };
    },
  },
  Dialogs: { OpenFile: runtime.openFile },
}));

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
    DetectNow: vi.fn(),
    InspectArchive: vi.fn(),
    Inspection: vi.fn(),
    CancelTask: vi.fn(),
  },
}));

const infoMock = vi.mocked(ApplicationService.Info);
const pathsMock = vi.mocked(ApplicationService.Paths);
const healthMock = vi.mocked(ApplicationService.Health);
const recentTasksMock = vi.mocked(ApplicationService.RecentTasks);
const settingsMock = vi.mocked(ApplicationService.Settings);
const installsMock = vi.mocked(ApplicationService.GameInstalls);
const addInstallMock = vi.mocked(ApplicationService.AddGameInstall);
const detectMock = vi.mocked(ApplicationService.DetectNow);
const inspectArchiveMock = vi.mocked(ApplicationService.InspectArchive);
const inspectionMock = vi.mocked(ApplicationService.Inspection);
const cancelTaskMock = vi.mocked(ApplicationService.CancelTask);

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
  unavailable: [],
  game: null,
  smapi: null,
};
const defaultSettings = [
  {
    name: "theme",
    kind: "string",
    value: "system",
    isDefault: true,
    description: "Colour scheme preference: system, light, or dark.",
  },
];

const zipPath = "C:\\Users\\test\\Downloads\\BetterFarm 1.2.0.zip";

const runningRow: Task = {
  id: "task-inspect-running",
  operation: "archive.inspect",
  status: Status.StatusRunning,
  current: 0,
  total: 0,
  message: "hashing the archive",
  outcome: null,
  createdAt: "2026-09-27T10:00:00.000Z",
  updatedAt: "2026-09-27T10:00:01.000Z",
};
const pendingRow: Task = { ...runningRow, id: "task-inspect-pending", status: Status.StatusPending, message: "" };
const succeededRow: Task = {
  ...runningRow,
  id: "task-inspect-done",
  status: Status.StatusSucceeded,
  message: "scanning the package",
  outcome: "3 units, 2 findings",
};
const failedRow: Task = {
  ...runningRow,
  id: "task-inspect-failed",
  status: Status.StatusFailed,
  message: "the archive contains an encrypted entry",
  outcome: "ARCHIVE_ENCRYPTED",
};
const cancelledRow: Task = {
  ...runningRow,
  id: "task-inspect-cancelled",
  status: Status.StatusCancelled,
  message: "cancelled by the user",
  outcome: null,
};

/** The package-level XNB copy, as internal/inspect sends it in finding.message. */
const xnbPackageCopy =
  "This package replaces Stardew Valley content files directly.\nAstradew does not install XNB mods automatically.";

const preview: Preview = {
  sourcePath: zipPath,
  originalName: "BetterFarm 1.2.0.zip",
  sizeBytes: 5_242_880,
  archiveSha256: "aa11bb22cc33dd44ee55ff6600778899aabbccddeeff00112233445566778899",
  packageSha256: "99887766554433221100ffeeddccbbaa99887766554433221100ffeeddccbbaa",
  units: [
    {
      relativePath: "BetterFarm 1.2.0/BetterFarm",
      folderName: "BetterFarm",
      name: "Better Farm",
      author: "Alecia",
      version: "1.2.0",
      uniqueID: "Alecia.BetterFarm",
      kind: Kind.KindCodeMod,
      entryDll: "BetterFarm.dll",
      contentPackFor: "",
      dependencies: [
        { uniqueID: "Pathoschild.ContentPatcher", minimumVersion: "2.0.0", isRequired: true },
      ],
      verdict: Verdict.VerdictPartial,
      fieldErrors: [
        {
          field: "UpdateKeys",
          stage: Stage.StageValidation,
          message: "it is not a recognised update key",
        },
      ],
      systemMod: false,
      installable: true,
      note: "",
    },
    {
      relativePath: "BetterFarm 1.2.0/[CP] Better Farm",
      folderName: "[CP] Better Farm",
      name: "Better Farm Content",
      author: "Alecia",
      version: "1.2.0",
      uniqueID: "Alecia.BetterFarm.Content",
      kind: Kind.KindContentPack,
      entryDll: "",
      contentPackFor: "Alecia.BetterFarm",
      dependencies: [],
      verdict: Verdict.VerdictValid,
      fieldErrors: [],
      systemMod: false,
      installable: true,
      note: "",
    },
    {
      relativePath: "OldColors",
      folderName: "OldColors",
      name: "",
      author: "",
      version: "",
      uniqueID: "",
      kind: Kind.KindInvalid,
      entryDll: "",
      contentPackFor: "",
      dependencies: [],
      verdict: Verdict.VerdictInvalid,
      fieldErrors: [
        { field: "manifest.json", stage: Stage.StageSyntax, message: "it is not valid JSON" },
      ],
      systemMod: false,
      installable: false,
      note: "its manifest.json could not be read",
    },
  ],
  findings: [
    {
      kind: FindingKind.FindingLegacyXnb,
      path: "OldColors",
      message: "it's not a SMAPI mod (see https://smapi.io/xnb for info).",
    },
    { kind: FindingKind.FindingLegacyXnb, path: "", message: xnbPackageCopy },
  ],
  duplicates: [
    {
      uniqueID: "Alecia.BetterFarm",
      entries: [
        { path: "BetterFarm 1.2.0/BetterFarm", version: "1.2.0" },
        { path: "Extra/BetterFarm", version: "1.1.0" },
      ],
      launchBlocker: true,
    },
  ],
  installable: true,
};

const succeededView: InspectionView = {
  task: succeededRow,
  status: "succeeded",
  preview,
  failure: null,
};
const runningView: InspectionView = {
  task: runningRow,
  status: "running",
  preview: null,
  failure: null,
};

function typedRejection(code: string, details: string) {
  return Object.assign(new Error("call failed"), {
    cause: { code, message: code, details, recoverable: true },
  });
}

/** Fire a task event at whatever is subscribed, exactly as the runtime would. */
function emitEvent(name: string, data: unknown) {
  for (const handler of runtime.listeners.get(name) ?? []) {
    handler({ name, data });
  }
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
  detectMock.mockResolvedValue({
    found: [],
    adopted: null,
    pointerOutcome: "kept-healthy",
    needsChoice: false,
    message: "No Stardew Valley install found. Choose the game folder manually.",
    installsKnown: 0,
  });
  runtime.openFile.mockResolvedValue(zipPath);
  window.location.hash = "#/library";
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  runtime.listeners.clear();
  window.location.hash = "";
});

/** Open the Library route's import sheet and return the dialog element. */
async function openImportSheet(user: UserEvent) {
  await user.click(await screen.findByRole("button", { name: "Import archive…" }));
  return await screen.findByRole("dialog", { name: "Import archive" });
}

/** Open an idle sheet and start a run through the file dialog. */
async function startRun(user: UserEvent) {
  const dialog = await openImportSheet(user);
  await user.click(within(dialog).getByRole("button", { name: "Choose .zip archive…" }));
  return dialog;
}

describe("library import action", () => {
  it("offers the import action beside the honest empty copy", async () => {
    render(<App />);
    await screen.findByText("Astradew");

    const region = await screen.findByRole("region", { name: "Import an archive" });
    const action = within(region).getByRole("button", { name: "Import archive…" });
    expect(action.hasAttribute("disabled")).toBe(false);
    expect(within(region).queryByText(/Import is unavailable/)).toBeNull();
    expect(screen.getByText("No mods installed. Mods you add will be listed here.")).not.toBeNull();
  });

  it("disables the action with a visible reason while an inspection runs", async () => {
    recentTasksMock.mockResolvedValue([runningRow]);
    inspectionMock.mockResolvedValue(runningView);
    render(<App />);
    await screen.findByText("Astradew");

    const region = await screen.findByRole("region", { name: "Import an archive" });
    const action = within(region).getByRole("button", { name: "Import archive…" });
    await waitFor(() => expect(action.hasAttribute("disabled")).toBe(true));
    const reason = within(region).getByText(
      "Import is unavailable: an inspection is already running.",
    );
    expect(action.getAttribute("aria-describedby")).toBe(reason.getAttribute("id"));
  });

  it("disables the action with a reason when the backend did not answer", async () => {
    recentTasksMock.mockRejectedValue(new Error("runtime unavailable"));
    render(<App />);
    await screen.findByText("Astradew");

    const region = await screen.findByRole("region", { name: "Import an archive" });
    const action = within(region).getByRole("button", { name: "Import archive…" });
    await waitFor(() => expect(action.hasAttribute("disabled")).toBe(true));
    const reason = within(region).getByText(
      "Import is unavailable: the backend did not answer.",
    );
    expect(action.getAttribute("aria-describedby")).toBe(reason.getAttribute("id"));
  });

  it("opens the sheet in its idle state and explains the flow", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await openImportSheet(user);
    expect(dialog.getAttribute("aria-modal")).toBe("true");
    expect(within(dialog).getByText(/Nothing is installed/)).not.toBeNull();
    expect(within(dialog).getByRole("button", { name: "Choose .zip archive…" })).not.toBeNull();
  });
});

describe("liveness of the newest running row", () => {
  it("enables Import and reopens as expired when the row has no run behind it", async () => {
    recentTasksMock.mockResolvedValue([runningRow]);
    inspectionMock.mockRejectedValue(
      typedRejection(
        "INSPECTION_NOT_FOUND",
        `inspection "${runningRow.id}" is no longer in memory (running)`,
      ),
    );
    render(<App />);
    await screen.findByText("Astradew");

    const region = await screen.findByRole("region", { name: "Import an archive" });
    const action = within(region).getByRole("button", { name: "Import archive…" });
    await waitFor(() => expect(action.hasAttribute("disabled")).toBe(false));
    expect(within(region).queryByText(/Import is unavailable/)).toBeNull();

    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    expect(
      await within(dialog).findByText(/This inspection is from an earlier run of Astradew/),
    ).not.toBeNull();
  });

  it("keeps Import disabled and reopens live while the process holds the run", async () => {
    recentTasksMock.mockResolvedValue([runningRow]);
    inspectionMock.mockResolvedValue(runningView);
    render(<App />);
    await screen.findByText("Astradew");

    const region = await screen.findByRole("region", { name: "Import an archive" });
    const action = within(region).getByRole("button", { name: "Import archive…" });
    await waitFor(() => expect(action.hasAttribute("disabled")).toBe(true));
    const reason = within(region).getByText(
      "Import is unavailable: an inspection is already running.",
    );
    expect(action.getAttribute("aria-describedby")).toBe(reason.getAttribute("id"));

    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    expect(
      await within(dialog).findByRole("button", { name: "Cancel inspection" }),
    ).not.toBeNull();
  });

  it("keeps Import disabled with its own reason when the process cannot answer", async () => {
    recentTasksMock.mockResolvedValue([runningRow]);
    inspectionMock.mockRejectedValue(
      typedRejection("STORE_OPEN_FAILED", "task store unavailable: no open database"),
    );
    render(<App />);
    await screen.findByText("Astradew");

    const region = await screen.findByRole("region", { name: "Import an archive" });
    const action = within(region).getByRole("button", { name: "Import archive…" });
    await waitFor(() => expect(action.hasAttribute("disabled")).toBe(true));
    expect(
      within(region).getByText(
        "Import is unavailable: the running inspection could not be read (STORE_OPEN_FAILED).",
      ),
    ).not.toBeNull();
    expect(
      within(region).queryByText("Import is unavailable: an inspection is already running."),
    ).toBeNull();
  });

  it("closes a stale row's sheet without cancelling a run that is not there", async () => {
    recentTasksMock.mockResolvedValue([runningRow]);
    let reads = 0;
    inspectionMock.mockImplementation((() => {
      reads += 1;
      // The section's liveness read refuses; the sheet's own read never
      // answers, so the sheet is still reading when it is closed.
      if (reads === 1) {
        return Promise.reject(
          typedRejection(
            "INSPECTION_NOT_FOUND",
            `inspection "${runningRow.id}" is no longer in memory (running)`,
          ),
        );
      }
      return new Promise<InspectionView>(() => {});
    }) as unknown as typeof ApplicationService.Inspection);
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    expect(within(dialog).getByText("Reading the inspection…")).not.toBeNull();
    await user.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(cancelTaskMock).not.toHaveBeenCalled();
  });
});

describe("inspection flow", () => {
  it("follows idle to inspecting to preview, with a progress event moving the bar", async () => {
    const user = userEvent.setup();
    inspectArchiveMock.mockResolvedValue(pendingRow);
    inspectionMock.mockResolvedValue(succeededView);
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await startRun(user);
    expect(runtime.openFile).toHaveBeenCalledWith({
      Title: "Choose a mod archive to inspect",
      ButtonText: "Inspect",
      Filters: [{ DisplayName: "Zip archives", Pattern: "*.zip" }],
    });
    expect(inspectArchiveMock).toHaveBeenCalledWith(zipPath);

    const cancel = await within(dialog).findByRole("button", { name: "Cancel inspection" });
    expect(cancel).not.toBeNull();
    expect(within(dialog).getByText("The inspection is running.")).not.toBeNull();
    const bar = within(dialog).getByRole("progressbar");
    expect(bar.getAttribute("value")).toBeNull();

    act(() => {
      emitEvent("task.progress", {
        taskId: pendingRow.id,
        operation: "archive.inspect",
        current: 2_621_440,
        total: 10_485_760,
        message: "hashing the archive",
      });
    });
    expect(within(dialog).getByText(/hashing the archive/)).not.toBeNull();
    expect(within(dialog).getByText("2.5 MiB of 10 MiB — 25%")).not.toBeNull();
    expect(bar.getAttribute("value")).toBe("25");

    act(() => {
      emitEvent("task.completed", {
        taskId: pendingRow.id,
        operation: "archive.inspect",
        current: 0,
        total: 0,
        message: "3 units, 2 findings",
      });
    });
    expect(await screen.findByText("Nothing is installed yet — inspect only.")).not.toBeNull();
    expect(inspectionMock).toHaveBeenCalledWith(pendingRow.id);
    expect(bar.isConnected).toBe(false);
  });

  it("ignores events for another task", async () => {
    const user = userEvent.setup();
    inspectArchiveMock.mockResolvedValue(pendingRow);
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await startRun(user);
    await within(dialog).findByRole("button", { name: "Cancel inspection" });

    act(() => {
      emitEvent("task.progress", {
        taskId: "task-someone-else",
        operation: "archive.inspect",
        current: 1,
        total: 2,
        message: "hashing the archive",
      });
      emitEvent("task.completed", { taskId: "task-someone-else", message: "2 units, 0 findings" });
    });
    expect(within(dialog).queryByText(/hashing the archive/)).toBeNull();
    expect(inspectionMock).not.toHaveBeenCalled();
  });

  it("shows the indeterminate phase as indeterminate", async () => {
    const user = userEvent.setup();
    inspectArchiveMock.mockResolvedValue(pendingRow);
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await startRun(user);
    await within(dialog).findByRole("button", { name: "Cancel inspection" });
    act(() => {
      emitEvent("task.progress", {
        taskId: pendingRow.id,
        operation: "archive.inspect",
        current: 0,
        total: 0,
        message: "scanning the package",
      });
    });

    expect(within(dialog).getByText("scanning the package")).not.toBeNull();
    expect(
      within(dialog).getByText(/stays indeterminate/),
    ).not.toBeNull();
    expect(within(dialog).getByRole("progressbar").getAttribute("value")).toBeNull();
  });

  it("renders the whole preview: identity, digests, units, findings, duplicates", async () => {
    recentTasksMock.mockResolvedValue([succeededRow]);
    inspectionMock.mockResolvedValue(succeededView);
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    await screen.findByText("Nothing is installed yet — inspect only.");

    const identity = within(dialog).getByRole("region", { name: "Archive identity" });
    expect(within(identity).getByText("BetterFarm 1.2.0.zip")).not.toBeNull();
    expect(within(identity).getByText("5.0 MiB (5,242,880 bytes)")).not.toBeNull();
    expect(within(identity).getByText(preview.archiveSha256)).not.toBeNull();
    expect(within(identity).getByText(preview.packageSha256)).not.toBeNull();
    expect(within(identity).getByText(zipPath)).not.toBeNull();

    const units = within(dialog).getByRole("region", { name: "Mod units" });
    expect(within(units).getByText("Mod units (3)")).not.toBeNull();
    const codeMod = within(units).getByRole("article", { name: "Mod Unit Better Farm" });
    expect(within(codeMod).getByText(/Code mod · entry BetterFarm\.dll/)).not.toBeNull();
    expect(within(codeMod).getByText("1.2.0")).not.toBeNull();
    expect(within(codeMod).getByText("Alecia.BetterFarm")).not.toBeNull();
    expect(within(codeMod).getByText("Alecia")).not.toBeNull();
    expect(within(codeMod).getByText("BetterFarm 1.2.0/BetterFarm")).not.toBeNull();
    expect(within(codeMod).getByText(/partial — installed with warnings/)).not.toBeNull();
    expect(within(codeMod).getByText("Installable")).not.toBeNull();
    expect(
      within(codeMod).getByText("Pathoschild.ContentPatcher — 2.0.0 or newer (required)"),
    ).not.toBeNull();
    expect(
      within(codeMod).getByText("UpdateKeys (validation): it is not a recognised update key"),
    ).not.toBeNull();

    const contentPack = within(units).getByRole("article", {
      name: "Mod Unit Better Farm Content",
    });
    expect(
      within(contentPack).getByText(/Content pack · content pack for Alecia\.BetterFarm/),
    ).not.toBeNull();
    expect(within(contentPack).getByText("BetterFarm 1.2.0/[CP] Better Farm")).not.toBeNull();

    const invalid = within(units).getByRole("article", { name: "Mod Unit OldColors" });
    expect(within(invalid).getByText(/Invalid manifest/)).not.toBeNull();
    expect(
      within(invalid).getByText("Not installable: its manifest.json could not be read"),
    ).not.toBeNull();
    expect(
      within(invalid).getByText("manifest.json (syntax): it is not valid JSON"),
    ).not.toBeNull();

    const findings = within(dialog).getByRole("region", { name: "Findings" });
    expect(within(findings).getByText("Findings (2)")).not.toBeNull();
    expect(
      within(findings).getByText(/replaces Stardew Valley content files directly\./),
    ).not.toBeNull();
    expect(
      within(findings).getByText(/Astradew does not install XNB mods automatically\./),
    ).not.toBeNull();
    expect(within(findings).getByText(/Legacy XNB content — OldColors/)).not.toBeNull();

    const duplicates = within(dialog).getByRole("region", { name: "Duplicate Unique IDs" });
    expect(
      within(duplicates).getByText(/Alecia\.BetterFarm — SMAPI cannot tell these copies apart/),
    ).not.toBeNull();
    expect(within(duplicates).getByText("Extra/BetterFarm (version 1.1.0)")).not.toBeNull();

    expect(
      within(dialog).getByText(/reports this package as installable/),
    ).not.toBeNull();
    expect(within(dialog).getByText(/Inspecting changed nothing/)).not.toBeNull();
  });

  it("says a package with no installable unit cannot be installed", async () => {
    recentTasksMock.mockResolvedValue([succeededRow]);
    inspectionMock.mockResolvedValue({
      task: succeededRow,
      status: "succeeded",
      preview: { ...preview, units: [], findings: [], duplicates: [], installable: false },
      failure: null,
    });
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    expect(await within(dialog).findByText("This package holds no Mod Unit.")).not.toBeNull();
    expect(
      within(dialog).getByText(/reports this package as not installable/),
    ).not.toBeNull();
  });

  it("a started run disables the import action until it ends", async () => {
    let rows: Task[] = [];
    // Wails returns a CancellablePromise; the stub only has to be thenable.
    recentTasksMock.mockImplementation(
      (() => Promise.resolve(rows)) as unknown as typeof ApplicationService.RecentTasks,
    );
    inspectArchiveMock.mockResolvedValue(pendingRow);
    // The row alone does not disable the action: the process confirming the
    // run is live does.
    inspectionMock.mockResolvedValue({ ...runningView, task: pendingRow });
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const region = await screen.findByRole("region", { name: "Import an archive" });
    const action = within(region).getByRole("button", { name: "Import archive…" });
    await openImportSheet(user);
    rows = [pendingRow];
    await user.click(screen.getByRole("button", { name: "Choose .zip archive…" }));
    await screen.findByRole("button", { name: "Cancel inspection" });
    await waitFor(() => expect(action.hasAttribute("disabled")).toBe(true));
  });
});

describe("mount-time re-read", () => {
  it("reopens a running inspection with live progress", async () => {
    recentTasksMock.mockResolvedValue([runningRow]);
    inspectionMock.mockResolvedValue(runningView);
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    await within(dialog).findByRole("button", { name: "Cancel inspection" });
    expect(inspectionMock).toHaveBeenCalledWith(runningRow.id);
    expect(within(dialog).getByText("hashing the archive")).not.toBeNull();

    act(() => {
      emitEvent("task.progress", {
        taskId: runningRow.id,
        operation: "archive.inspect",
        current: 1_048_576,
        total: 4_194_304,
        message: "hashing the archive",
      });
    });
    expect(within(dialog).getByRole("progressbar").getAttribute("value")).toBe("25");
  });

  it("reopens a finished inspection with its preview", async () => {
    recentTasksMock.mockResolvedValue([succeededRow]);
    inspectionMock.mockResolvedValue(succeededView);
    render(<App />);
    await screen.findByText("Astradew");

    await screen.findByRole("dialog", { name: "Import archive" });
    expect(await screen.findByText("Nothing is installed yet — inspect only.")).not.toBeNull();
    expect(screen.getByRole("article", { name: "Mod Unit Better Farm" })).not.toBeNull();
  });

  it("reopens a failed inspection with its typed refusal", async () => {
    recentTasksMock.mockResolvedValue([failedRow]);
    inspectionMock.mockResolvedValue({
      task: failedRow,
      status: "failed",
      preview: null,
      failure: {
        code: "ARCHIVE_ENCRYPTED",
        message: "the archive contains an encrypted entry",
        details: 'entry "secret/config.json": it is encrypted',
      },
    });
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    const alert = await within(dialog).findByRole("alert");
    expect(alert.textContent).toContain("encrypted (password-protected)");
    expect(
      within(dialog).getByText('Details: entry "secret/config.json": it is encrypted'),
    ).not.toBeNull();
    expect(within(dialog).getByText("Refusal code: ARCHIVE_ENCRYPTED")).not.toBeNull();
  });

  it("reopens a cancelled inspection with an honest note", async () => {
    recentTasksMock.mockResolvedValue([cancelledRow]);
    inspectionMock.mockResolvedValue({
      task: cancelledRow,
      status: "cancelled",
      preview: null,
      failure: null,
    });
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    expect(
      await within(dialog).findByText(/This inspection was cancelled\. Nothing was installed/),
    ).not.toBeNull();
  });

  it("shows the expired state for a row from a previous app run", async () => {
    recentTasksMock.mockResolvedValue([succeededRow]);
    inspectionMock.mockRejectedValue(
      typedRejection(
        "INSPECTION_NOT_FOUND",
        `inspection "${succeededRow.id}" is no longer in memory (succeeded)`,
      ),
    );
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    const note = await within(dialog).findByText(/This inspection is from an earlier run of Astradew/);
    expect(note.textContent).toContain("Nothing was installed");
    expect(within(dialog).queryByRole("alert")).toBeNull();
  });

  it("does not reopen anything when no inspection was ever recorded", async () => {
    recentTasksMock.mockResolvedValue([]);
    render(<App />);
    await screen.findByText("Astradew");

    await screen.findByRole("region", { name: "Import an archive" });
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("refusals", () => {
  const refusalCases: Array<[string, RegExp, string]> = [
    ["ARCHIVE_UNREADABLE", /could not read the chosen file/, "the file changed while it was being inspected"],
    ["ARCHIVE_PATH_TRAVERSAL", /would land outside the inspection's temporary folder/, 'entry "../evil.txt"'],
    ["ARCHIVE_LINK_ENTRY", /symbolic link entry/, 'entry "link/": it is a symbolic link'],
    ["ARCHIVE_ENCRYPTED", /encrypted \(password-protected\) entry/, 'entry "secret.zip": it is encrypted'],
    ["ARCHIVE_NAME_INVALID", /holds a name this computer cannot represent/, 'entry "AUX.txt": it is a reserved device name'],
    ["ARCHIVE_LIMIT_EXCEEDED", /breaks one of the archive safety limits/, 'entry "bomb.txt": it expanded past the 100:1 ratio limit'],
    ["ARCHIVE_CORRUPT", /is not a readable ZIP/, 'entry "data.bin": it declares 10 bytes but only 4 could be read'],
    ["INSPECTION_FAILED", /could not write its temporary files/, 'stage "inspect/task-1": access is denied'],
    ["INSPECTION_BUSY", /Another inspection is already running/, "task task-inspect-running is running"],
    ["TASK_NOT_CANCELLABLE", /there was nothing to cancel/, "task task-1 is succeeded"],
    ["TASK_NOT_FOUND", /has no record/, "task task-1: no row carries it"],
    ["STORE_OPEN_FAILED", /database is not open/, "task store unavailable: no open database"],
  ];

  /** Start a run whose start refuses with code, and return the sheet's alert text. */
  async function refusalAlert(user: UserEvent, code: string, details: string) {
    inspectArchiveMock.mockRejectedValue(typedRejection(code, details));
    render(<App />);
    await screen.findByText("Astradew");
    const dialog = await startRun(user);
    const alert = await within(dialog).findByRole("alert");
    return { dialog, alert };
  }

  it.each(refusalCases)(
    "renders %s with its own copy, code, and the offending entry",
    async (code, copy, details) => {
      const user = userEvent.setup();
      const { dialog, alert } = await refusalAlert(user, code, details);

      expect(alert.textContent).toMatch(copy);
      expect(within(dialog).getByText(`Refusal code: ${code}`)).not.toBeNull();
      expect(within(dialog).getByText(`Details: ${details}`)).not.toBeNull();
    },
  );

  it("gives every refusal code a distinct copy", async () => {
    const user = userEvent.setup();
    const copies = new Map<string, string>();
    for (const [code, , details] of refusalCases) {
      const { alert } = await refusalAlert(user, code, details);
      copies.set(code, alert.textContent ?? "");
      cleanup();
    }
    expect(copies.size).toBe(refusalCases.length);
    for (const [code, copy] of copies) {
      expect(copy.length, code).toBeGreaterThan(0);
      expect(copy, code).not.toContain("carried no typed code");
    }
    expect(new Set(copies.values()).size).toBe(refusalCases.length);
  });

  it("falls back to the backend's words when a rejection carries no typed code", async () => {
    inspectArchiveMock.mockRejectedValue(new Error("binding exploded"));
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await startRun(user);
    const alert = await within(dialog).findByRole("alert");
    expect(alert.textContent).toContain("carried no typed code");
    expect(within(dialog).getByText("Refusal code: none")).not.toBeNull();
  });
});

describe("cancellation", () => {
  it("cancels the run and closes only once the cancellation settles", async () => {
    recentTasksMock.mockResolvedValue([runningRow]);
    inspectionMock.mockResolvedValue(runningView);
    let settle: (task: Task) => void = () => {};
    cancelTaskMock.mockImplementation(
      (() =>
        new Promise<Task>((resolve) => {
          settle = resolve;
        })) as unknown as typeof ApplicationService.CancelTask,
    );
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    await user.click(await within(dialog).findByRole("button", { name: "Close" }));

    expect(cancelTaskMock).toHaveBeenCalledWith(runningRow.id);
    expect(
      within(dialog).getByText("Cancelling the inspection and removing its temporary files…"),
    ).not.toBeNull();
    expect(
      (within(dialog).getByRole("button", { name: "Close" }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(screen.queryByRole("dialog", { name: "Import archive" })).not.toBeNull();

    await act(async () => {
      settle({ ...runningRow, status: Status.StatusCancelled });
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("cancels on Esc as well", async () => {
    recentTasksMock.mockResolvedValue([runningRow]);
    inspectionMock.mockResolvedValue(runningView);
    cancelTaskMock.mockResolvedValue({ ...runningRow, status: Status.StatusCancelled });
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    await screen.findByRole("dialog", { name: "Import archive" });
    await user.keyboard("{Escape}");

    expect(cancelTaskMock).toHaveBeenCalledWith(runningRow.id);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("does not cancel when a finished inspection is closed", async () => {
    recentTasksMock.mockResolvedValue([succeededRow]);
    inspectionMock.mockResolvedValue(succeededView);
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    await screen.findByRole("dialog", { name: "Import archive" });
    await user.keyboard("{Escape}");

    expect(cancelTaskMock).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("cancels when a live row is closed before its read answers", async () => {
    recentTasksMock.mockResolvedValue([runningRow]);
    inspectionMock
      // The section confirms the run is live, then the sheet's own read hangs.
      .mockResolvedValueOnce(runningView)
      .mockImplementation(
        (() => new Promise<InspectionView>(() => {})) as unknown as typeof ApplicationService.Inspection,
      );
    cancelTaskMock.mockResolvedValue({ ...runningRow, status: Status.StatusCancelled });
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    expect(within(dialog).getByText("Reading the inspection…")).not.toBeNull();
    await user.keyboard("{Escape}");

    expect(cancelTaskMock).toHaveBeenCalledWith(runningRow.id);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("does not cancel when a finished row is closed before its read answers", async () => {
    recentTasksMock.mockResolvedValue([succeededRow]);
    inspectionMock.mockImplementation(
      (() => new Promise<InspectionView>(() => {})) as unknown as typeof ApplicationService.Inspection,
    );
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    await screen.findByRole("dialog", { name: "Import archive" });
    await user.keyboard("{Escape}");

    expect(cancelTaskMock).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("unsubscribes from every task event when the sheet closes", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    await openImportSheet(user);
    expect(runtime.listeners.get("task.progress")?.size).toBe(1);

    await user.keyboard("{Escape}");
    await waitFor(() => expect(runtime.listeners.get("task.progress")?.size).toBe(0));
    expect(runtime.listeners.get("task.completed")?.size).toBe(0);
  });
});

describe("refused cancellation", () => {
  it("stays open and shows the row's settled state when the cancel is refused", async () => {
    recentTasksMock.mockResolvedValue([runningRow]);
    inspectionMock
      .mockResolvedValueOnce(runningView) // the section's liveness read
      .mockResolvedValueOnce(runningView) // the sheet's own read
      .mockResolvedValue(succeededView); // the re-read after the refusal
    cancelTaskMock.mockRejectedValue(
      typedRejection("TASK_NOT_CANCELLABLE", `task ${runningRow.id} is succeeded`),
    );
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    await user.click(await within(dialog).findByRole("button", { name: "Close" }));

    expect(cancelTaskMock).toHaveBeenCalledWith(runningRow.id);
    expect(
      await within(dialog).findByText("Nothing is installed yet — inspect only."),
    ).not.toBeNull();

    // Now that the sheet shows a settled state, closing works again.
    await user.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(cancelTaskMock).toHaveBeenCalledTimes(1);
  });

  it("shows the refusal when the cancel is refused and the row cannot be read", async () => {
    recentTasksMock.mockResolvedValue([runningRow]);
    inspectionMock
      .mockResolvedValueOnce(runningView) // the section's liveness read
      .mockResolvedValueOnce(runningView) // the sheet's own read
      .mockRejectedValue(
        typedRejection("STORE_OPEN_FAILED", "task store unavailable: no open database"),
      ); // the re-read after the refusal
    cancelTaskMock.mockRejectedValue(
      typedRejection("TASK_NOT_CANCELLABLE", `task ${runningRow.id} is succeeded`),
    );
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    await user.click(await within(dialog).findByRole("button", { name: "Close" }));

    const alert = await within(dialog).findByRole("alert");
    expect(alert.textContent).toContain("there was nothing to cancel");
    expect(within(dialog).getByText("Refusal code: TASK_NOT_CANCELLABLE")).not.toBeNull();
    expect(
      within(dialog).getByText(`Details: task ${runningRow.id} is succeeded`),
    ).not.toBeNull();
    expect(screen.queryByRole("dialog", { name: "Import archive" })).not.toBeNull();
  });
});

describe("dialog semantics and the file dialog", () => {
  it("moves focus into the sheet and returns it to the opener", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const opener = await screen.findByRole("button", { name: "Import archive…" });
    const dialog = await openImportSheet(user);
    expect(dialog.contains(document.activeElement)).toBe(true);

    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(document.activeElement).toBe(opener);
  });

  it("keeps Tab inside the sheet", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await openImportSheet(user);
    await user.tab({ shift: true });
    expect(dialog.contains(document.activeElement)).toBe(true);
    await user.tab();
    expect(dialog.contains(document.activeElement)).toBe(true);
  });

  it("does nothing at all when the file dialog is cancelled", async () => {
    runtime.openFile.mockResolvedValue("");
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await openImportSheet(user);
    await user.click(within(dialog).getByRole("button", { name: "Choose .zip archive…" }));

    expect(inspectArchiveMock).not.toHaveBeenCalled();
    expect(within(dialog).queryByRole("alert")).toBeNull();
    expect(within(dialog).getByRole("button", { name: "Choose .zip archive…" })).not.toBeNull();
    expect(cancelTaskMock).not.toHaveBeenCalled();
  });

  /** Click the backdrop and put focus on the body, wherever the environment left it. */
  async function clickBackdrop(user: UserEvent) {
    const backdrop = document.querySelector(".sheet-backdrop");
    expect(backdrop).not.toBeNull();
    await user.click(backdrop as HTMLElement);
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur();
    }
    expect(document.activeElement).toBe(document.body);
  }

  it("still closes with Esc after a backdrop click moved focus out of the sheet", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    await openImportSheet(user);
    await clickBackdrop(user);

    // The backdrop click itself deliberately does not close...
    expect(screen.queryByRole("dialog", { name: "Import archive" })).not.toBeNull();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("still traps Tab after a backdrop click moved focus out of the sheet", async () => {
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    const dialog = await openImportSheet(user);
    await clickBackdrop(user);

    await user.tab();
    expect(dialog.contains(document.activeElement)).toBe(true);
    await user.tab({ shift: true });
    expect(dialog.contains(document.activeElement)).toBe(true);
  });

  it("does not let a closed sheet handle the next sheet's keys", async () => {
    // Sheet one: a stale running row, opened and closed with Esc.
    recentTasksMock.mockResolvedValue([runningRow]);
    inspectionMock.mockRejectedValue(
      typedRejection(
        "INSPECTION_NOT_FOUND",
        `inspection "${runningRow.id}" is no longer in memory (running)`,
      ),
    );
    inspectArchiveMock.mockResolvedValue(pendingRow);
    // Sheet two's cancellation never settles, so its own Esc is deliberately
    // inert: only a listener left behind by sheet one could still close it.
    cancelTaskMock.mockImplementation(
      (() => new Promise<Task>(() => {})) as unknown as typeof ApplicationService.CancelTask,
    );
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Astradew");

    await screen.findByRole("dialog", { name: "Import archive" });
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    await user.click(await screen.findByRole("button", { name: "Import archive…" }));
    const dialog = await screen.findByRole("dialog", { name: "Import archive" });
    await user.click(within(dialog).getByRole("button", { name: "Choose .zip archive…" }));
    await user.click(await within(dialog).findByRole("button", { name: "Cancel inspection" }));
    expect(
      await within(dialog).findByText(/Cancelling the inspection and removing its temporary files/),
    ).not.toBeNull();

    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "Import archive" })).not.toBeNull();
  });
});
