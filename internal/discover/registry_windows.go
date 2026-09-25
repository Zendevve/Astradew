//go:build windows

package discover

import (
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// steamRootFromRegistry locates the Windows Steam root through the registry
// mechanism: the WOW6432Node branch first (Steam registers as a 32-bit
// app), then the plain HKLM and HKCU fallbacks. Absent keys yield "", never
// an error.
func steamRootFromRegistry() string {
	locations := []struct {
		hive registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`},
		{registry.LOCAL_MACHINE, `SOFTWARE\Valve\Steam`},
		{registry.CURRENT_USER, `Software\Valve\Steam`},
	}
	for _, location := range locations {
		key, err := registry.OpenKey(location.hive, location.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		value, _, err := key.GetStringValue("InstallPath")
		_ = key.Close()
		if err == nil && value != "" {
			return value
		}
	}
	return ""
}

// gogPathsFromRegistry probes the per-game installed-game keys for install
// paths. Value names vary by installer generation, so path,
// InstallLocation, InstallDirectory, and the directory of LauncherPath are
// all accepted. Absent keys yield no paths, never an error.
func gogPathsFromRegistry() []string {
	bases := []struct {
		hive registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\GOG.com\Games`},
		{registry.LOCAL_MACHINE, `SOFTWARE\GOG.com\Games`},
	}
	var out []string
	for _, base := range bases {
		key, err := registry.OpenKey(base.hive, base.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		subkeys, err := key.ReadSubKeyNames(-1)
		_ = key.Close()
		if err != nil {
			continue
		}
		for _, sub := range subkeys {
			game, err := registry.OpenKey(base.hive, base.path+`\`+sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			for _, name := range []string{"path", "InstallLocation", "InstallDirectory"} {
				if value, _, err := game.GetStringValue(name); err == nil && value != "" {
					out = append(out, value)
				}
			}
			if launcher, _, err := game.GetStringValue("LauncherPath"); err == nil && launcher != "" {
				out = append(out, filepath.Dir(launcher))
			}
			_ = game.Close()
		}
	}
	return out
}
