import { useEffect, useState } from "react";

import { ApplicationService } from "../../bindings/github.com/Zendevve/astradew/internal/app";
import type { Task } from "../../bindings/github.com/Zendevve/astradew/internal/tasks/models";
import StatusText from "../ui/StatusText";

/** Fetch state of the durable startup task record. Idle until the effect runs. */
type StartupTaskState =
  | { status: "loading" }
  | { status: "ready"; tasks: Task[] }
  | { status: "failed"; message: string };

/**
 * StartupTaskSection renders the most recent durable "startup" task
 * record: operation, status, message, and timestamps, re-read from SQLite
 * on mount (ADR 0006: rows are the source of truth, events are live hints
 * never replayed). It invents nothing — with no record yet it says so,
 * and a failed record shows its durably recorded code as text (the code
 * is data returned by RecentTasks, not a call rejection, so it renders as
 * data). The shell stays responsive: a pure re-read with no blocking
 * waits.
 */
export default function StartupTaskSection() {
  const [state, setState] = useState<StartupTaskState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;
    ApplicationService.RecentTasks().then(
      (tasks) => {
        if (!cancelled) {
          setState({ status: "ready", tasks: tasks ?? [] });
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
    return (
      <section className="startup-task" aria-label="Startup task">
        <h2>Startup task</h2>
        <StatusText icon="○">Reading tasks…</StatusText>
      </section>
    );
  }
  if (state.status === "failed") {
    return (
      <section className="startup-task" aria-label="Startup task">
        <h2>Startup task</h2>
        <p role="alert" className="identity-error">
          Startup task is not available: {state.message}
        </p>
      </section>
    );
  }
  const record = state.tasks.find((task) => task.operation === "startup");
  if (record === undefined) {
    return (
      <section className="startup-task" aria-label="Startup task">
        <h2>Startup task</h2>
        <StatusText icon="○">No tasks recorded yet.</StatusText>
      </section>
    );
  }
  return (
    <section className="startup-task" aria-label="Startup task">
      <h2>Startup task</h2>
      {record.status === "failed" ? (
        <StatusText icon="●">
          Startup task failed with {record.outcome ?? "unknown error"}:{" "}
          {record.message}
        </StatusText>
      ) : (
        <StatusText icon="●">
          Startup task {record.status}
          {record.message !== "" ? `: ${record.message}` : ""}
          {record.outcome !== null &&
          record.outcome !== undefined &&
          record.outcome !== ""
            ? ` — ${record.outcome}`
            : ""}
        </StatusText>
      )}
      <p>
        Operation {record.operation}: recorded {record.createdAt}, updated{" "}
        {record.updatedAt}.
      </p>
    </section>
  );
}
