// update.go implements `grafel update` (issue #2213).
//
// Update downloads the latest (or a pinned) release artifact from GitHub,
// replaces the CLI binary atomically, and re-runs the install transaction
// (COPY or DEV mode, as recorded in install.json).
//
// The rollback path is:
//  1. Before overwriting, stash the current binary at <binPath>.prev
//  2. If the download or re-install fails, rename .prev back to the original
//  3. Remove the stash on success
//
// The update command is idempotent: running it a second time with the same
// tag is a fast no-op at the download step (checksums match).
package install

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// githubReleasesAPI is the GitHub API endpoint for release lookup.
	githubReleasesAPI = "https://api.github.com/repos/cajasmota/grafel/releases"

	// defaultUpdateTimeout is the HTTP timeout for GitHub API calls.
	defaultUpdateTimeout = 60 * time.Second
)

// UpdateOptions controls RunUpdate behaviour.
type UpdateOptions struct {
	// Tag is the release tag to install (e.g. "v1.2.3").
	// When empty, the latest stable release is used.
	Tag string

	// Pre, when true, allows pre-release tags in the "latest" resolution.
	// Only used when Tag is empty.
	Pre bool

	// BinPath is the path of the current grafel binary.
	// Defaults to os.Executable().
	BinPath string

	// StatePath is the path for install.json.
	// Defaults to DefaultStatePath().
	StatePath string

	// WorkingDir is used for git repo detection when re-running install.
	// Defaults to os.Getwd().
	WorkingDir string

	// SkillsSourceDir overrides skills discovery during re-install.
	SkillsSourceDir string

	// ClaudeConfigDirs overrides .claude.json auto-detection during re-install.
	ClaudeConfigDirs []string

	// Force bypasses the partial-install guard during re-install.
	Force bool

	// DryRun prints actions without writing anything.
	DryRun bool

	// SkipDaemonRestart skips the daemon restart step during re-install.
	// Useful in tests.
	SkipDaemonRestart bool

	// RestartDaemon is the injectable daemon restart function.
	RestartDaemon DaemonRestartFunc

	// HTTPClient is the HTTP client to use for downloading. When nil the
	// production client (defaultUpdateHTTPClient) is used.
	HTTPClient *http.Client

	// DownloadBinary is an injectable function that downloads the binary for
	// a given release tag and GOOS/GOARCH into destPath.  When nil the
	// production implementation (downloadReleaseBinary) is used.
	// Signature: func(client, tag, goos, goarch, destPath) error
	DownloadBinary func(client *http.Client, tag, goos, goarch, destPath string) error
}

// UpdateResult reports what RunUpdate accomplished.
type UpdateResult struct {
	// PreviousVersion is the SHA of the binary that was replaced.
	PreviousVersion string

	// NewVersion is the SHA of the newly installed binary.
	NewVersion string

	// Tag is the release tag that was installed.
	Tag string

	// InstallResult is the result of the re-install step.
	InstallResult *CopyResult

	// Skipped is true when the binary was already at the target version.
	Skipped bool
}

func (o *UpdateOptions) applyDefaults() error {
	if o.BinPath == "" {
		bin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve binary path: %w", err)
		}
		o.BinPath = bin
	}
	if o.StatePath == "" {
		p, err := DefaultStatePath()
		if err != nil {
			return err
		}
		o.StatePath = p
	}
	if o.WorkingDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve working dir: %w", err)
		}
		o.WorkingDir = cwd
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: defaultUpdateTimeout}
	}
	if o.DownloadBinary == nil {
		o.DownloadBinary = downloadReleaseBinary
	}
	return nil
}

