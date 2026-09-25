import { useEffect, useState } from "react";

import type { HealthReport } from "../../bindings/github.com/Zendevve/astradew/internal/app";
import { ApplicationService } from "../../bindings/github.com/Zendevve/astradew/internal/app";
import StatusText from "../ui/StatusText";

/** Fetch state of the backend health report. Idle until the effect runs. */
type HealthState =
  | { status: "loading" }
  | { status: "ready"; report: HealthReport }
  | { status: "failed"; message: string };


/**
 * HealthSection renders the Health view from the backend Health() report:
 * application identity, per-directory writability, live database state, the
 * observed game and SMAPI sections once detection succeeds, the recorded
 * startup steps, observed findings, and the not-yet-available capabilities.
 * It invents nothing — unobservable values appear under "Not yet available",
 * never as passes; a null game/smapi section renders nothing and the
 * corresponding unavailable entry keeps guiding.
 */
export default function HealthSection() {
  const [state, setState] = useState<HealthState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;
    ApplicationService.Health().then(
      (report) => {
        if (!cancelled) {
          setState({ status: "ready", report });
        }
      },
      (error: unknown) => {
        if (!cancelled) {
          setState({
            status: "failed",
            message: error instanceof Error ? error.message : String(error),
          });
        }
      },
    );
    return () => {
      cancelled = true;
    };
  }, []);

  if (state.status === "loading") {
    return <StatusText icon="○">Reading health…</StatusText>;
  }
  if (state.status === "failed") {
    return (
      <p role="alert" className="identity-error">
        Health is not available: {state.message}
      </p>
    );
  }
  const report = state.report;
  const directories = report.directories ?? [];
  const steps = report.initialisation ?? [];
  const findings = report.findings ?? [];
  const unavailable = report.unavailable ?? [];
  return (
    <div className="health-report">
      <section className="health-application" aria-label="Application">
        <h2>Application</h2>
        <p>
          {report.name} Version {report.version}
        </p>
        <p>Data root: {report.dataRoot}</p>
      </section>
      <section className="health-directories" aria-label="Data locations">
        <h2>Data locations</h2>
        <dl>
          {directories.map((dir) => (
            <div className="data-path" key={dir.name}>
              <dt>{dir.name}</dt>
              <dd>
                {dir.path}{" "}
                {dir.writable ? (
                  <StatusText icon="●">Writable.</StatusText>
                ) : (
                  <StatusText icon="●">Not writable.</StatusText>
                )}
              </dd>
            </div>
          ))}
        </dl>
      </section>
      <section className="health-database" aria-label="Database">
        <h2>Database</h2>
        {report.database.healthy ? (
          <StatusText icon="●">
            Open at schema version {report.database.version}:{" "}
            {report.database.path}
          </StatusText>
        ) : (
          <StatusText icon="●">
            {report.database.state}
            {report.database.path !== "" ? `: ${report.database.path}` : ""}
          </StatusText>
        )}
      </section>
      <section className="health-initialisation" aria-label="Initialisation">
        <h2>Initialisation</h2>
        {steps.length === 0 ? (
          <StatusText icon="○">
            No startup steps recorded: the backend did not report any.
          </StatusText>
        ) : (
          <ul>
            {steps.map((step) => (
              <li key={step.name}>
                {step.name}: {step.ok ? "done" : "not done"} — {step.message}
              </li>
            ))}
          </ul>
        )}
      </section>
      {report.game != null ? (
        <section className="health-game" aria-label="Game">
          <h2>Game</h2>
          <p>Path: {report.game.path}</p>
          <p>Source: {report.game.source}</p>
          <p>Game version: {report.game.gameVersion ?? "version unknown"}</p>
          <p>Installs known: {report.game.installsKnown}</p>
        </section>
      ) : null}
      {report.smapi != null ? (
        <section className="health-smapi" aria-label="SMAPI">
          <h2>SMAPI</h2>
          <p>State: {report.smapi.state}</p>
          <p>Version: {report.smapi.version ?? "version unknown"}</p>
          <p>{report.smapi.detail}</p>
          {(report.smapi.missing ?? []).length > 0 ? (
            <ul>
              {(report.smapi.missing ?? []).map((name) => (
                <li key={name}>Missing: {name}</li>
              ))}
            </ul>
          ) : null}
        </section>
      ) : null}
      {findings.length > 0 ? (
        <section className="health-findings" aria-label="Findings">
          <h2>Findings</h2>
          <ul>
            {findings.map((finding, index) => (
              <li key={`${finding.severity}-${index}`}>
                <p>
                  [{finding.severity}] {finding.what}
                </p>
                <p>Why it matters: {finding.why}</p>
                <p>What to do: {finding.action}</p>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
      <section className="health-unavailable" aria-label="Not yet available">
        <h2>Not yet available</h2>
        <ul>
          {unavailable.map((entry) => (
            <li key={entry.name}>
              {entry.name}: {entry.status} — {entry.reason}
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
