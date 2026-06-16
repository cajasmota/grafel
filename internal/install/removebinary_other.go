//go:build !windows

package install

// removeBinaryPlatform removes the binary on Unix-like systems. Unlinking a
// running executable is permitted there (the inode lives on until the last
// open handle closes), so a plain os.Remove is all that's required.
func removeBinaryPlatform(binPath string) error {
	return osRemove(binPath)
}

// caseFold is the identity on case-sensitive filesystems. The windows build
// overrides it with strings.ToLower so path comparison is case-insensitive.
func caseFold(p string) string { return p }
