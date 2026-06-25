//go:build windows

package skilllink

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cajasmota/grafel/internal/executil"
)

// linkSkillDirPlatform materialises a skill directory at dst without ever
// requiring administrator rights or Developer Mode (#5318).
//
// The fallback chain, cheapest/most-faithful first:
//
//  1. os.Symlink — only succeeds when the process already holds
//     SeCreateSymbolicLinkPrivilege (admin or Developer Mode). We try it
//     because a real symlink is the most faithful link, but its failure for a
//     plain non-admin user is EXPECTED and non-fatal.
//  2. Directory junction via `mklink /J` — an NTFS reparse point that a
//     standard user can create in plain cmd.exe with NO elevation. This is the
//     primary mechanism for the non-admin path.
//  3. Recursive copy — last resort (e.g. junctions disabled, cross-volume).
//
// Returns the LinkMode that actually succeeded, or a clear aggregated error if
// every mechanism failed (callers report it and continue; never crash).
func linkSkillDirPlatform(src, dst string) (LinkMode, error) {
	// 1) Real symlink — free when the privilege is already held.
	if err := os.Symlink(src, dst); err == nil {
		return LinkModeSymlink, nil
	}

	// 2) Directory junction — no elevation required. `mklink` is a cmd.exe
	//    builtin (not a standalone .exe), so it must be invoked via `cmd /c`.
	//    Argument order is `mklink /J <link> <target>`.
	if err := makeJunction(src, dst); err == nil {
		return LinkModeJunction, nil
	} else {
		// Remove any partial artifact the junction attempt may have left so the
		// copy fallback starts from a clean destination.
		_ = os.RemoveAll(dst)
		junctionErr := err

		// 3) Recursive copy.
		if cerr := copyDirTree(src, dst); cerr != nil {
			return LinkModeNone, fmt.Errorf(
				"link skill dir %s -> %s: junction failed (%v) and copy fallback failed (%w)",
				src, dst, junctionErr, cerr)
		}
		return LinkModeCopy, nil
	}
}

// makeJunction creates an NTFS directory junction at link pointing to target
// using the cmd.exe `mklink /J` builtin. Junctions are creatable by a standard
// (non-admin) user, unlike symbolic links.
func makeJunction(target, link string) error {
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	executil.NoWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J %s %s: %w: %s", link, target, err, string(out))
	}
	// mklink returns exit 0 even on some failures localised to stdout; verify
	// the link now exists as a directory reparse point.
	if _, statErr := os.Lstat(link); statErr != nil {
		return fmt.Errorf("mklink /J reported success but %s is absent: %v", link, statErr)
	}
	return nil
}

// copyDirTree recursively copies the directory tree rooted at src into dst.
// Used only as the last-resort fallback when neither a symlink nor a junction
// can be created. Self-contained so the skilllink package carries no extra
// cross-package dependency for the rare copy path.
func copyDirTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
