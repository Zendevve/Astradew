//go:build !windows

package discover

// steamRootFromRegistry is not applicable off Windows: there is no registry
// to read, so it reports absent — never an error.
func steamRootFromRegistry() string {
	return ""
}

// gogPathsFromRegistry is not applicable off Windows: there is no registry
// to read, so it reports no paths — never an error.
func gogPathsFromRegistry() []string {
	return nil
}
