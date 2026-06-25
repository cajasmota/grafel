package install

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/executil"
)

// grafelGitignoreEntry is the line we append to .gitignore when the
// user runs `grafel install` inside a git repo (issue #2207).
const grafelGitignoreEntry = "/.grafel/"

// EnsureGitignore appends grafelGitignoreEntry to the .gitignore at
// repoRoot if it is not already present. It is idempotent: if the exact
// entry already appears on any line of the file (modulo leading/trailing
// whitespace), the file is not modified.
//
// If the .gitignore file does not exist it is created.
// Returns the absolute path of the .gitignore file on success so callers
// can include it in state tracking, or an empty string + error on failure.
func EnsureGitignore(repoRoot string) (string, error) {
	gitignorePath := filepath.Join(repoRoot, ".gitignore")

	// Read existing content (tolerate missing file).
	existing, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read .gitignore %s: %w", gitignorePath, err)
	}

	// Check whether the entry is already present.
	if hasGitignoreEntry(existing, grafelGitignoreEntry) {
		return gitignorePath, nil
	}

	// Append the entry. Ensure there's a newline before the entry if the
	// file is non-empty and does not end with a newline.
	var buf bytes.Buffer
	buf.Write(existing)
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		buf.WriteByte('\n')
	}
	buf.WriteString(grafelGitignoreEntry)
	buf.WriteByte('\n')

	if err := os.WriteFile(gitignorePath, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("write .gitignore %s: %w", gitignorePath, err)
	}
	return gitignorePath, nil
}

// hasGitignoreEntry reports whether content already contains entry as a
// standalone line (leading/trailing whitespace stripped before comparison).
func hasGitignoreEntry(content []byte, entry string) bool {
	entry = strings.TrimSpace(entry)
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == entry {
			return true
		}
	}
	return false
}

// DetectGitRepo returns the root of the git working tree that contains
// cwd, or ("", false) if cwd is not inside a git repo.
//
// Detection is performed by running `git rev-parse --show-toplevel`
// rather than walking the filesystem, so it respects gitdir configs and
// worktrees correctly. The function returns (root, true) on success and
// always tolerates the case where git is not installed (returns false).
func DetectGitRepo(cwd string) (string, bool) {
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	executil.NoWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		// Not a git repo, or git not installed — both are fine; caller handles.
		return "", false
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", false
	}
	return root, true
}
