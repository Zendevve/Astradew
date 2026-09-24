import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import StartupTaskSection from "../pages/StartupTaskSection";
import { ApplicationService } from "../../bindings/github.com/Zendevve/astradew/internal/app";
import { Status, type Task } from "../../bindings/github.com/Zendevve/astradew/internal/tasks/models";

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
  },
}));

const recentTasksMock = vi.mocked(ApplicationService.RecentTasks);

const startupRecord: Task = {
  id: "task-20260924-120000-abcdef",
  operation: "startup",
  status: Status.StatusSucceeded,
  current: 1,
  total: 2,
  message: "root resolved",
  outcome: "startup complete at schema version 1",
  createdAt: "2026-09-24T12:00:00.000Z",
  updatedAt: "2026-09-24T12:00:01.000Z",
};

beforeEach(() => {
  recentTasksMock.mockResolvedValue([]);
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  window.location.hash = "";
});

describe("startup task section", () => {
  it("re-reads tasks on mount and renders the startup record", async () => {
    recentTasksMock.mockResolvedValue([startupRecord]);
    render(<StartupTaskSection />);

    expect(await screen.findByText(/startup complete at schema version 1/)).not.toBeNull();
    expect(screen.getByText(/root resolved/)).not.toBeNull();
    expect(screen.getByText(/2026-09-24T12:00:00.000Z/)).not.toBeNull();
    expect(recentTasksMock).toHaveBeenCalledTimes(1);
  });

  it("failed record shows the durably recorded code as text", async () => {
    recentTasksMock.mockResolvedValue([
      {
        ...startupRecord,
        status: Status.StatusFailed,
        message: "disk went away",
        outcome: "STORE_OPEN_FAILED",
      },
    ]);
    render(<StartupTaskSection />);

    const status = await screen.findByText(/failed with/);
    expect(status.textContent).toContain("STORE_OPEN_FAILED");
    expect(status.textContent).toContain("disk went away");
  });

  it("no records yet says so honestly", async () => {
    recentTasksMock.mockResolvedValue([]);
    render(<StartupTaskSection />);

    expect(await screen.findByText("No tasks recorded yet.")).not.toBeNull();
  });

  it("says it is reading while the backend has not answered", async () => {
    // Executor form: the tsconfig lib is ES2020, which has no
    // Promise.withResolvers types. The stub resolves a native promise
    // where the bindings type CancellablePromise; cast through never so
    // the test seam never imports the wails runtime.
    let resolveTasks!: (tasks: Task[]) => void;
    const deferred = new Promise<Task[]>((resolve) => {
      resolveTasks = resolve;
    });
    recentTasksMock.mockReturnValue(deferred as never);
    render(<StartupTaskSection />);
    expect(screen.getByText(/reading tasks/i)).not.toBeNull();

    resolveTasks([startupRecord]);
    expect(await screen.findByText(/startup complete at schema version 1/)).not.toBeNull();
  });

  it("backend refusal shows an alert reporting it", async () => {
    recentTasksMock.mockRejectedValue(new Error("connection refused"));
    render(<StartupTaskSection />);

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("connection refused");
  });
});
