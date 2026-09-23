// Package buildinfo carries the identity of the Astradew build it is compiled into.
package buildinfo

// Name is the product name. It is what the window title and the interface show,
// and it is the name packaging metadata uses.
const Name = "Astradew"

// Description is the one-line product description used by packaging metadata.
const Description = "Stardew Valley mod environment manager"

// Identifier is the reverse-DNS bundle identifier used by packaging.
const Identifier = "com.zendevve.astradew"

// Version is the semantic version of this build. The release pipeline overrides
// it at link time:
//
//	go build -ldflags "-X github.com/Zendevve/astradew/internal/buildinfo.Version=1.2.3"
//
// The default marks a build that no release pipeline produced.
var Version = "0.0.0-dev"