// RunUpdate downloads the target release and atomically replaces the CLI
// binary, then re-runs the install transaction to refresh skills, MCP, and
// the daemon.
func RunUpdate(opts UpdateOptions) (*UpdateResult, error) {
	if err := opts.applyDefaults(); err != nil {
		return nil, err
	}

	// ── resolve target tag ────────────────────────────────────────────────────
	tag := opts.Tag
	if tag == "" {
		var err error
		tag, err = resolveLatestTag(opts.HTTPClient, opts.Pre)
		if err != nil {
			return nil, fmt.Errorf("resolve latest release tag: %w", err)
		}
	}

	result := &UpdateResult{Tag: tag}

	// ── read current state ────────────────────────────────────────────────────
	prevSHA, err := sha256File(opts.BinPath)
	if err != nil {
		return nil, fmt.Errorf("hash current binary: %w", err)
	}
	result.PreviousVersion = prevSHA

	// ── download to a temp file ───────────────────────────────────────────────
	tmpDownload := opts.BinPath + ".update-download"
	defer func() { _ = os.Remove(tmpDownload) }()

	if !opts.DryRun {
		if err := opts.DownloadBinary(opts.HTTPClient, tag, runtime.GOOS, runtime.GOARCH, tmpDownload); err != nil {
			return nil, fmt.Errorf("download release %s: %w", tag, err)
		}

		// Check if the downloaded binary is identical to what we already have.
		newSHA, err := sha256File(tmpDownload)
		if err != nil {
			return nil, fmt.Errorf("hash downloaded binary: %w", err)
		}
		result.NewVersion = newSHA

		if newSHA == prevSHA {
			result.Skipped = true
			return result, nil
		}
	} else {
		result.NewVersion = "(dry-run)"
	}

	// ── stash the current binary for rollback ─────────────────────────────────
	stashPath := opts.BinPath + ".prev"
	if !opts.DryRun {
		if err := copyFile(opts.BinPath, stashPath); err != nil {
			return nil, fmt.Errorf("stash current binary for rollback: %w", err)
		}
	}

	// rollbackBinary restores the stashed binary if something goes wrong.
	rollbackBinary := func() {
		if opts.DryRun {
			return
		}
		if err := os.Rename(stashPath, opts.BinPath); err != nil {
			fmt.Fprintf(os.Stderr, "grafel update: rollback failed — could not restore %s from %s: %v\n",
				opts.BinPath, stashPath, err)
			fmt.Fprintf(os.Stderr, "  Manual recovery: mv %s %s\n", stashPath, opts.BinPath)
		}
	}

	// ── atomically replace the binary ─────────────────────────────────────────
	if !opts.DryRun {
		// Preserve permissions of the original binary.
		info, err := os.Stat(opts.BinPath)
		if err != nil {
			rollbackBinary()
			return nil, fmt.Errorf("stat current binary: %w", err)
		}
		if err := os.Chmod(tmpDownload, info.Mode()); err != nil {
			rollbackBinary()
			return nil, fmt.Errorf("chmod downloaded binary: %w", err)
		}
		if err := os.Rename(tmpDownload, opts.BinPath); err != nil {
			rollbackBinary()
			return nil, fmt.Errorf("replace binary: %w", err)
		}
	}

	// ── re-run install ────────────────────────────────────────────────────────
	installOpts := CopyOptions{
		BinPath:           opts.BinPath,
		SkillsSourceDir:   opts.SkillsSourceDir,
		ClaudeConfigDirs:  opts.ClaudeConfigDirs,
		StatePath:         opts.StatePath,
		WorkingDir:        opts.WorkingDir,
		Force:             true, // update always re-installs
		DryRun:            opts.DryRun,
		SkipDaemonRestart: opts.SkipDaemonRestart,
		RestartDaemon:     opts.RestartDaemon,
	}

	installResult, err := RunCopy(installOpts)
	if err != nil {
		rollbackBinary()
		return nil, fmt.Errorf("re-install after update: %w", err)
	}
	result.InstallResult = installResult

	// ── remove stash on success ───────────────────────────────────────────────
	if !opts.DryRun {
		_ = os.Remove(stashPath)
	}

	return result, nil
}

// ── GitHub release helpers ────────────────────────────────────────────────────

// resolveLatestTag calls the GitHub releases API and returns the tag of the
// latest stable (or, when pre=true, latest including pre-release) release.
func resolveLatestTag(client *http.Client, pre bool) (string, error) {
	url := githubReleasesAPI + "/latest"
	if pre {
		// For pre-releases we must list all releases and pick the first.
		url = githubReleasesAPI + "?per_page=1"
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned HTTP %d for %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	// Simple JSON extraction without importing encoding/json (to keep deps light).
	// We only need the tag_name field.
	tag := extractJSONStringField(string(body), "tag_name")
	if tag == "" {
		return "", fmt.Errorf("could not extract tag_name from GitHub API response")
	}
	return tag, nil
}

// extractJSONStringField is a minimal JSON field extractor for a flat JSON
// object.  It handles the common case where the value is a double-quoted
// string.  This avoids encoding/json for a trivial parse of a single field.
func extractJSONStringField(json, field string) string {
	key := `"` + field + `":`
	idx := strings.Index(json, key)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(json[idx+len(key):])
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	// Find closing quote (simple: assume no escaped quotes in tag names).
	end := strings.Index(rest[1:], `"`)
	if end < 0 {
		return ""
	}
	return rest[1 : end+1]
}

// releaseAssetName builds the release-archive asset name for the given tag and
// GOOS/GOARCH. It MUST stay in sync with the "Package archive" step of
// .github/workflows/release.yml, which publishes goreleaser-style archives:
//
//	grafel_<version-without-v>_<oslabel>_<arch>.<ext>
//
// where:
//   - oslabel = linux | macos (GOOS=darwin) | windows
//   - arch    = x86_64 (GOARCH=amd64) | arm64
//   - ext     = zip for windows, tar.gz otherwise
//
// e.g.:
//
//	grafel_0.1.5_macos_arm64.tar.gz
//	grafel_0.1.5_linux_x86_64.tar.gz
//	grafel_0.1.5_windows_x86_64.zip
func releaseAssetName(tag, goos, goarch string) string {
	version := strings.TrimPrefix(tag, "v")

	osLabel := goos
	if goos == "darwin" {
		osLabel = "macos"
	}

	arch := goarch
	if goarch == "amd64" {
		arch = "x86_64"
	}

	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}

	return fmt.Sprintf("grafel_%s_%s_%s.%s", version, osLabel, arch, ext)
}

