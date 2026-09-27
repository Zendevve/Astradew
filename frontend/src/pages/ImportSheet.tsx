import { useEffect, useRef, useState } from "react";
import { Dialogs, Events } from "@wailsio/runtime";

import {
  ApplicationService,
  type InspectionView,
  type Preview,
} from "../../bindings/github.com/Zendevve/astradew/internal/app";
import {
  FindingKind,
  Kind,
  type DuplicateGroup,
  type Finding,
  type Unit,
} from "../../bindings/github.com/Zendevve/astradew/internal/inspect";
import { Stage, Verdict } from "../../bindings/github.com/Zendevve/astradew/internal/manifest/models";
import { appErrorCode, appErrorPayload } from "../errors";
import Button from "../ui/Button";
import Sheet from "../ui/Sheet";
import StatusText from "../ui/StatusText";

/**
 * The five task event names the backend emits as live hints (issue #55's
 * "API, tasks, progress, cancellation"). They carry TaskEventPayload; the
 * durable row stays the truth, so a terminal event only makes this sheet
 * re-read Inspection.
 */
const taskEventNames = [
  "task.started",
  "task.progress",
  "task.completed",
  "task.failed",
  "task.cancelled",
] as const;

/** The payload every task event carries, as internal/app.TaskEvent sends it. */
interface TaskEventPayload {
  taskId?: string;
  operation?: string;
  current?: number;
  total?: number;
  message?: string;
}

/** A refusal to render: the typed code when there is one, plus the backend's own words. */
interface Failure {
  code: string | null;
  message: string;
  details: string;
}

/**
 * What the sheet shows: the five states of issue #55 (idle, inspecting,
 * preview, failure, expired), a cancelled note, and the transient reading and
 * cancelling moments.
 */
type SheetState =
  | { status: "idle" }
  | { status: "reading" }
  | { status: "inspecting"; taskID: string; current: number; total: number; message: string }
  | { status: "preview"; preview: Preview }
  | { status: "failed"; failure: Failure }
  | { status: "cancelled" }
  | { status: "expired" }
  | { status: "cancelling"; taskID: string };

/** Which task the sheet was opened for: none (choose a file) or an existing inspection. */
export type SheetTarget =
  | { kind: "idle" }
  | { kind: "task"; taskID: string; live: boolean };

/**
 * ImportSheet follows one inspection: it re-reads Inspection on mount so the
 * row stays authoritative, takes live progress from the task events, and
 * re-reads again when a terminal event arrives. Closing it during a run
 * cancels that run and waits for the cancellation to settle before closing,
 * so a closed sheet never leaves an inspection behind. A cancellation the
 * backend refuses means nothing is live here to stop, so the sheet stays open
 * and re-reads the row — the refusal, or the row's own answer, is shown
 * rather than closed over.
 */
