// Helpers for reading the typed service errors produced by
// internal/apperror out of a Wails bound-method call rejection.
//
// A failed bound call rejects with a RuntimeError shaped
// {message, cause, kind}: Wails serialises the Go error through the
// service's MarshalError hook and attaches the result to the rejection's
// `cause` (see runtime.ts runtimeCallWithID). apperror.MarshalError emits
// {"code","message","details","recoverable"}, so the code is available on
// the rejection's cause without parsing message text.
//
// Branch on appErrorCode(), never on err.message.

export type AppErrorCode = string;

export interface AppErrorPayload {
  code: AppErrorCode;
  message: string;
  details: string;
  recoverable: boolean;
}

/** True when value looks like an apperror payload (code string present). */
export function isAppError(value: unknown): value is AppErrorPayload {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  if (!("code" in value)) {
    return false;
  }
  const code: unknown = value.code;
  return typeof code === "string" && code.length > 0;
}

/**
 * Extract the full apperror payload from a Wails call rejection, or null
 * when the rejection carries no typed payload. Reads the rejection's
 * cause — where the marshalled *apperror.AppError lands — accepting either
 * the decoded payload or its JSON string form, and falls back to a
 * top-level code field for directly-thrown payloads.
 */
export function appErrorPayload(err: unknown): AppErrorPayload | null {
  const candidates: unknown[] = [err];
  if (typeof err === "object" && err !== null && "cause" in err) {
    candidates.push(err.cause);
  }
  for (const candidate of candidates) {
    if (isAppError(candidate)) {
      return candidate;
    }
    if (typeof candidate === "string") {
      try {
        const parsed: unknown = JSON.parse(candidate);
        if (isAppError(parsed)) {
          return parsed;
        }
      } catch {
        continue;
      }
    }
  }
  return null;
}

/**
 * Extract the apperror code from a Wails call rejection. Returns null when
 * the rejection carries no typed code.
 */
export function appErrorCode(err: unknown): AppErrorCode | null {
  const payload = appErrorPayload(err);
  if (payload === null) {
    return null;
  }
  return payload.code;
}
