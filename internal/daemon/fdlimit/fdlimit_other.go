//go:build !linux && !darwin

package fdlimit

// Raise is a no-op on platforms without POSIX RLIMIT_NOFILE semantics
// (e.g. Windows). It reports success with zero limits so callers can log a
// uniform, non-fatal result.
func Raise(target uint64) (old, updated uint64, err error) {
	return 0, 0, nil
}
