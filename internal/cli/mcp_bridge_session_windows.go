package cli

// stdinIdentity has no cheap device/inode equivalent on Windows: os.Stdin's
// Sys() carries file attributes, not an identity. Returning ("", false) makes
// bridgeSessionID fall back to this process's pid, which is unique by
// construction — one record per bridge. That is strictly safe here because the
// record gates nothing but a log line (#6999).
func stdinIdentity() (string, bool) {
	return "", false
}