export default function ImportSheet({
  target,
  onClose,
  onChanged,
}: {
  target: SheetTarget;
  onClose: () => void;
  /** Called whenever this sheet learns the watched run has ended. */
  onChanged: () => void;
}) {
  const [state, setState] = useState<SheetState>(() =>
    target.kind === "idle" ? { status: "idle" } : { status: "reading" },
  );
  const [choosing, setChoosing] = useState(false);
  const stateRef = useRef(state);
  const taskIDRef = useRef<string | null>(target.kind === "task" ? target.taskID : null);

  useEffect(() => {
    stateRef.current = state;
  }, [state]);

  /** Show a view, and tell the section when the run it was watching has ended. */
  const applyView = (view: InspectionView, taskID: string) => {
    const next = viewState(view, taskID);
    setState(next);
    if (next.status !== "inspecting" && next.status !== "reading") {
      onChanged();
    }
  };

  /**
   * Read one task's view and render it: the single read-then-apply path the
   * mount re-read, the terminal-event re-read, and a refused cancellation all
   * share, so they cannot drift.
   *
   * `wanted` reports whether this read is still the one that should decide
   * what the sheet shows — a read whose effect has been torn down, or whose
   * task subscription has gone, must not land. `unreadable` decides what a
   * failed read shows; the default is the rejection state, and a refused
   * cancellation passes the refusal itself, because when the row cannot
   * answer either, that refusal is all there is left to say.
   */
  const readView = (
    taskID: string,
    wanted: () => boolean,
    unreadable: (error: unknown) => void = (error) => setState(rejectionState(error)),
  ) => {
    ApplicationService.Inspection(taskID).then(
      (view) => {
        if (wanted()) {
          applyView(view, taskID);
        }
      },
      (error: unknown) => {
        if (wanted()) {
          unreadable(error);
        }
      },
    );
  };

  useEffect(() => {
    if (target.kind !== "task") {
      return;
    }
    const taskID = target.taskID;
    let cancelled = false;
    readView(taskID, () => !cancelled);
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target]);

  useEffect(() => {
    let mounted = true;
    const unregister = taskEventNames.map((name) =>
      Events.On(name, (event) => {
        const payload = event.data as TaskEventPayload | null | undefined;
        if (payload === null || payload === undefined) {
          return;
        }
        const taskID = payload.taskId;
        if (typeof taskID !== "string" || taskID !== taskIDRef.current) {
          return;
        }
        const current = stateRef.current;
        if (current.status === "cancelling") {
          // The close that started the cancellation owns what happens next.
          return;
        }
        if (
          current.status === "inspecting" &&
          (name === "task.started" || name === "task.progress")
        ) {
          setState({
            status: "inspecting",
            taskID: current.taskID,
            current:
              typeof payload.current === "number" ? payload.current : current.current,
            total: typeof payload.total === "number" ? payload.total : current.total,
            message:
              typeof payload.message === "string" ? payload.message : current.message,
          });
          return;
        }
        if (name === "task.completed" || name === "task.failed" || name === "task.cancelled") {
          readView(taskID, () => mounted);
        }
      }),
    );
    return () => {
      mounted = false;
      for (const off of unregister) {
        off();
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const requestClose = () => {
    const current = stateRef.current;
    if (current.status === "cancelling") {
      return;
    }
    // Closing during a run cancels the run and waits for its stage to go, so
    // the sheet disappears only once nothing is left running behind it. A row
    // still being read is treated the same way when the section saw it live:
    // the read only has to say what the row already said.
    const watchingRun =
      current.status === "inspecting" ||
      (current.status === "reading" && target.kind === "task" && target.live);
    const taskID =
      current.status === "inspecting"
        ? current.taskID
        : target.kind === "task"
          ? target.taskID
          : null;
    if (!watchingRun || taskID === null) {
      onClose();
      return;
    }
    setState({ status: "cancelling", taskID });
    ApplicationService.CancelTask(taskID).then(
      () => {
        onChanged();
        onClose();
      },
      (error: unknown) => {
        // Refused: this process holds no run to stop, so the row — not this
        // sheet — knows how the run ended. Re-read it and show that, and when
        // even the row cannot be read, show the refusal itself. Closing over
        // a refusal would hide why nothing was cancelled.
        onChanged();
        readView(taskID, () => true, () => setState(rejectionState(error)));
      },
    );
  };

  const chooseFile = () => {
    setChoosing(true);
    Dialogs.OpenFile({
      Title: "Choose a mod archive to inspect",
      ButtonText: "Inspect",
      Filters: [{ DisplayName: "Zip archives", Pattern: "*.zip" }],
    }).then(
      (choice) => {
        const path = typeof choice === "string" ? choice : "";
        if (path === "") {
          // Cancelling the dialog does nothing at all: no task, no staging,
          // no error, and the sheet stays where it was.
          setChoosing(false);
          return;
        }
        ApplicationService.InspectArchive(path).then(
          (row) => {
            taskIDRef.current = row.id;
            setChoosing(false);
            setState({
              status: "inspecting",
              taskID: row.id,
              current: row.current,
              total: row.total,
              message: row.message,
            });
            onChanged();
          },
          (error: unknown) => {
            setChoosing(false);
            setState(rejectionState(error));
          },
        );
      },
      (error: unknown) => {
        setChoosing(false);
        setState(rejectionState(error));
      },
    );
  };

  return (
    <Sheet
      title="Import archive"
      onClose={requestClose}
      closeDisabled={state.status === "cancelling"}
    >
      <SheetBody
        state={state}
        choosing={choosing}
        onChoose={chooseFile}
        onClose={requestClose}
      />
    </Sheet>
  );
}

/** One state of the import sheet. Every state ends in a way the user can act on. */
function SheetBody({
  state,
  choosing,
  onChoose,
  onClose,
}: {
  state: SheetState;
  choosing: boolean;
  onChoose: () => void;
  onClose: () => void;
}) {
  switch (state.status) {
    case "idle":
      return (
        <>
          <p>
            Choose a mod archive and Astradew will report what it holds: every
            Mod Unit, its identity, version, kind and dependencies, and anything
            that would need attention. Nothing is installed.
          </p>
          <Button onClick={onChoose} disabled={choosing}>
            {choosing ? "Choosing…" : "Choose .zip archive…"}
          </Button>
          <p className="sheet-hint">
            Only <code>.zip</code> archives can be inspected in this phase.
          </p>
        </>
      );
    case "reading":
      return <StatusText icon="○">Reading the inspection…</StatusText>;
    case "inspecting":
      return <InspectingBody state={state} onCancel={onClose} />;
    case "cancelling":
      return (
        <StatusText icon="○">
          Cancelling the inspection and removing its temporary files…
        </StatusText>
      );
    case "preview":
      return <PreviewBody preview={state.preview} />;
    case "failed":
      return <FailureBody failure={state.failure} />;
    case "cancelled":
      return (
        <StatusText icon="●">
          This inspection was cancelled. Nothing was installed, and its
          temporary files were removed.
        </StatusText>
      );
    case "expired":
      return (
        <StatusText icon="●">
          This inspection is from an earlier run of Astradew, so its result is
          no longer held. Nothing was installed: an inspection only reports, and
          nothing about a preview is kept across restarts.
        </StatusText>
      );
    default:
      return null;
  }
}

/** The live run: phase message, a determinate bar while hashing or extracting, an indeterminate one while scanning, and the cancel action. */
function InspectingBody({
  state,
  onCancel,
}: {
  state: Extract<SheetState, { status: "inspecting" }>;
  onCancel: () => void;
}) {
  const determinate = state.total > 0;
  const percent = determinate
    ? Math.min(100, Math.round((state.current / state.total) * 100))
    : 0;
  return (
    <>
      <StatusText icon="○">
        {state.message !== "" ? state.message : "The inspection is running."}
      </StatusText>
      <progress
        className="sheet-progress"
        aria-label="Inspection progress"
        max={100}
        value={determinate ? percent : undefined}
      />
      <p className="sheet-hint">
        {determinate
          ? `${formatBytes(state.current)} of ${formatBytes(state.total)} — ${percent}%`
          : "This phase reports no progress numbers, so the bar stays indeterminate."}
      </p>
      <p className="sheet-hint">
        The archive is read in place and staged in a temporary folder; nothing
        reaches your Library.
      </p>
      <Button onClick={onCancel}>Cancel inspection</Button>
    </>
  );
}

/** The install preview: archive identity, units, findings, duplicates, and the honest inspect-only note. */
function PreviewBody({ preview }: { preview: Preview }) {
  const units = preview.units ?? [];
  const findings = preview.findings ?? [];
  const duplicates = preview.duplicates ?? [];
  return (
    <>
      <StatusText icon="✓">Nothing is installed yet — inspect only.</StatusText>
      <section className="sheet-section" aria-label="Archive identity">
        <h3>Archive</h3>
        <dl className="sheet-facts">
          <div>
            <dt>File</dt>
            <dd>{preview.originalName}</dd>
          </div>
          <div>
            <dt>Size</dt>
            <dd>
              {formatBytes(preview.sizeBytes)} (
              {preview.sizeBytes.toLocaleString("en-US")} bytes)
            </dd>
          </div>
          <div>
            <dt>Archive SHA-256</dt>
            <dd className="hash">{preview.archiveSha256}</dd>
          </div>
          <div>
            <dt>Package SHA-256</dt>
            <dd className="hash">{preview.packageSha256}</dd>
          </div>
          <div>
            <dt>Source</dt>
            <dd className="hash">{preview.sourcePath}</dd>
          </div>
        </dl>
      </section>
      <section className="sheet-section" aria-label="Mod units">
        <h3>Mod units ({units.length})</h3>
        {units.length === 0 ? (
          <p>This package holds no Mod Unit.</p>
        ) : (
          units.map((unit, index) => (
            <UnitCard key={`${unit.relativePath}-${unit.uniqueID}-${index}`} unit={unit} />
          ))
        )}
      </section>
      {findings.length > 0 && (
        <section className="sheet-section" aria-label="Findings">
          <h3>Findings ({findings.length})</h3>
          <ul className="finding-list">
            {findings.map((finding, index) => (
              <FindingItem
                key={`${finding.kind}-${finding.path}-${index}`}
                finding={finding}
              />
            ))}
          </ul>
        </section>
      )}
      {duplicates.length > 0 && (
        <section className="sheet-section" aria-label="Duplicate Unique IDs">
          <h3>Duplicate Unique IDs ({duplicates.length})</h3>
          <ul className="duplicate-list">
            {duplicates.map((group) => (
              <DuplicateItem key={group.uniqueID} group={group} />
            ))}
          </ul>
        </section>
      )}
      <p className="sheet-outcome">
        {preview.installable
          ? "Astradew reports this package as installable: at least one Mod Unit here can be installed."
          : "Astradew reports this package as not installable: no Mod Unit here can be installed as it stands."}
      </p>
      <p className="sheet-hint">
        Inspecting changed nothing: the source archive is untouched, the
        temporary staging folder is gone, and your Library, Profiles and game
        folder are exactly as they were.
      </p>
    </>
  );
}

/** One Mod Unit: identity, version, kind, entry point, dependencies, verdict, and its field problems. */
function UnitCard({ unit }: { unit: Unit }) {
  const dependencies = unit.dependencies ?? [];
  const fieldErrors = unit.fieldErrors ?? [];
  const name = unit.name !== "" ? unit.name : unit.folderName;
  return (
    <article className="mod-unit" aria-label={`Mod Unit ${name}`}>
      <h4>{name}</h4>
      <p className="mod-unit-kind">
        {kindLabel(unit.kind)}
        {unit.entryDll !== "" ? ` · entry ${unit.entryDll}` : ""}
        {unit.contentPackFor !== "" ? ` · content pack for ${unit.contentPackFor}` : ""}
        {unit.systemMod ? " · ships with SMAPI" : ""}
      </p>
      <StatusText icon={unit.installable ? "✓" : "✕"}>
        {unit.installable ? "Installable" : "Not installable"}
        {unit.note !== "" ? `: ${unit.note}` : ""}
      </StatusText>
      <dl className="sheet-facts">
        <div>
          <dt>Version</dt>
          <dd>{unit.version !== "" ? unit.version : "not declared"}</dd>
        </div>
        <div>
          <dt>Unique ID</dt>
          <dd>{unit.uniqueID !== "" ? unit.uniqueID : "not declared"}</dd>
        </div>
        <div>
          <dt>Author</dt>
          <dd>{unit.author !== "" ? unit.author : "not declared"}</dd>
        </div>
        <div>
          <dt>Folder</dt>
          <dd>{unit.relativePath !== "" ? unit.relativePath : "the package root"}</dd>
        </div>
        <div>
          <dt>Manifest</dt>
          <dd>{verdictLabel(unit.verdict)}</dd>
        </div>
      </dl>
      {dependencies.length > 0 && (
        <>
          <h5>Dependencies</h5>
          <ul className="dependency-list">
            {dependencies.map((dependency, index) => (
              <li key={`${dependency.uniqueID}-${index}`}>
                {dependency.uniqueID}
                {dependency.minimumVersion !== ""
                  ? ` — ${dependency.minimumVersion} or newer`
                  : ""}
                {dependency.isRequired ? " (required)" : " (optional)"}
              </li>
            ))}
          </ul>
        </>
      )}
      {fieldErrors.length > 0 && (
        <>
          <h5>Manifest problems</h5>
          <ul className="field-error-list">
            {fieldErrors.map((error, index) => (
              <li key={`${error.field}-${index}`}>
                {error.field} ({stageLabel(error.stage)}): {error.message}
              </li>
            ))}
          </ul>
        </>
      )}
    </article>
  );
}

/** One layout finding: what it is about, and the backend's own message verbatim. */
function FindingItem({ finding }: { finding: Finding }) {
  return (
    <li>
      <p className="finding-kind">
        {findingKindLabel(finding.kind)}
        {finding.path !== "" ? ` — ${finding.path}` : ""}
      </p>
      <p className="finding-message">{finding.message}</p>
    </li>
  );
}

/** One duplicate Unique ID group: the entries claiming it and whether it blocks launch. */
function DuplicateItem({ group }: { group: DuplicateGroup }) {
  const entries = group.entries ?? [];
  return (
    <li>
      <p className="duplicate-id">
        {group.uniqueID}
        {group.launchBlocker
          ? " — SMAPI cannot tell these copies apart once loaded, so this blocks launch."
          : ""}
      </p>
      <ul>
        {entries.map((entry, index) => (
          <li key={`${entry.path}-${index}`}>
            {entry.path}
            {entry.version !== "" ? ` (version ${entry.version})` : ""}
          </li>
        ))}
      </ul>
    </li>
  );
}

/** The failure of a run or of a refused call: the code's own copy, then the backend's details. */
function FailureBody({ failure }: { failure: Failure }) {
  const details = failure.details !== "" ? failure.details : failure.message;
  return (
    <>
      <p role="alert" className="sheet-alert">
        {failureCopy(failure.code)}
      </p>
      {details !== "" && <p className="sheet-details">Details: {details}</p>}
      <p className="sheet-code">Refusal code: {failure.code ?? "none"}</p>
    </>
  );
}

/**
 * Refusal copy per code: what happened, what it means, and the recovery. Each
 * code the inspection API can return has exactly one treatment:
 * INSPECTION_NOT_FOUND renders as the sheet's expired state (rejectionState
 * routes it there), so it is deliberately absent here rather than given a
 * second, divergent sentence.
 */
function failureCopy(code: string | null): string {
  switch (code) {
    case "ARCHIVE_UNREADABLE":
      return "Astradew could not read the chosen file. It may have been moved, deleted, or changed while it was being inspected. Check the file and choose it again.";
    case "ARCHIVE_PATH_TRAVERSAL":
      return "This archive is malformed or hostile: it holds an entry that would land outside the inspection's temporary folder. Astradew refused the whole archive and wrote nothing. Get a fresh copy from a source you trust.";
    case "ARCHIVE_LINK_ENTRY":
      return "This archive holds a symbolic link entry. Astradew never creates links, so it refused the whole archive. Get a clean copy.";
    case "ARCHIVE_ENCRYPTED":
      return "This archive holds an encrypted (password-protected) entry, which cannot be inspected. Use an unencrypted copy of the mod.";
    case "ARCHIVE_NAME_INVALID":
      return "This archive holds a name this computer cannot represent: a reserved device name, an illegal character, a trailing dot or space, or two entries differing only by letter case. Astradew never renames entries, so it refused the archive. Get a clean copy.";
    case "ARCHIVE_LIMIT_EXCEEDED":
      return "This archive breaks one of the archive safety limits — size, expanded size, entry count, compression ratio, path depth, or path length. If you trust the archive, raise that limit in Settings; otherwise get a smaller copy.";
    case "ARCHIVE_CORRUPT":
      return "This archive is not a readable ZIP: its structure is damaged. Re-download the file and try again.";
    case "INSPECTION_FAILED":
      return "The inspection could not write its temporary files. Free up disk space or fix permissions on the temporary folder, then try again.";
    case "INSPECTION_BUSY":
      return "Another inspection is already running. Wait for it to finish or cancel it, then inspect this archive.";
    case "TASK_NOT_CANCELLABLE":
      return "This inspection is not running in this app session any more, so there was nothing to cancel. Its recorded row says how it ended.";
    case "TASK_NOT_FOUND":
      return "That task has no record; it may have been cleared. Start a new inspection.";
    case "STORE_OPEN_FAILED":
      return "Astradew's database is not open, so no inspection can be recorded. Restart the app; if this keeps happening, check the Health view.";
    default:
      return "The inspection failed, and the failure carried no typed code. The details below are what the backend reported.";
  }
}

/** Map one inspection view onto the sheet's state. */
function viewState(view: InspectionView, taskID: string): SheetState {
  switch (view.status) {
    case "succeeded":
      if (view.preview !== null) {
        return { status: "preview", preview: view.preview };
      }
      return {
        status: "failed",
        failure: {
          code: null,
          message: "The inspection finished without a preview.",
          details: `task ${taskID} is ${view.task.status}, but its preview is no longer held`,
        },
      };
    case "failed":
      if (view.failure !== null) {
        return { status: "failed", failure: view.failure };
      }
      return {
        status: "failed",
        failure: {
          code: null,
          message: "The inspection failed without a typed failure.",
          details: `task ${taskID} is ${view.task.status}: ${view.task.outcome ?? ""}`,
        },
      };
    case "cancelled":
      return { status: "cancelled" };
    default:
      return {
        status: "inspecting",
        taskID,
        current: view.task.current,
        total: view.task.total,
        message: view.task.message,
      };
  }
}

/** Turn a call rejection into the sheet's state: expiry for a lost inspection, a failure otherwise. */
function rejectionState(error: unknown): SheetState {
  const code = appErrorCode(error);
  if (code === "INSPECTION_NOT_FOUND") {
    return { status: "expired" };
  }
  const payload = appErrorPayload(error);
  return {
    status: "failed",
    failure: {
      code,
      message: payload?.message ?? (error instanceof Error ? error.message : String(error)),
      details: payload?.details ?? "",
    },
  };
}

/** Human label for a Mod Unit kind. */
function kindLabel(kind: Kind): string {
  switch (kind) {
    case Kind.KindCodeMod:
      return "Code mod";
    case Kind.KindContentPack:
      return "Content pack";
    case Kind.KindInvalid:
      return "Invalid manifest";
    default:
      return kind;
  }
}

/** Human label for a manifest verdict. */
function verdictLabel(verdict: Verdict): string {
  switch (verdict) {
    case Verdict.VerdictValid:
      return "valid";
    case Verdict.VerdictPartial:
      return "partial — installed with warnings";
    case Verdict.VerdictInvalid:
      return "invalid — not loadable";
    default:
      return verdict;
  }
}

/** Human label for the stage that produced a field error. */
function stageLabel(stage: Stage): string {
  switch (stage) {
    case Stage.StageSyntax:
      return "syntax";
    case Stage.StageInterpretation:
      return "interpretation";
    case Stage.StageValidation:
      return "validation";
    default:
      return stage;
  }
}

/** Human label for a layout finding kind. */
function findingKindLabel(kind: FindingKind): string {
  switch (kind) {
    case FindingKind.FindingLegacyXnb:
      return "Legacy XNB content";
    case FindingKind.FindingRootContentTree:
      return "Content folder instead of a mod";
    case FindingKind.FindingSMAPIInstaller:
      return "SMAPI installer bundle";
    case FindingKind.FindingSMAPIPayload:
      return "SMAPI installer payload";
    case FindingKind.FindingHiddenManifests:
      return "Hidden manifests";
    case FindingKind.FindingEmptyFolder:
      return "Empty folder";
    case FindingKind.FindingIgnoredFolder:
      return "Folder SMAPI skips";
    case FindingKind.FindingVortexLeftover:
      return "Leftover from another manager";
    case FindingKind.FindingLooseRootFiles:
      return "Loose files at the root";
    case FindingKind.FindingUnreadable:
      return "Unreadable";
    case FindingKind.FindingScanLimit:
      return "Scan stopped early";
    default:
      return kind;
  }
}

/** A byte count in the largest unit that keeps it readable. */
function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 1024) {
    return `${bytes} B`;
  }
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
}
