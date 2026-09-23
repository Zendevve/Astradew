// Package app exposes Astradew's own identity to the frontend.
//
// The service is a plain type: it holds the name and version it was constructed
// with and reports them. It has no dependency on the framework runtime, so it is
// exercised directly by Go tests.
package app

// Info is the identity of the running application as the interface sees it.
type Info struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ApplicationService reports the identity of the application it was constructed
// for. It is bound to the frontend as ApplicationService.
type ApplicationService struct {
	name    string
	version string
}

// New returns an ApplicationService reporting the given product name and
// version.
func New(name, version string) *ApplicationService {
	return &ApplicationService{name: name, version: version}
}

// Info returns the application's identity.
func (s *ApplicationService) Info() Info {
	return Info{Name: s.name, Version: s.version}
}
