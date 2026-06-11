package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// #4468: dashboard embed staleness guard.
//
// `make dashboard-build` builds the SPA into webui-v2/dist and then copies it
// into internal/dashboard/dist so that `//go:embed dist` (static.go) embeds the
// fresh bundle. A manual / CI sequence that runs `vite build` followed by
// `go build` WITHOUT the copy step silently re-embeds the OLD internal dist —
// the daemon then serves a stale UI while reporting the new commit.
//
// DistDirsMatch compares the freshly-built bundle (builtDir, e.g.
// webui-v2/dist) against the embedded bundle (embeddedDir, e.g.
// internal/dashboard/dist) by content hash. It is the engine behind the
// `make verify-dashboard` CI guard and is exercised by unit tests with temp
// dirs (no real npm build required).

// distFingerprint hashes every regular file under dir (recursively) into a
// single stable digest. The PLACEHOLDER.md sentinel is ignored so that an
// unbuilt embedded dir is reported as "placeholder", not as a content mismatch.
// Returns ("", nil) when the dir is missing or contains only the placeholder.
func distFingerprint(dir string) (string, error) {
	type fileHash struct {
		rel string
		sum string
	}
	var files []fileHash

	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		// Ignore the checked-in sentinel — it is not part of the SPA bundle.
		if rel == "PLACEHOLDER.md" {
			return nil
		}
		f, oerr := os.Open(path)
		if oerr != nil {
			return oerr
		}
		defer f.Close()
		h := sha256.New()
		if _, cerr := io.Copy(h, f); cerr != nil {
			return cerr
		}
		files = append(files, fileHash{rel: rel, sum: hex.EncodeToString(h.Sum(nil))})
		return nil
	})
	if walkErr != nil {
		if os.IsNotExist(walkErr) {
			return "", nil
		}
		return "", walkErr
	}
	if len(files) == 0 {
		return "", nil
	}

	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	agg := sha256.New()
	for _, fh := range files {
		fmt.Fprintf(agg, "%s\x00%s\x00", fh.rel, fh.sum)
	}
	return hex.EncodeToString(agg.Sum(nil)), nil
}

// DistStalenessResult describes the outcome of a staleness comparison.
type DistStalenessResult struct {
	// Match is true when the embedded bundle is byte-identical to the built one.
	Match bool
	// EmbeddedEmpty is true when the embedded dir is missing or holds only the
	// placeholder (never built / never copied).
	EmbeddedEmpty bool
	// BuiltEmpty is true when the built dir is missing or empty (SPA not built).
	BuiltEmpty bool
	// BuiltFingerprint / EmbeddedFingerprint are the aggregate content digests.
	BuiltFingerprint    string
	EmbeddedFingerprint string
	// Reason is a human-readable explanation, suitable for a CI failure message.
	Reason string
}

// DistDirsMatch compares the freshly-built SPA bundle (builtDir, e.g.
// webui-v2/dist) against the embedded bundle (embeddedDir, e.g.
// internal/dashboard/dist) and reports whether the embed is up to date.
//
// It returns an error only on I/O failure. A stale embed is NOT an error — it
// is reported via DistStalenessResult.Match=false so the caller decides
// severity (the CLI guard exits non-zero; tests assert the flag).
func DistDirsMatch(builtDir, embeddedDir string) (DistStalenessResult, error) {
	built, err := distFingerprint(builtDir)
	if err != nil {
		return DistStalenessResult{}, fmt.Errorf("fingerprint built dir %s: %w", builtDir, err)
	}
	embedded, err := distFingerprint(embeddedDir)
	if err != nil {
		return DistStalenessResult{}, fmt.Errorf("fingerprint embedded dir %s: %w", embeddedDir, err)
	}

	res := DistStalenessResult{
		BuiltFingerprint:    built,
		EmbeddedFingerprint: embedded,
		BuiltEmpty:          built == "",
		EmbeddedEmpty:       embedded == "",
	}

	switch {
	case res.BuiltEmpty:
		// Nothing was built to compare against — the guard cannot judge
		// staleness, so it passes (a Go-only / pre-built-CI flow). Callers that
		// require a built dir should check BuiltEmpty.
		res.Match = true
		res.Reason = fmt.Sprintf("no built bundle at %s; nothing to compare (skipping staleness check)", builtDir)
	case res.EmbeddedEmpty:
		res.Match = false
		res.Reason = fmt.Sprintf(
			"embedded bundle %s is empty/placeholder but a fresh build exists at %s; run `make dashboard-build` (or copy webui-v2/dist) so the embed is not stale",
			embeddedDir, builtDir)
	case built == embedded:
		res.Match = true
		res.Reason = "embedded dashboard bundle matches the built bundle"
	default:
		res.Match = false
		res.Reason = fmt.Sprintf(
			"STALE dashboard embed: %s (%s) differs from the freshly built %s (%s); run `make dashboard-build` so `go build` embeds the current UI — otherwise the daemon serves an old dashboard while reporting the new commit",
			embeddedDir, short(embedded), builtDir, short(built))
	}
	return res, nil
}

func short(sum string) string {
	if len(sum) <= 12 {
		return sum
	}
	return sum[:12]
}

// VerifyDashboardEmbed is the entry point for the `make verify-dashboard` CI
// guard. It compares webui-v2/dist against internal/dashboard/dist relative to
// repoRoot and returns a non-nil error when the embed is stale.
func VerifyDashboardEmbed(repoRoot string) error {
	builtDir := filepath.Join(repoRoot, "webui-v2", "dist")
	embeddedDir := filepath.Join(repoRoot, "internal", "dashboard", "dist")
	res, err := DistDirsMatch(builtDir, embeddedDir)
	if err != nil {
		return err
	}
	if !res.Match {
		return fmt.Errorf("%s", res.Reason)
	}
	if strings.Contains(res.Reason, "skipping") {
		fmt.Fprintln(os.Stderr, "verify-dashboard: "+res.Reason)
	}
	return nil
}
