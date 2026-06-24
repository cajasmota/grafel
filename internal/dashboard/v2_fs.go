// v2_fs.go — server-side filesystem directory browser for the create-group
// ScanWizard (#1529).
//
// The browser File System Access API (showDirectoryPicker) only yields an
// opaque FileSystemDirectoryHandle with NO real on-disk path, so the wizard
// could never tell the daemon WHICH directory to index — leaving the user
// stuck at "paste its full path to continue". Because the daemon runs on the
// SAME machine as the browser (localhost) and already indexes arbitrary local
// paths, it can list its OWN filesystem so the UI can present a real folder
// browser: navigating a folder yields its ABSOLUTE path, and selecting it is
// sufficient to proceed — no manual typing required.
//
//	GET /api/v2/fs/list?path=<abs>  → list the subdirectories of <path>
//
// When path is empty we default to a sensible start: the user's home dir,
// surfacing common project roots ($HOME/Documents, $HOME/Documents/Projects)
// as shortcuts. We list DIRECTORIES ONLY (never file contents). Errors
// (nonexistent path, permission denied) are reported gracefully in the
// envelope rather than as HTTP failures, so the UI can show a message and let
// the user navigate elsewhere.

package dashboard

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// drivesSentinel is the virtual path representing the "drives level" above the
// per-drive roots on Windows (C:\, D:\, …). Listing it returns one entry per
// available drive; navigating "up" from a drive root yields it. It is never a
// real on-disk path, so it is meaningless (and unreachable) off Windows.
const drivesSentinel = "drives:"

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// v2FsEntry is one subdirectory under the listed path.
type v2FsEntry struct {
	// Name is the directory basename (e.g. "Projects").
	Name string `json:"name"`
	// Path is the absolute on-disk path (what the daemon will index).
	Path string `json:"path"`
	// IsDir is always true (we list directories only) but kept explicit so the
	// shape is self-describing for the frontend.
	IsDir bool `json:"isDir"`
	// Hidden is true for dot-directories (.git, .grafel, …).
	Hidden bool `json:"hidden"`
}

// v2FsShortcut is a convenience jump target (home, Documents, Projects).
type v2FsShortcut struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// v2FsListReply is the response for GET /api/v2/fs/list.
type v2FsListReply struct {
	// Path is the resolved absolute path that was listed.
	Path string `json:"path"`
	// Parent is the absolute parent path ("" when at the filesystem root).
	Parent string `json:"parent"`
	// Entries are the immediate subdirectories of Path, sorted by name.
	Entries []v2FsEntry `json:"entries"`
	// Shortcuts are quick-jump targets (only populated for the default/home view).
	Shortcuts []v2FsShortcut `json:"shortcuts,omitempty"`
	// Drives lists the available drive roots on Windows (C:\, D:\, …). It is
	// populated on EVERY listing so the frontend can render a drive selector and
	// let the user switch drives without first navigating up. Empty off Windows.
	Drives []v2FsEntry `json:"drives,omitempty"`
	// IsDrives is true when Path is the virtual "drives level" (Windows only):
	// Entries are the drive roots themselves and there is no parent.
	IsDrives bool `json:"isDrives,omitempty"`
	// Error is a human-readable reason when the path could not be listed
	// (nonexistent / permission denied). Entries is empty in that case.
	Error string `json:"error,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/v2/fs/list — list subdirectories of an absolute path
// ─────────────────────────────────────────────────────────────────────────────

// handleV2FsList lists the immediate subdirectories of the requested path so
// the WebUI v2 ScanWizard can present a server-side folder browser. When no
// path is supplied it defaults to the daemon's home directory and surfaces
// common project roots as shortcuts. Localhost-only by deployment (the daemon
// already indexes arbitrary local paths), so no path is out of bounds.
func (s *Server) handleV2FsList(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("path"))

	home, _ := os.UserHomeDir()

	// Windows "drives level": list the available drive roots so the user can
	// switch C:→D: etc. This sits above every drive root; it has no parent.
	if drivesSupported && raw == drivesSentinel {
		drives := listDrives()
		writeV2JSON(w, http.StatusOK, v2OK(v2FsListReply{
			Path:     drivesSentinel,
			Parent:   "",
			Entries:  drives,
			Drives:   drives,
			IsDrives: true,
		}))
		return
	}

	// Default start: the user's home directory.
	if raw == "" {
		raw = home
	}

	abs, err := expandPath(raw)
	if err != nil {
		writeV2JSON(w, http.StatusOK, v2OK(v2FsListReply{
			Path:  raw,
			Error: "cannot resolve path: " + err.Error(),
		}))
		return
	}

	info, statErr := os.Stat(abs)
	if statErr != nil || !info.IsDir() {
		msg := "path does not exist or is not a directory"
		if statErr != nil {
			msg = statErr.Error()
		}
		writeV2JSON(w, http.StatusOK, v2OK(v2FsListReply{
			Path:   abs,
			Parent: parentOrEmpty(abs),
			Error:  msg,
		}))
		return
	}

	dirEntries, readErr := os.ReadDir(abs)
	if readErr != nil {
		writeV2JSON(w, http.StatusOK, v2OK(v2FsListReply{
			Path:   abs,
			Parent: parentOrEmpty(abs),
			Error:  "cannot read directory: " + readErr.Error(),
		}))
		return
	}

	entries := make([]v2FsEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		// Directories only — never file contents. Symlinks that point at a
		// directory are included via Stat so users can follow them.
		if !de.IsDir() {
			if de.Type()&os.ModeSymlink == 0 {
				continue
			}
			target, terr := os.Stat(filepath.Join(abs, de.Name()))
			if terr != nil || !target.IsDir() {
				continue
			}
		}
		name := de.Name()
		entries = append(entries, v2FsEntry{
			Name:   name,
			Path:   filepath.Join(abs, name),
			IsDir:  true,
			Hidden: strings.HasPrefix(name, "."),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	reply := v2FsListReply{
		Path:    abs,
		Parent:  parentOrEmpty(abs),
		Entries: entries,
	}

	// Surface common project roots only on the home view so the UI can offer
	// one-click jumps without the user typing.
	if home != "" && abs == home {
		reply.Shortcuts = homeShortcuts(home)
	}

	// On Windows attach the available drives to EVERY listing (so the UI can
	// render a drive selector) and ensure "up" from a drive root ascends to the
	// drives level rather than dead-ending on the same drive.
	if drivesSupported {
		reply.Drives = listDrives()
		if isDriveRoot(abs) {
			reply.Parent = drivesSentinel
		}
	}

	writeV2JSON(w, http.StatusOK, v2OK(reply))
}

// parentOrEmpty returns the parent directory of abs, or "" when abs is already
// the filesystem root (so the UI can disable the "up" control).
func parentOrEmpty(abs string) string {
	parent := filepath.Dir(abs)
	if parent == abs {
		return ""
	}
	return parent
}

// homeShortcuts returns the quick-jump targets that actually exist on disk.
func homeShortcuts(home string) []v2FsShortcut {
	candidates := []v2FsShortcut{
		{Label: "Home", Path: home},
		{Label: "Documents", Path: filepath.Join(home, "Documents")},
		{Label: "Projects", Path: filepath.Join(home, "Documents", "Projects")},
	}
	out := make([]v2FsShortcut, 0, len(candidates))
	for _, c := range candidates {
		if info, err := os.Stat(c.Path); err == nil && info.IsDir() {
			out = append(out, c)
		}
	}
	return out
}
