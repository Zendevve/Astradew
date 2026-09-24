import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import App from "../App";
import { ApplicationService, type Info } from "../../bindings/github.com/Zendevve/astradew/internal/app";
import { ROUTES } from "../pages/routes";

// Guarantees the bindings stub even if this file runs without the setup
// file's global mock; same factory, so behaviour is identical.
vi.mock("../../bindings/github.com/Zendevve/astradew/internal/app", () => ({
  ApplicationService: { Info: vi.fn(), ProbeFailure: vi.fn() },
}));

const infoMock = vi.mocked(ApplicationService.Info);
const probeMock = vi.mocked(ApplicationService.ProbeFailure);

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
