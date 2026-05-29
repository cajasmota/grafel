//go:build android || ios

// gomobile_branch.go — fixture for the Data Flow.branch_conditions surface
// (#3255). A gomobile-bound package whose mobile-facing code branches on the
// runtime platform via both the `if runtime.GOOS == ...` and the
// `switch runtime.GOOS { ... }` idioms.
package mobilecfg

import (
	"runtime"

	"golang.org/x/mobile/app"
)

// configPath returns a platform-specific config path. It branches on
// runtime.GOOS with an if / else-if chain — the canonical Go platform branch.
func configPath() string {
	if runtime.GOOS == "android" {
		return "/data/local/cfg"
	} else if runtime.GOOS == "ios" {
		return "Documents/cfg"
	}
	return "./cfg"
}

// platformLabel discriminates on runtime.GOOS via a switch, with a
// multi-platform case arm and a default.
func platformLabel() string {
	switch runtime.GOOS {
	case "android":
		return "droid"
	case "ios", "darwin":
		return "apple"
	default:
		return "other"
	}
}

// guarded composes runtime.GOOS into a logical condition; the GOOS comparison
// still floats out as a platform branch.
func guarded(enabled bool) bool {
	if enabled && runtime.GOOS == "android" {
		return true
	}
	return false
}

func main() {
	app.Main(func(a app.App) {
		_ = configPath()
		_ = platformLabel()
		_ = guarded(true)
	})
}
