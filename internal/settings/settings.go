// Package settings is the typed registry over the stored key/value settings
// rows. The registry below is the only place a setting is defined: every
// read and write resolves its declaration first, so an undeclared key is a
// typed SETTING_UNKNOWN refusal, never a silent default.
//
// Values are stored in the existing settings(key TEXT PRIMARY KEY,
// value TEXT NOT NULL) table as JSON scalars (string, bool, or int). Rows
// this build's registry does not declare — written by a newer build, a
// migration, or hand SQL — are never read, validated, or rewritten by this
// package: round trips leave them byte-untouched.
package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Zendevve/astradew/internal/apperror"
)

// Kind is the JSON scalar kind a setting holds. Only scalars: settings are
// single values, never objects or arrays.
type Kind string

const (
	// KindString holds a JSON string.
	KindString Kind = "string"
	// KindBool holds a JSON boolean.
	KindBool Kind = "bool"
	// KindInt holds a JSON integer number.
	KindInt Kind = "int"
)

// Setting declares one setting: its name, kind, default, human description,
// and value validation beyond the kind check. The registry is the only place
// a setting is defined.
type Setting struct {
	Name        string
	Kind        Kind
	Default     any
	Description string
	Validate    func(value any) error
}

// Value is one resolved setting: its declaration plus the current stored
// value and whether the stored value is the declared default.
type Value struct {
	Name        string `json:"name"`
	Kind        Kind   `json:"kind"`
	Value       any    `json:"value"`
	IsDefault   bool   `json:"isDefault"`
	Description string `json:"description"`
}

// Registry lists every setting this build declares. Small and honest: a UI
// presentation preference and a behavioural toggle.
var Registry = []Setting{
	{
		Name:        "theme",
		Kind:        KindString,
		Default:     "system",
		Description: "Colour scheme preference: system, light, or dark.",
		Validate: func(value any) error {
			text, _ := value.(string)
			switch text {
			case "system", "light", "dark":
				return nil
			default:
				return fmt.Errorf("want one of system, light, dark, got %q", text)
			}
		},
	},
	{
		Name:        "ui-scale",
		Kind:        KindInt,
		Default:     100,
		Description: "Interface scale in percent, from 50 to 200.",
		Validate: func(value any) error {
			scale, _ := value.(int)
			if scale < 50 || scale > 200 {
				return fmt.Errorf("want 50 to 200, got %d", scale)
			}
			return nil
		},
	},
	{
		Name:        "check-updates-on-start",
		Kind:        KindBool,
		Default:     true,
		Description: "Check for mod updates when Astradew starts.",
	},
	{
		// PrimaryGameInstallID names the game_installs row the product
		// surfaces. Its only editor is SetPrimaryGameInstall: it is
		// registered here so Get/set validation exists, but EXCLUDED from
		// GetAll list output so no raw numeric input can dangle it.
		Name:        "primary-game-install-id",
		Kind:        KindInt,
		Default:     0,
		Description: "Primary game installation row id; set only through the installs chooser.",
		Validate: func(value any) error {
			id, _ := value.(int)
			if id < 1 {
				return fmt.Errorf("want >= 1, got %d", id)
			}
			return nil
		},
	},
}

// Service reads and writes settings rows through the store's database
// handle. It holds no state beyond the handle.
type Service struct {
	db *sql.DB
}

// New returns a Service over db. db must be the store's open handle; the
// service never opens or migrates the database itself.
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// lookup resolves name to its registry declaration, or reports
// SETTING_UNKNOWN for a key no build declares.
func lookup(name string) (*Setting, error) {
	for i := range Registry {
		if Registry[i].Name == name {
			return &Registry[i], nil
		}
	}
	return nil, apperror.NewRecoverable(apperror.CodeSettingUnknown, "unknown setting", fmt.Sprintf("setting %q is not declared in this build's registry", name))
}

