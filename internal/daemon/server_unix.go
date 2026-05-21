//go:build !windows

package daemon

import "os"

// chmodSocket sets 0600 permissions on the Unix-domain socket file so only
// the owner can connect. Returns an error if chmod fails; the caller is
// responsible for closing the listener on error.
func chmodSocket(socketPath string) error {
	return os.Chmod(socketPath, 0o600)
}