// binaryMemberName returns the name of the binary member inside the release
// archive for the given GOOS.
func binaryMemberName(goos string) string {
	if goos == "windows" {
		return "grafel.exe"
	}
	return "grafel"
}

// downloadReleaseBinary fetches the grafel release archive for the given
// tag/os/arch from the GitHub release assets, extracts the grafel binary
// member from it, and writes that member to destPath.
//
// release.yml ships ARCHIVES (not raw binaries): a .tar.gz (linux/macos) or
// .zip (windows) that bundles the grafel binary alongside LICENSE and
// README.md. We extract only the binary member and ignore the rest.
func downloadReleaseBinary(client *http.Client, tag, goos, goarch, destPath string) error {
	assetName := releaseAssetName(tag, goos, goarch)

	// First, get the release to find the asset download URL.
	releaseURL := fmt.Sprintf("%s/tags/%s", githubReleasesAPI, tag)
	req, err := http.NewRequest(http.MethodGet, releaseURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET release %s: %w", tag, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned HTTP %d for release %s", resp.StatusCode, tag)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read release response: %w", err)
	}

	// Find the download URL for our asset.
	downloadURL, err := findAssetDownloadURL(string(body), assetName)
	if err != nil {
		return fmt.Errorf("find asset %s in release %s: %w", assetName, tag, err)
	}

	// Download the binary.
	dlReq, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}

	dlResp, err := client.Do(dlReq)
	if err != nil {
		return fmt.Errorf("download %s: %w", downloadURL, err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d for %s", dlResp.StatusCode, downloadURL)
	}

	// Stream the archive to a temp file so we can extract from it (zip needs a
	// io.ReaderAt; tar/gzip stream fine but a temp file keeps both paths uniform
	// and bounds memory).
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}
	archiveTmp, err := os.CreateTemp(filepath.Dir(destPath), "grafel-archive-*"+filepath.Ext(assetName))
	if err != nil {
		return fmt.Errorf("create temp archive: %w", err)
	}
	archivePath := archiveTmp.Name()
	defer func() { _ = os.Remove(archivePath) }()

	if _, err := io.Copy(archiveTmp, dlResp.Body); err != nil {
		_ = archiveTmp.Close()
		return fmt.Errorf("download archive %s: %w", assetName, err)
	}
	if err := archiveTmp.Close(); err != nil {
		return fmt.Errorf("close temp archive: %w", err)
	}

	member := binaryMemberName(goos)
	if strings.HasSuffix(assetName, ".zip") {
		return extractZipMember(archivePath, member, destPath)
	}
	return extractTarGzMember(archivePath, member, destPath)
}

// extractTarGzMember extracts the file named member from the gzip-compressed
// tar archive at archivePath and writes it to destPath with the executable bit
// set. Other members (LICENSE, README.md) are ignored.
func extractTarGzMember(archivePath, member, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar stream: %w", err)
		}
		if path.Base(hdr.Name) != member || hdr.Typeflag != tar.TypeReg {
			continue
		}
		return writeBinaryMember(tr, destPath)
	}
	return fmt.Errorf("binary member %q not found in archive %s", member, filepath.Base(archivePath))
}

// extractZipMember extracts the file named member from the zip archive at
// archivePath and writes it to destPath with the executable bit set. Other
// members (LICENSE, README.md) are ignored.
func extractZipMember(archivePath, member, destPath string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip archive: %w", err)
	}
	defer zr.Close()

	for _, zf := range zr.File {
		if path.Base(zf.Name) != member || zf.FileInfo().IsDir() {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return fmt.Errorf("open zip member %q: %w", member, err)
		}
		err = writeBinaryMember(rc, destPath)
		_ = rc.Close()
		return err
	}
	return fmt.Errorf("binary member %q not found in archive %s", member, filepath.Base(archivePath))
}

// writeBinaryMember writes the extracted binary stream to destPath with the
// executable bit set (0o755).
func writeBinaryMember(src io.Reader, destPath string) error {
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create dest file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, src); err != nil {
		return fmt.Errorf("write binary: %w", err)
	}
	return nil
}

// ghRelease is a minimal projection of the GitHub Releases API response,
// covering only the asset name and download URL fields we need.
type ghRelease struct {
	Assets []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// findAssetDownloadURL parses a GitHub releases API JSON response and returns
// the browser_download_url for the asset whose name matches assetName.
//
// It unmarshals the response into a minimal struct rather than scanning the
// raw bytes: the real API places a large nested "uploader" object between an
// asset's "name" and "browser_download_url" fields, so a naive proximity scan
// misses the URL (and is brittle to field ordering and dotted asset names).
func findAssetDownloadURL(body, assetName string) (string, error) {
	var rel ghRelease
	if err := json.Unmarshal([]byte(body), &rel); err != nil {
		return "", fmt.Errorf("parse release JSON: %w", err)
	}

	for _, asset := range rel.Assets {
		if asset.Name == assetName {
			if asset.BrowserDownloadURL == "" {
				return "", fmt.Errorf("asset %q has no browser_download_url", assetName)
			}
			return asset.BrowserDownloadURL, nil
		}
	}

	return "", fmt.Errorf("asset %q not found in release", assetName)
}
