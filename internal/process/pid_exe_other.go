//go:build !windows

package process

// pidExeBaseName returns the basename of the executable backing pid and true
// when the platform can answer it via a cheap, pid-targeted primitive.
//
// On non-Windows platforms PidIsGrafel already resolves the process via
// FindByName (ps/proc), so there is no separate single-pid primitive worth
// wiring here: returning (_, false) tells PidIsGrafel to use its FindByName
// path unchanged. The Windows build provides the real implementation, where
// per-pid OpenProcess + QueryFullProcessImageName is available even though
// full process enumeration is not.
func pidExeBaseName(_ int) (string, bool) {
	return "", false
}