// Get returns the stored JSON scalar for key, or the declared default when
// never written. An unknown key refuses with SETTING_UNKNOWN.
func (s *Service) Get(ctx context.Context, key string) (any, error) {
	decl, err := lookup(key)
	if err != nil {
		return nil, err
	}
	var raw string
	queryErr := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&raw)
	if queryErr == sql.ErrNoRows {
		return decl.Default, nil
	}
	if queryErr != nil {
		return nil, queryErr
	}
	value, err := decode(decl.Kind, raw)
	if err != nil {
		return nil, err
	}
	return value, nil
}

// PrimaryGameInstallIDKey is the settings key naming the primary
// game_installs row. Row existence is resolved at read/use time, never at
// Set time: Set cannot refuse a dangling id, and a dangling pointer
// degrades to "no primary", never to a wrong install.
const PrimaryGameInstallIDKey = "primary-game-install-id"

// GetAll resolves every registry declaration to its current value,
// reporting whether each is still the default. The primary-install pointer
// is EXCLUDED: its only editor is the installs chooser, never a raw
// numeric input. Get still reads it.
func (s *Service) GetAll(ctx context.Context) ([]Value, error) {
	stored, err := s.readAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Value, 0, len(Registry))
	for i := range Registry {
		decl := &Registry[i]
		if decl.Name == PrimaryGameInstallIDKey {
			continue
		}
		value := decl.Default
		isDefault := true
		if raw, ok := stored[decl.Name]; ok {
			decoded, err := decode(decl.Kind, raw)
			if err != nil {
				return nil, err
			}
			value = decoded
			isDefault = encode(decoded) == encode(decl.Default)
		}
		out = append(out, Value{
			Name:        decl.Name,
			Kind:        decl.Kind,
			Value:       value,
			IsDefault:   isDefault,
			Description: decl.Description,
		})
	}
	return out, nil
}

// Set validates raw (a JSON scalar) against the key's declaration and stores
// it. An unknown key refuses with SETTING_UNKNOWN; a value failing the kind
// or Validate check refuses with recoverable SETTING_INVALID, naming the key,
// the reason, and the preserved previous value. The stored value is left
// unchanged on any refusal.
func (s *Service) Set(ctx context.Context, key string, raw json.RawMessage) error {
	decl, err := lookup(key)
	if err != nil {
		return err
	}
	previous, err := s.Get(ctx, key)
	if err != nil {
		return err
	}
	invalid := func(reason string) error {
		return apperror.NewRecoverable(
			apperror.CodeSettingInvalid,
			fmt.Sprintf("invalid value for setting %q", key),
			fmt.Sprintf("setting %q: %s; previous value %s preserved", key, reason, encode(previous)),
		)
	}
	value, err := decode(decl.Kind, strings.TrimSpace(string(raw)))
	if err != nil {
		return invalid(err.Error())
	}
	if decl.Validate != nil {
		if err := decl.Validate(value); err != nil {
			return invalid(err.Error())
		}
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, encode(value))
	return err
}

// readAll returns every stored row, including keys no declaration covers.
// Callers resolve registry keys out of the map; unrecognised rows pass
// through untouched because nothing ever writes them back.
func (s *Service) readAll(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, rows.Err()
}

// encode renders a Go scalar as its canonical JSON form.
func encode(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}

// decode parses raw JSON and checks it holds the kind's scalar: strings only
// for string, booleans only for bool, whole numbers only for int. Objects,
// arrays, null, and cross-kind scalars are rejected.
func decode(kind Kind, raw string) (any, error) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("not valid JSON: %s", err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("not a single JSON value: %q", raw)
	}
	switch kind {
	case KindString:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("want a JSON string, got %q", raw)
		}
		return text, nil
	case KindBool:
		flag, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("want a JSON boolean, got %q", raw)
		}
		return flag, nil
	case KindInt:
		number, ok := value.(json.Number)
		if !ok {
			return nil, fmt.Errorf("want a JSON integer, got %q", raw)
		}
		parsed, err := number.Int64()
		if err != nil {
			return nil, fmt.Errorf("want a JSON integer, got %q", raw)
		}
		return int(parsed), nil
	default:
		return nil, fmt.Errorf("unknown setting kind %q", kind)
	}
}
