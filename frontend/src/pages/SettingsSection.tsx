import { useEffect, useState } from "react";

import type { SettingView } from "../../bindings/github.com/Zendevve/astradew/internal/app";
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
 * SettingsSection renders the Settings view from the backend Settings()
 * list: one labelled native input per registry setting with its current
 * value and honest default marker. Editing a value calls SetSetting and
 * re-reads; an invalid value shows the typed code branch with the backend
 * message. It invents nothing — values come only from the backend.
 */
export default function SettingsSection() {
  const [state, setState] = useState<SettingsState>({ status: "loading" });

  const load = () => {
    setState({ status: "loading" });
    ApplicationService.Settings().then(
      (settings) => {
        setState({ status: "ready", settings: settings ?? [] });
      },
      (error: unknown) => {
        setState({
          status: "failed",
          message: error instanceof Error ? error.message : String(error),
        });
      },
    );
  };

  useEffect(() => {
    let cancelled = false;
    ApplicationService.Settings().then(
      (settings) => {
        if (!cancelled) {
          setState({ status: "ready", settings: settings ?? [] });
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
    return <StatusText icon="○">Reading settings…</StatusText>;
  }
  if (state.status === "failed") {
    return (
      <p role="alert" className="identity-error">
        Settings are not available: {state.message}
      </p>
    );
  }
  return (
    <section className="settings-list" aria-label="Application settings">
      {state.settings.map((setting) => (
        <SettingRow key={setting.name} setting={setting} onSaved={load} />
      ))}
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
