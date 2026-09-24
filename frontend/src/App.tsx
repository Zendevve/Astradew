import { useEffect, useState } from "react";

import {
  ApplicationService,
  type Info,
} from "../bindings/github.com/Zendevve/astradew/internal/app";
import { appErrorCode, appErrorPayload } from "./errors";
import Button from "./ui/Button";
import NavLink from "./ui/NavLink";
import EmptyState from "./ui/EmptyState";
import StatusText from "./ui/StatusText";
import HealthSection from "./pages/HealthSection";
import { ROUTES } from "./pages/routes";

/** State of the failure-path demo probe. Idle until the user runs it. */
type ProbeState =
  | { status: "idle" }
  | { status: "running" }
  | { status: "failed"; code: string; message: string }
  | { status: "unexpected"; message: string };

/**
 * App is the application shell: backend identity line, top bar, primary nav,
 * and one honest empty-state page per route. It invents nothing — with no
 * game configured every action is present but disabled with its reason, and
 * every page states what is missing and what will fill it. Routing is a hash
 * listener, so no router dependency is needed.
 */
export default function App() {
  const [identity, setIdentity] = useState<Info | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [path, setPath] = useState<string>(readPath);
  const [probe, setProbe] = useState<ProbeState>({ status: "idle" });

  const runProbe = () => {
    setProbe({ status: "running" });
    ApplicationService.ProbeFailure().then(
      () => {
        setProbe({
          status: "unexpected",
          message: "Probe unexpectedly succeeded.",
        });
      },
      (error: unknown) => {
        const code = appErrorCode(error);
        const payload = appErrorPayload(error);
        const message =
          payload?.message ??
          (error instanceof Error ? error.message : String(error));
        if (code === null) {
          setProbe({ status: "unexpected", message });
        } else {
          setProbe({ status: "failed", code, message });
        }
      },
    );
  };

  useEffect(() => {
    let cancelled = false;

    ApplicationService.Info().then(
      (info) => {
        if (!cancelled) {
          setIdentity(info);
        }
      },
      (error: unknown) => {
        if (!cancelled) {
          setFailure(error instanceof Error ? error.message : String(error));
        }
      },
    );

    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const onHashChange = () => {
      setPath(readPath());
    };
    window.addEventListener("hashchange", onHashChange);
    return () => {
      window.removeEventListener("hashchange", onHashChange);
    };
  }, []);

  const active = ROUTES.find((route) => route.path === path) ?? ROUTES[0];

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">
        Skip to main content
      </a>
      <header className="top-bar">
        <div className="identity">
          {failure !== null ? (
            <p role="alert" className="identity-error">
              The backend did not answer: {failure}
            </p>
          ) : identity === null ? (
            <p className="identity-loading">Reading application identity…</p>
          ) : (
            <p className="identity-line">
              <span className="identity-name">{identity.name}</span>{" "}
              <span className="identity-version">
                Version {identity.version}
              </span>
            </p>
          )}
        </div>
        <nav aria-label="Primary" className="primary-nav">
          <ul>
            {ROUTES.map((route) => (
              <li key={route.path}>
                <NavLink to={route.path} current={route.path === active.path}>
                  {route.label}
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>
        <div className="top-bar-controls">
          <div className="control">
            <label htmlFor="profile-select">Profile</label>
            <select
              id="profile-select"
              disabled
              aria-describedby="profile-reason"
            >
              <option>No profiles</option>
            </select>
            <p className="control-reason" id="profile-reason">
              Profiles are unavailable: no game is configured.
            </p>
          </div>
          <div className="control">
            <label htmlFor="top-search">Search</label>
            <input
              id="top-search"
              type="search"
              placeholder="Search"
              disabled
              aria-describedby="search-reason"
            />
            <p className="control-reason" id="search-reason">
              Search is unavailable: no game is configured.
            </p>
          </div>
          <StatusText icon="○">
            No updates: no game is configured.
          </StatusText>
          <div className="control">
            <Button disabled aria-describedby="launch-reason">
              Launch
            </Button>
            <p className="control-reason" id="launch-reason">
              Launch is unavailable: no game is configured.
            </p>
          </div>
        </div>
      </header>
      <main id="main-content">
        {active.path === "/health" ? (
          <>
            <EmptyState title={active.heading} description={active.empty} />
            <HealthSection />
          </>
        ) : (
          <EmptyState title={active.heading} description={active.empty} />
        )}
        <section className="app-paths" aria-label="Application data">
          <h2>Application data</h2>
          <StatusText icon="○">
            Locations moved to the Health view: open Health in the primary
            navigation to see every resolved path with its writability state.
          </StatusText>
        </section>
        <section className="diagnostics" aria-label="Diagnostics">
          <h2>Diagnostics</h2>
          <Button onClick={runProbe} disabled={probe.status === "running"}>
            Run failure probe
          </Button>
          {probe.status === "idle" ? (
            <StatusText icon="○">Failure probe not run.</StatusText>
          ) : probe.status === "running" ? (
            <StatusText icon="○">Running failure probe…</StatusText>
          ) : probe.status === "failed" && probe.code === "PROBE_FAILURE" ? (
            <StatusText icon="●">
              Probe failed with {probe.code}: {probe.message}
            </StatusText>
          ) : probe.status === "failed" ? (
            <StatusText icon="●">
              Probe failed with unexpected code {probe.code}: {probe.message}
            </StatusText>
          ) : (
            <StatusText icon="●">
              Probe result unexpected: {probe.message}
            </StatusText>
          )}
        </section>
      </main>
    </div>
  );
}

/** Current hash path if it names a route, otherwise the dashboard path. */
function readPath(): string {
  const hash = window.location.hash.replace(/^#/, "");
  for (const route of ROUTES) {
    if (route.path === hash) {
      return hash;
    }
  }
  return "/";
}
