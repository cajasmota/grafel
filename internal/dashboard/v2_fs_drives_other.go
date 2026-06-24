//go:build !windows

package dashboard

// drivesSupported is false on non-Windows platforms: there is a single
// filesystem root ("/"), so there is no "drives" level and "up" from the root
// correctly yields "" (no parent). These stubs let the shared handler call the
// same helpers unconditionally.
const drivesSupported = false

// listDrives never returns entries off Windows.
func listDrives() []v2FsEntry { return nil }

// isDriveRoot is always false off Windows.
func isDriveRoot(string) bool { return false }
