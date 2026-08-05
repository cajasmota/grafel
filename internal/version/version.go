package version

import (
	"fmt"
	"runtime/debug"
)

// Set by ldflags. Default values used in dev builds without -ldflags.
var (
	Version = "0.0.0-dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// DevVersion is the Version value of a binary built without ldflags
// injection, i.e. a local `go build` / `go run` rather than a release
// artifact.
const DevVersion = "0.0.0-dev"

// IsDev reports whether v is the un-injected dev-build version string.
// Callers that treat release binaries differently from scratch builds
// (e.g. install-state drift reporting) use this rather than hard-coding
// the sentinel.
func IsDev(v string) bool { return v == DevVersion }

// String returns a human-readable build descriptor. Pure function — no
// package-state mutation. When the binary was built without ldflags
// injection (typical `go build` with no Makefile), it falls back to
// VCS metadata embedded by the Go toolchain.
func String() string {
	v := Version
	commit := Commit
	date := Date

	if IsDev(v) {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, s := range info.Settings {
				switch s.Key {
				case "vcs.revision":
					if commit == "unknown" && s.Value != "" {
						commit = s.Value
						if len(commit) > 12 {
							commit = commit[:12]
						}
					}
				case "vcs.time":
					if date == "unknown" && s.Value != "" {
						date = s.Value
					}
				}
			}
		}
	}

	return fmt.Sprintf("%s (commit %s, built %s)", v, commit, date)
}
