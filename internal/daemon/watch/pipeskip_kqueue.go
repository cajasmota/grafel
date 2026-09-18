//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package watch

// backendSkipsPipesAndSockets records that fsnotify's kqueue backend refuses to
// watch a FIFO or a unix-domain socket: addWatch returns ("", nil) — success
// with no watch established — as soon as os.Lstat reports os.ModeNamedPipe or
// os.ModeSocket (backend_kqueue.go:365-368). No descriptor is opened, so grafel
// must not charge one (#7245).
//
// An ALLOW-LIST of the platforms whose backend has been read for this property,
// not "not Linux". fsnotify selects backend_kqueue.go on exactly these five
// GOOS values (backend_kqueue.go's own build tag), and every other backend must
// inherit "charge as before" rather than a guarantee nobody has checked:
//
//   - inotify (linux) takes no per-entry descriptor at all, and reports a FIFO's
//     removal through the directory's own watch — the charge and the release are
//     symmetric, so skipping the charge here would turn an over-count into the
//     #6268 under-count.
//   - ReadDirectoryChangesW (windows) holds one handle per tree; its perEntry()
//     is 0, so this constant would change nothing there even if it were set.
//   - fen (solaris, illumos) has not been read for this property.
const backendSkipsPipesAndSockets = true
