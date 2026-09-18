//go:build !(darwin || freebsd || netbsd || openbsd || dragonfly)

package watch

// backendSkipsPipesAndSockets is false everywhere fsnotify does not select its
// kqueue backend. See the kqueue-side file for why this is an allow-list and
// what each other backend does instead (#7245).
const backendSkipsPipesAndSockets = false
