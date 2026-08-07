//go:build darwin

package watch

import (
	"math"

	"github.com/cajasmota/grafel/internal/daemon/fdlimit"
)

// fdDescriptorPerFile records that on this platform a watched directory costs
// one OS file descriptor per file inside it, so descriptor accounting is real
// and the budget is enforced. Selected by build tag rather than runtime.GOOS
// so the constant is available to the compiler and to build-tagged tests
// (#6218: a runtime GOOS check plus t.Skip guards execution, not compilation).
const fdDescriptorPerFile = true

// defaultCostModel returns the kqueue arithmetic: dirs + files.
func defaultCostModel() fdCostModel { return kqueueCostModel }

// fdBudgetReserveFraction is the share of the effective descriptor limit held
// back for everything in the daemon that is NOT an fs watch: graph mmaps and
// segment files, the unix socket and its accepted connections, the dashboard
// listener, log files, and the pipes of every `git` subprocess the indexer
// spawns. A quarter is deliberately generous — running out of descriptors for
// the store is a corruption risk, whereas running out of watch budget only
// costs freshness, and this fix exists precisely so the second failure is the
// one we take.
const fdBudgetReserveNumerator, fdBudgetReserveDenominator = 1, 4

// fdBudgetMinReserve is the floor on that headroom, for the case of an
// unusually low soft limit where a quarter would be a handful of descriptors.
const fdBudgetMinReserve = 1024

// defaultFDBudgetLimit derives the watch budget from the EFFECTIVE descriptor
// limit — the post-clamp RLIMIT_NOFILE soft limit read back via getrlimit, not
// the number anyone asked for. That distinction is the whole point on Darwin:
// the daemon's launchd plist requests NumberOfFiles 65536, which exceeds
// kern.maxfilesperproc (61440 on the measured machine) and is silently clamped
// down, and fdlimit.Raise likewise falls back to smaller candidates on EINVAL.
// Budgeting from the requested value would over-commit by exactly the amount
// the kernel refused.
func defaultFDBudgetLimit() int {
	if n, ok := envFDBudget(); ok {
		return n
	}
	soft, _, err := fdlimit.Current()
	if err != nil || soft == 0 {
		// Unknown limit: do not invent a ceiling we cannot justify. The
		// pre-existing directory cap still applies.
		return 0
	}
	if soft > math.MaxInt32 {
		soft = math.MaxInt32 // RLIM_INFINITY and friends
	}
	limit := int(soft)
	reserve := limit * fdBudgetReserveNumerator / fdBudgetReserveDenominator
	if reserve < fdBudgetMinReserve {
		reserve = fdBudgetMinReserve
	}
	if reserve >= limit {
		return 0
	}
	return limit - reserve
}
