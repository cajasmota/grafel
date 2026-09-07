//go:build linux

package watch

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// inotifyPoolApplies records that this platform HAS a per-UID inotify watch
// pool, so a projection of watch descriptors can be compared against a real
// ceiling. Selected by build tag rather than a runtime.GOOS check so the
// constant is available to the compiler and to build-tagged tests (#6218).
const inotifyPoolApplies = true

// inotifyMaxUserWatchesPath is the kernel's per-UID watch ceiling. It is NOT
// namespaced: a container reads and shares the host's value, and cannot raise
// it (the sysctl is read-only from an unprivileged user namespace). That is
// exactly why the probe exists — grafel can see its own demand against this
// number, and cannot see anyone else's.
//
// A var, not a const, for exactly one reason: the Linux leg of CI can then
// point it at a fixture and grade the unreadable and unparseable branches,
// which no host with a working /proc can produce on demand. Never reassigned
// in production.
var inotifyMaxUserWatchesPath = "/proc/sys/fs/inotify/max_user_watches"

// readInotifyLimit reports the per-UID watch ceiling and where it came from.
func readInotifyLimit() (int, string, error) {
	b, err := os.ReadFile(inotifyMaxUserWatchesPath)
	if err != nil {
		return 0, inotifyMaxUserWatchesPath, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, inotifyMaxUserWatchesPath, fmt.Errorf("unparseable value %q: %w", strings.TrimSpace(string(b)), err)
	}
	if n < 0 {
		n = 0
	}
	return n, inotifyMaxUserWatchesPath, nil
}
