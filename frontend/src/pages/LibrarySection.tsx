import { useEffect, useId, useState } from "react";

import { ApplicationService } from "../../bindings/github.com/Zendevve/astradew/internal/app";
import { Status, type Task } from "../../bindings/github.com/Zendevve/astradew/internal/tasks/models";
import { appErrorCode } from "../errors";
import Button from "../ui/Button";
import StatusText from "../ui/StatusText";
import ImportSheet, { type SheetTarget } from "./ImportSheet";

/** Fetch state of the recent task rows this section reads. */
type RowsState = { status: "loading" } | { status: "ready" } | { status: "failed" };

/**
 * The newest archive.inspect row, or nothing when the records hold none: the
 * rows come back newest first, so the first match is the one that decides
 * what this section follows (ADR 0006: the row is the truth).
 */
function newestInspection(records: Task[] | null): Task | null {
  return (records ?? []).find((row) => row.operation === "archive.inspect") ?? null;
}

/**
 * True while a row has not finished. Pending and running are the two states
 * that may still have a run behind them, and the only ones worth asking the
 * process about: a row on its own cannot tell a live run from one this
 * process no longer has, which is what an app that exited during an
 * inspection leaves behind.
 */
function isRunning(row: Task): boolean {
  return row.status === Status.StatusPending || row.status === Status.StatusRunning;
}

/**
 * What one read of the newest inspection says about the action. "idle" covers
 * every case where nothing of ours is live here: no newest row, a row that
 * finished, and a pending or running row left behind by an earlier app run —
 * a row the backend would happily inspect again.
 */
type NewestRead =
  | { status: "rows-failed" }
  | { status: "idle"; newest: Task | null }
  | { status: "live"; newest: Task }
  | { status: "unreadable"; newest: Task; code: string | null };

/** The action's state: one read's answer, or the moment before it arrives. */
type Liveness = { status: "checking" } | NewestRead;

/**
 * Read the newest inspection, and ask the process whether its run is live.
 *
 * Liveness is an in-process fact, so the row cannot answer it — Inspection
 * does, and its answer is authoritative either way. A view that reads running
 * means this app is still running the inspection; INSPECTION_NOT_FOUND means
 * the row is stale, so the action is available again; any other refusal is
 * the backend failing to answer at all, which is its own state and its own
 * reason. Asking here, and not also in the render, is what keeps the action's
 * state and the sheet's target from ever disagreeing.
 */
async function readNewest(): Promise<NewestRead> {
  let records: Task[] | null;
  try {
    records = await ApplicationService.RecentTasks();
  } catch {
    return { status: "rows-failed" };
  }
  const newest = newestInspection(records);
  if (newest === null || !isRunning(newest)) {
    return { status: "idle", newest };
  }
  try {
    const view = await ApplicationService.Inspection(newest.id);
    return view.status === "running"
      ? { status: "live", newest }
      : { status: "idle", newest };
  } catch (error: unknown) {
    const code = appErrorCode(error);
    return code === "INSPECTION_NOT_FOUND"
      ? { status: "idle", newest }
      : { status: "unreadable", newest, code };
  }
}

/** The action's disabled reason, or null while Import is available. */
function importReason(rows: RowsState, liveness: Liveness): string | null {
  if (rows.status === "failed") {
    return "Import is unavailable: the backend did not answer.";
  }
  if (rows.status === "loading") {
    return "Import is unavailable until the task records are read.";
  }
  switch (liveness.status) {
    case "checking":
      return "Import is unavailable until the running inspection is confirmed.";
    case "live":
      return "Import is unavailable: an inspection is already running.";
    case "unreadable":
      return `Import is unavailable: the running inspection could not be read${
        liveness.code === null ? "" : ` (${liveness.code})`
      }.`;
    default:
      return null;
  }
}

/**
 * LibrarySection renders the Library route's import surface. The route's
 * honest empty copy stays above it; this section owns the import action and
 * the import sheet. The action is disabled with a visible reason while one
 * inspection is live or the backend did not answer — the same
 * disabled-plus-control-reason pattern the shell uses.
 *
 * On mount it re-reads RecentTasks and reopens the newest archive.inspect row
 * (ADR 0006: the row is the truth). A running row reopens as the inspecting
 * state with live events subscribed; a finished row reopens with its
 * in-memory preview or failure payload when Inspection still holds one, and as
 * the honest expired state when it does not, because nothing about a preview
 * is persisted.
 *
 * The row is the truth about what the sheet shows, but not about liveness: a
 * row left running by an earlier app run has no run behind it, and only this
 * process can tell the two apart. So a pending or running row is settled by
 * asking the process (Inspection), and that single answer decides both the
 * action's state and what the sheet reopens as. The sheet's own re-read then
 * renders whatever the row holds — expired included — and onChanged repeats
 * this read, so the action follows whatever the run it was watching did.
 */
export default function LibrarySection() {
  const reasonId = useId();
  const [rows, setRows] = useState<RowsState>({ status: "loading" });
  const [liveness, setLiveness] = useState<Liveness>({ status: "checking" });
  const [sheet, setSheet] = useState<SheetTarget | null>(null);

  /**
   * Apply one read: the rows' state, the action's state, and — on the mount
   * read — what the sheet reopens as. The sheet opens from the same answer
   * that decides the action, so a disabled action and a live sheet can never
   * come from two different reads. A sheet that is already open owns what it
   * shows, so a refresh from onChanged leaves it alone.
   */
  const applyRead = (read: NewestRead, reload: boolean) => {
    setRows(read.status === "rows-failed" ? { status: "failed" } : { status: "ready" });
    setLiveness(read);
    if (reload && "newest" in read && read.newest !== null) {
      setSheet({
        kind: "task",
        taskID: read.newest.id,
        live: read.status === "live",
      });
    }
  };

  useEffect(() => {
    let cancelled = false;
    readNewest().then((read) => {
      if (!cancelled) {
        applyRead(read, true);
      }
    });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const reason = importReason(rows, liveness);

  return (
    <section className="library-import" aria-label="Import an archive">
      <h2>Import</h2>
      <p>
        Astradew inspects a downloaded <code>.zip</code> archive without changing
        anything: it reads the archive, lists the Mod Units it holds, and reports
        what they would add. The source file is never modified, and the temporary
        staging folder is removed when the inspection ends.
      </p>
      <div className="control">
        <Button
          onClick={() => setSheet({ kind: "idle" })}
          disabled={reason !== null}
          aria-describedby={reason !== null ? reasonId : undefined}
        >
          Import archive…
        </Button>
        {reason !== null && (
          <p className="control-reason" id={reasonId}>
            {reason}
          </p>
        )}
      </div>
      {rows.status === "loading" && (
        <StatusText icon="○">Reading the last inspections…</StatusText>
      )}
      {sheet !== null && (
        <ImportSheet
          target={sheet}
          onClose={() => setSheet(null)}
          onChanged={() => {
            readNewest().then((read) => applyRead(read, false));
          }}
        />
      )}
    </section>
  );
}
