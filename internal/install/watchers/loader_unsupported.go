//go:build !darwin && !linux && !windows

package watchers

import "errors"

var errLoaderUnsupported = errors.New(
	"watcher loader is not supported on this platform; unit file is written but not activated",
)

type unsupportedLoader struct{}

// NewLoader returns a no-op Loader for unsupported platforms.
func NewLoader() Loader { return unsupportedLoader{} }

func (unsupportedLoader) Load(_ Unit) error   { return errLoaderUnsupported }
func (unsupportedLoader) Unload(_ Unit) error { return nil }
func (unsupportedLoader) Status(u Unit) (WatcherStatus, error) {
	return WatcherStatus{TaskName: u.Label()}, nil
}
