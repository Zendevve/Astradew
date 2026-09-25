import { useEffect, useState } from "react";

import type {
  GameInstallView,
  SettingView,
} from "../../bindings/github.com/Zendevve/astradew/internal/app";
import { ApplicationService } from "../../bindings/github.com/Zendevve/astradew/internal/app";
import { appErrorCode, appErrorPayload } from "../errors";
import Button from "../ui/Button";
import StatusText from "../ui/StatusText";
/** Fetch state of the settings list. Idle until the effect runs. */
type SettingsState =
  | { status: "loading" }
  | { status: "ready"; settings: SettingView[] }
  | { status: "failed"; message: string };

/** Per-row save state: idle, saving, or a typed refusal to display. */
type RowState =
  | { status: "idle" }
  | { status: "saving" }
  | { status: "invalid"; code: string; message: string };

/**
 * SettingsSection renders the Settings view: the "Game installs" group
 * (resolved primary card, folder picker, installs list with explicit
 * choices) above the generic registry rows. It invents nothing — values
 * come only from the backend.
 */
export default function SettingsSection() {
  const [state, setState] = useState<SettingsState>({ status: "loading" });
  const [installs, setInstalls] = useState<GameInstallView[]>([]);
  const [installsFailed, setInstallsFailed] = useState<string | null>(null);

  const load = (isCancelled?: () => boolean) => {
    const cancelled = isCancelled ?? (() => false);
    setInstallsFailed(null);
    ApplicationService.Settings().then(
      (settings) => {
        if (!cancelled()) {
          setState({ status: "ready", settings: settings ?? [] });
        }
      },
      (error: unknown) => {
        if (!cancelled()) {
          setState({
            status: "failed",
            message: error instanceof Error ? error.message : String(error),
          });
        }
      },
    );
    ApplicationService.GameInstalls().then(
      (rows) => {
        if (!cancelled()) {
          setInstalls(rows ?? []);
        }
      },
      (error: unknown) => {
        if (!cancelled()) {
          setInstallsFailed(error instanceof Error ? error.message : String(error));
        }
      },
    );
  };

  useEffect(() => {
    let cancelled = false;
    load(() => cancelled);
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (state.status === "loading") {
    return <StatusText icon="○">Reading settings…</StatusText>;
  }
  if (state.status === "failed") {
    return (
      <p role="alert" className="identity-error">
        Settings are not available: {state.message}
      </p>
    );
  }
  const primary = installs.find((row) => row.isPrimary) ?? null;
  return (
    <>
      <GameInstallsGroup
        installs={installs}
        primary={primary}
        loadError={installsFailed}
        onChanged={load}
      />
      <section className="settings-list" aria-label="Application settings">
        {state.settings.map((setting) => (
          <SettingRow key={setting.name} setting={setting} onSaved={load} />
        ))}
      </section>
    </>
  );
}

/**
 * InstallerBundleMarker mirrors internal/detect.InstallerBundleMarker: the
 * machine-stable Details prefix on the installer-bundle refusal. It is a
 * code, not prose — the human sentence after it stays free to change.
 */
const installerBundleMarker = "SMAPI_INSTALLER_BUNDLE: ";

/** Refusal copy per code: what happened, how to recover, and the retry. */
function refusalCopy(code: string | null, details: string): string {
  switch (code) {
    case "GAME_NOT_FOUND":
      if (details.startsWith(installerBundleMarker)) {
        return "This looks like the SMAPI installer — run it instead, then pick the game folder.";
      }
      return "No Stardew Valley install in this folder. Choose the folder containing Stardew Valley.dll.";
    case "GAME_LEGACY":
      return "This game is too old for current SMAPI (or is the compatibility branch). Update the game / switch branch, then retry.";
    case "GAME_INVALID":
      return `${details} Verify game files or fix permissions, then retry.`;
    default:
      return details;
  }
}

/** Detect state: idle, running, a result summary, or a typed failure. */
type DetectState =
  | { status: "idle" }
  | { status: "running" }
  | { status: "done"; message: string; needsChoice: boolean }
  | { status: "failed"; message: string };

/** Add state: idle, adding, or a typed refusal to display. */
type AddState =
  | { status: "idle" }
  | { status: "adding" }
  | { status: "refused"; code: string; message: string };

/**
 * GameInstallsGroup is the single picker flow: resolved primary card, path
 * text entry plus add button (the automation-drivable picker), installs
 * list with Primary markers and Use-this choices, and an enabled Detect-now
 * button that runs the automatic discovery pass (zero/one/many copy with
 * per-row choosers when the pointer needs a choice).
 */
function GameInstallsGroup({
  installs,
  primary,
  loadError,
  onChanged,
}: {
  installs: GameInstallView[];
  primary: GameInstallView | null;
  loadError: string | null;
  onChanged: () => void;
}) {
  const [draft, setDraft] = useState("");
  const [add, setAdd] = useState<AddState>({ status: "idle" });
  const [detect, setDetect] = useState<DetectState>({ status: "idle" });

  const submit = () => {
    const path = draft.trim();
    if (path === "") {
      return;
    }
    setAdd({ status: "adding" });
    ApplicationService.AddGameInstall(path).then(
      () => {
        setAdd({ status: "idle" });
        setDraft("");
        onChanged();
      },
      (error: unknown) => {
        const code = appErrorCode(error);
        const payload = appErrorPayload(error);
        const details =
          payload?.details ??
          (error instanceof Error ? error.message : String(error));
        setAdd({
          status: "refused",
          code: code ?? "UNKNOWN",
          message: refusalCopy(code, details),
        });
      },
    );
  };

  const choose = (id: number) => {
    ApplicationService.SetPrimaryGameInstall(id).then(
      () => {
        onChanged();
      },
      (error: unknown) => {
        const code = appErrorCode(error);
        const payload = appErrorPayload(error);
        const details =
          payload?.details ??
          (error instanceof Error ? error.message : String(error));
        setAdd({
          status: "refused",
          code: code ?? "UNKNOWN",
          message: details,
        });
      },
    );
  };

  const runDetect = () => {
    setDetect({ status: "running" });
    ApplicationService.DetectNow().then(
      (result) => {
        setDetect({
          status: "done",
          message: result.message,
          needsChoice: result.needsChoice,
        });
        onChanged();
      },
      (error: unknown) => {
        const payload = appErrorPayload(error);
        const details =
          payload?.details ??
          (error instanceof Error ? error.message : String(error));
        setDetect({ status: "failed", message: details });
      },
    );
  };

  return (
    <section className="game-installs" aria-label="Game installs">
      <h2>Game installs</h2>
      {loadError !== null ? (
        <p role="alert" className="identity-error">
          Game installs are not available: {loadError}
        </p>
      ) : null}
      {primary === null ? (
        <p>No game configured yet. Run Detect now or choose the game folder.</p>
      ) : (
        <div className="primary-card">
          <p>Path: {primary.path}</p>
          <p>Source: {primary.source}</p>
          <p>Game version: {primary.gameVersion ?? "version unknown"}</p>
          <p>SMAPI entry point: {primary.smapiExePath ?? "not detected"}</p>
          <p>
            SMAPI: {primary.smapiState}
            {primary.smapiVersion ? ` ${primary.smapiVersion}` : " (version unknown)"}
          </p>
        </div>
      )}
      <div className="picker-row">
        <label htmlFor="game-folder-path">Choose game folder…</label>
        <input
          id="game-folder-path"
          type="text"
          value={draft}
          placeholder="C:\Program Files (x86)\Steam\steamapps\common\Stardew Valley"
          onChange={(event) => {
            setDraft(event.target.value);
          }}
        />
        <Button onClick={submit} disabled={add.status === "adding" || draft.trim() === ""}>
          {add.status === "adding" ? "Adding…" : "Add"}
        </Button>
      </div>
      {add.status === "refused" ? (
        <p role="alert" className="identity-error">
          {add.code}: {add.message} <Button onClick={submit}>Choose a different folder</Button>
        </p>
      ) : null}
      <div className="detect-row">
        <Button onClick={runDetect} disabled={detect.status === "running"}>
          {detect.status === "running" ? "Detecting…" : "Detect now"}
        </Button>
      </div>
      {detect.status === "done" ? (
        <p role="status" className="detect-result">
          {detect.message}
          {detect.needsChoice ? " Pick the primary with the per-row chooser." : ""}
        </p>
      ) : null}
      {detect.status === "failed" ? (
        <p role="alert" className="identity-error">
          Detection failed: {detect.message}
        </p>
      ) : null}
      {installs.length > 0 ? (
        <ul>
          {installs.map((row) => (
            <li key={row.id}>
              <span>{row.path}</span> <span>{row.source}</span>{" "}
              {row.isPrimary ? (
                <span>Primary</span>
              ) : (
                <Button onClick={() => choose(row.id)}>Use this</Button>
              )}
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}


function SettingRow({
  setting,
  onSaved,
}: {
  setting: SettingView;
  onSaved: () => void;
}) {
  const [draft, setDraft] = useState<string>(() =>
    setting.kind === "bool"
      ? (setting.value === true ? "true" : "false")
      : (setting.value === null || setting.value === undefined
        ? ""
        : String(setting.value)),
  );
  const [row, setRow] = useState<RowState>({ status: "idle" });

  // Keep the draft in sync when a re-read lands after a save.
  useEffect(() => {
    if (setting.kind === "bool") {
      setDraft(setting.value === true ? "true" : "false");
    } else if (setting.value === null || setting.value === undefined) {
      setDraft("");
    } else {
      setDraft(String(setting.value));
    }
  }, [setting.value, setting.kind]);

  const save = () => {
    let parsed: unknown;
    if (setting.kind === "bool") {
      if (draft !== "true" && draft !== "false") {
        setRow({
          status: "invalid",
          code: "SETTING_INVALID",
          message: `want true or false, got ${draft}`,
        });
        return;
      }
      parsed = draft === "true";
    } else if (setting.kind === "int") {
      if (!/^-?\d+$/.test(draft.trim())) {
        setRow({
          status: "invalid",
          code: "SETTING_INVALID",
          message: `want an integer, got ${draft}`,
        });
        return;
      }
      parsed = Number.parseInt(draft.trim(), 10);
    } else {
      parsed = draft;
    }
    setRow({ status: "saving" });
    ApplicationService.SetSetting(setting.name, parsed).then(
      () => {
        setRow({ status: "idle" });
        onSaved();
      },
      (error: unknown) => {
        const code = appErrorCode(error);
        const payload = appErrorPayload(error);
        const message =
          payload?.message ??
          (error instanceof Error ? error.message : String(error));
        setRow({
          status: "invalid",
          code: code ?? "UNKNOWN",
          message,
        });
      },
    );
  };

  const inputId = `setting-${setting.name}`;
  return (
    <div className="setting-row">
      <label htmlFor={inputId}>
        {setting.name}
        {setting.isDefault ? " (default)" : ""}
      </label>
      <p className="control-reason">{setting.description}</p>
      {setting.kind === "bool" ? (
        <input
          id={inputId}
          type="checkbox"
          checked={draft === "true"}
          disabled={row.status === "saving"}
          onChange={(event) => {
            setDraft(event.target.checked ? "true" : "false");
          }}
        />
      ) : setting.kind === "int" ? (
        <input
          id={inputId}
          type="number"
          value={draft}
          disabled={row.status === "saving"}
          onChange={(event) => {
            setDraft(event.target.value);
          }}
        />
      ) : (
        <input
          id={inputId}
          type="text"
          value={draft}
          disabled={row.status === "saving"}
          onChange={(event) => {
            setDraft(event.target.value);
          }}
        />
      )}
      <Button onClick={save} disabled={row.status === "saving"}>
        {row.status === "saving" ? "Saving…" : "Save"}
      </Button>
      {row.status === "invalid" ? (
        <p role="alert" className="identity-error">
          {row.code}: {row.message}
        </p>
      ) : null}
    </div>
  );
}
