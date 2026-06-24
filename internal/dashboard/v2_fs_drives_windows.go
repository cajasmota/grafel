//go:build windows

package dashboard

import (
	"os"
)

// drivesSupported reports whether the host has a "drives" level above the
// filesystem root. On Windows there is no single root ("/") — each drive
// (C:\, D:\, …) is its own tree — so the picker needs a virtual level above
// the drive roots that lists the available drive letters.
const drivesSupported = true

// listDrives enumerates the drive letters that currently exist on this Windows
// host (A:..Z:), probing each with os.Stat on "<letter>:\". Only drives that
// resolve are returned, in ascending order. Each entry's Path is the drive
// root (e.g. "C:\\") so selecting it navigates into that drive.
func listDrives() []v2FsEntry {
	out := make([]v2FsEntry, 0, 26)
	for c := 'A'; c <= 'Z'; c++ {
		root := string(c) + `:\`
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			out = append(out, v2FsEntry{
				Name:   string(c) + ":",
				Path:   root,
				IsDir:  true,
				Hidden: false,
			})
		}
	}
	return out
}

// isDriveRoot reports whether abs is a bare drive root such as "C:\" or "C:".
// At a drive root the "up" control should ascend to the drives level rather
// than returning "" (filepath.Dir("C:\\") == "C:\\", which would otherwise
// trap the user on a single drive — the bug this fixes).
func isDriveRoot(abs string) bool {
	// Forms: `C:\` (len 3) or `C:` (len 2).
	switch len(abs) {
	case 2:
		return abs[1] == ':' && isDriveLetter(abs[0])
	case 3:
		return abs[1] == ':' && (abs[2] == '\\' || abs[2] == '/') && isDriveLetter(abs[0])
	}
	return false
}

func isDriveLetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}
