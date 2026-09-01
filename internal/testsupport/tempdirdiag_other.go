//go:build !windows

package testsupport

// describeHolders is the non-Windows stub for the #6512 diagnostic.
//
// It is unreachable in practice: TempDirWithCleanupDiagnostic registers no
// cleanup at all unless runtime.GOOS == "windows", so nothing on a macOS or
// Linux leg ever calls this. It exists so the shared half of the diagnostic
// (EnumerateResidual, FormatResidual, RemovalProbe) stays compilable and
// testable off-Windows — POSIX permits unlinking an open file, which is
// exactly why this failure class is invisible on those platforms and why
// there is nothing here to identify.
func describeHolders([]string) string {
	return "handle holders: not applicable on this platform (POSIX unlinks open files)\n"
}
