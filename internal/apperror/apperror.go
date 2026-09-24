// Package apperror defines the typed errors Astradew services return to the
// frontend.
//
// A bound-service method reports failure as a plain error return. Wails
// serialises that error for the TypeScript caller through the per-service
// MarshalError hook (application.ServiceOptions.MarshalError): whatever JSON
// MarshalError returns for the error becomes the `cause` of the
// `RuntimeError` the generated `Call.ByID` promise rejects with. The TS side
// (runtime.ts runtimeCallWithID) parses a non-OK response shaped
// {message, cause, kind} and rethrows it with err.cause = json.cause, so a
// RuntimeError rejection carries the marshalled AppError as its cause.
//
// MarshalError below is that hook: it encodes *AppError as
// {"code","message","details","recoverable"} and returns nil for anything
// else so Wails falls back to its default marshaller.
package apperror

import (
	"encoding/json"
	"errors"
)

// Code identifies the failure mode of an AppError. The frontend branches on
// Code, never on message text.
type Code string

const (
	// CodeProbeFailure is returned by the ApplicationService.ProbeFailure
	// demo probe. It exists so tests can prove a code survives the
	// Go-to-TypeScript marshalling boundary intact.
	CodeProbeFailure Code = "PROBE_FAILURE"

	// CodeAppRootUnusable is returned when the Astradew application data
	// root cannot be created or proven writable (unwritable location, or a
	// file where a directory belongs). It is recoverable: the user can fix
	// permissions or free the path and retry. Details name the path and
	// the reason.
	CodeAppRootUnusable Code = "APPROOT_UNUSABLE"

	// CodeStoreOpenFailed is returned when the SQLite database cannot be
	// opened or its required PRAGMAs cannot be established. It is
	// recoverable: the user can restore a backup or move the file aside
	// and retry. Details name the database path and the reason.
	CodeStoreOpenFailed Code = "STORE_OPEN_FAILED"

	// CodeStoreMigrationFailed is returned when a numbered migration cannot
	// be applied. The failed migration rolls back fully and the
	// pre-migration backup is kept. It is recoverable: the user can restore
	// the newest backup or start fresh while preserving the old file.
	// Details name the failing version, the database path, and both repair
	// actions with concrete paths.
	CodeStoreMigrationFailed Code = "STORE_MIGRATION_FAILED"
	// CodeSettingUnknown is returned when a setting outside this build's
	// registry is addressed. The registry is the only place a setting is
	// defined, so an undeclared key is a programmer error, not a default:
	// it is not recoverable by retrying. Details name the key.
	CodeSettingUnknown Code = "SETTING_UNKNOWN"

	// CodeSettingInvalid is returned when a value fails a setting's kind or
	// validation check. It is recoverable: the caller can offer a valid
	// value and retry. The stored value is left unchanged. Details name the
	// key, the reason, and the preserved previous value.
	CodeSettingInvalid Code = "SETTING_INVALID"

	// CodeSettingUnavailable is returned when settings are addressed on a
	// service constructed without a database handle. It is recoverable: the
	// caller can retry once a store is bound. Nothing is fabricated.
	CodeSettingUnavailable Code = "SETTING_UNAVAILABLE"
	// CodeGameNotFound refuses a path with no game install. Recoverable: pick
	// another folder (manual selection is the universal fallback). Details name
	// the path, plus the installer-bundle hint when the folder is the SMAPI
	// installer rather than a game.
	CodeGameNotFound Code = "GAME_NOT_FOUND"

	// CodeGameLegacy refuses a legacy/compat-branch game unmoddable by current
	// SMAPI. Recoverable: update the game / switch branch, then retry. Details
	// name legacy vs compatibility-branch.
	CodeGameLegacy Code = "GAME_LEGACY"

	// CodeGameInvalid refuses a corrupt or unreadable game dir. Recoverable:
	// verify/reinstall game files or fix permissions, then retry. Details name
	// the reason.
	CodeGameInvalid Code = "GAME_INVALID"
	// CodeTaskNotFound is returned when a task record cannot be found for
	// the requested id (tasks Get/UpdateProgress/Succeed/Fail, or the
	// bound Task method). Callers branch on the code, never on message
	// text.
	CodeTaskNotFound Code = "TASK_NOT_FOUND"
)

// AppError is a typed service failure. It crosses to TypeScript as the cause
// of the call rejection, so its JSON shape is the contract the frontend
// branches on.
type AppError struct {
	Code        Code   `json:"code"`
	Message     string `json:"message"`
	Details     string `json:"details"`
	Recoverable bool   `json:"recoverable"`
}

// Error reports "CODE: message" so logs and the rejection message stay human
// readable. Consumers must branch on Code, not on this text.
func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code) + ": " + e.Message
}

// New returns a non-recoverable AppError with no details.
func New(code Code, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

// NewRecoverable returns a recoverable AppError carrying extra details for
// display or diagnostics.
func NewRecoverable(code Code, message, details string) *AppError {
	return &AppError{Code: code, Message: message, Details: details, Recoverable: true}
}

// MarshalError implements the Wails per-service error marshaller contract
// (application.ServiceOptions.MarshalError): it encodes *AppError as JSON so
// the value lands on the TypeScript rejection's cause. It returns nil for any
// other error, which tells Wails to use its default marshaller instead.
func MarshalError(err error) []byte {
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr == nil {
		return nil
	}
	encoded, jsonErr := json.Marshal(appErr)
	if jsonErr != nil {
		return nil
	}
	return encoded
}
