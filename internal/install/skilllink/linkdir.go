package skilllink

// LinkMode records how a skill directory was materialised into a Claude
// skills directory. Different platforms (and the non-admin Windows fallback
// chain) end up at different modes, and the uninstall / validate paths need
// to recognise each one.
type LinkMode int

const (
	// LinkModeNone means nothing was created (error before any link/copy).
	LinkModeNone LinkMode = iota
	// LinkModeSymlink is a real OS symbolic link (unix, or Windows when the
	// process happens to hold SeCreateSymbolicLinkPrivilege / Developer Mode).
	LinkModeSymlink
	// LinkModeJunction is a Windows directory junction (NTFS reparse point).
	// Junctions do NOT require admin rights or Developer Mode, so this is the
	// preferred mechanism for a non-admin user in plain cmd.exe. (#5318)
	LinkModeJunction
	// LinkModeCopy is a plain recursive directory copy — the last-resort
	// fallback when neither a symlink nor a junction can be created.
	LinkModeCopy
)

func (m LinkMode) String() string {
	switch m {
	case LinkModeSymlink:
		return "symlink"
	case LinkModeJunction:
		return "junction"
	case LinkModeCopy:
		return "copy"
	default:
		return "none"
	}
}

// linkSkillDir materialises src (a skill directory) at dst.
//
// Platform behaviour:
//
//   - unix: a real symbolic link (os.Symlink). Behaviour is unchanged from the
//     historical implementation — see linkdir_unix.go.
//   - Windows: a directory junction via `mklink /J`, which needs NO admin or
//     Developer Mode, falling back to a recursive copy if even that fails
//     (e.g. cross-volume, or junctions disabled). Real symlinks are tried
//     first only when they are free (privilege already held); we never surface
//     the "A required privilege is not held by the client" failure as a fatal
//     error. See linkdir_windows.go.
//
// dst must not already exist (callers remove a stale destination first).
// Returns the LinkMode actually used so the caller can record it for the
// uninstall / validate paths and degrade gracefully (clear error, never a
// crash) when nothing worked.
func linkSkillDir(src, dst string) (LinkMode, error) {
	return linkSkillDirPlatform(src, dst)
}
