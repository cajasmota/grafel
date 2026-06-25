//go:build !windows

package skilllink

import "os"

// linkSkillDirPlatform on unix creates a real symbolic link, preserving the
// historical behaviour exactly. Symlink creation never requires elevation on
// Linux/macOS, so there is no fallback chain here.
func linkSkillDirPlatform(src, dst string) (LinkMode, error) {
	if err := os.Symlink(src, dst); err != nil {
		return LinkModeNone, err
	}
	return LinkModeSymlink, nil
}
