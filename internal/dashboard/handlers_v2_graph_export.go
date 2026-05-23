// handlers_v2_graph_export.go — WebUI v2 GRAPH export + import endpoints.
//
// Parallel to handlers_v2_docs.go (#1658) which exports generated markdown
// docs, these endpoints export/import the GRAPH data — the indexed store
// contents (graph.fb, graph.json, enrichments, links, metadata, embeddings)
// plus the fleet config — so a group can be backed up, transferred
// machine-to-machine, or shared.
//
// Routes (registered in server.go):
//
//	GET  /api/v2/groups/{group}/export?format=zip&kind=graph|all  → handleV2GraphExport
//	POST /api/v2/groups/import?force=true&name=<override>         → handleV2GraphImport
//
// Archive layout (extensible — see ExportManifest.Version):
//
//	<group>/manifest.json                — ExportManifest (version + group + repos)
//	<group>/fleet.json                   — copy of the per-group config
//	<group>/store/<repoSlug>/...         — entire daemon store dir for each repo
//	                                       (graph.fb, graph.json, enrichments,
//	                                       links, metadata, embeddings.bin, ...)
//	<group>/docs/<repoSlug>/...          — generated technical docs   (kind=all only)
//	<group>/docs/business/...            — generated business docs    (kind=all only)
//
// On import the daemon:
//   1. unpacks the archive into a temp dir,
//   2. validates the manifest + required layout,
//   3. relocates each store payload into the local daemon's store dir
//      (`$ARCHIGRAPH_HOME/store/<slug>-<hash>`), keyed by repo path,
//   4. writes the fleet config to its canonical path,
//   5. registers the group with the registry,
//   6. (optionally) copies generated docs into the docs store.
//
// The endpoint refuses if the target group is already registered unless
// `force=true` is set. `name=<override>` lets a caller import the archive
// under a different group slug — useful when comparing a backup against the
// live group without overwriting it.
//
// The graph cache is invalidated for the imported group so the next read
// re-loads from the freshly restored store.

package dashboard

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/registry"
)

// exportManifestVersion is the current archive format version. Bump when
// the on-disk layout changes in a way an old importer cannot handle.
const exportManifestVersion = 1

// maxImportArchiveBytes caps the body size accepted by the import endpoint
// (1 GiB). Big graphs are real — embeddings + flatbuffer + JSON sidecars
// for a polyglot platform can be hundreds of MB — so the cap is generous
// but bounded to keep the daemon from being trivially DoS-ed.
const maxImportArchiveBytes = 1 << 30

// ExportManifest is the small json sidecar packed at the top of every
// graph archive. It lets the importer validate compatibility without
// having to parse the fleet config first, and records useful provenance.
type ExportManifest struct {
	Version    int               `json:"version"`
	Kind       string            `json:"kind"` // "graph" or "all"
	Group      string            `json:"group"`
	ExportedAt string            `json:"exported_at"` // RFC3339 UTC
	Repos      []ExportRepoEntry `json:"repos"`
}

// ExportRepoEntry records one repo's identity inside the archive so the
// importer can map archive paths back to a registry.Repo entry without
// re-deriving slug hashes.
type ExportRepoEntry struct {
	Slug     string `json:"slug"`
	Path     string `json:"path"` // original absolute path on the exporting host
	Stack    string `json:"stack,omitempty"`
	CloneURL string `json:"clone_url,omitempty"`
}

// ──────────────────────────────────────────────────────────────────────────
// Export
// ──────────────────────────────────────────────────────────────────────────

// handleV2GraphExport — GET /api/v2/groups/{group}/export?format=zip&kind=graph|all
//
// Streams a zip archive of the graph store for every repo in the group
// plus the fleet config. With kind=all the generated docs (technical +
// business) are bundled too. Designed to be extensible: additional kinds
// or formats can be added without breaking the existing contract.
func (s *Server) handleV2GraphExport(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	if groupName == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	configPath, err := groupConfigPath(groupName)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", "group not found: "+groupName)
		return
	}
	cfg, err := registry.LoadGroupConfig(configPath)
	if err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", "load fleet: "+err.Error())
		return
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "zip"
	}
	if format != "zip" {
		writeV2Err(w, http.StatusBadRequest, "bad_request",
			"unsupported format: "+format+" (supported: zip)")
		return
	}

	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind == "" {
		kind = "graph"
	}
	switch kind {
	case "graph", "all":
	default:
		writeV2Err(w, http.StatusBadRequest, "bad_request",
			"unsupported kind: "+kind+" (supported: graph, all)")
		return
	}

	// Build the manifest from the live fleet config so import doesn't have
	// to recompute slugs.
	manifest := ExportManifest{
		Version:    exportManifestVersion,
		Kind:       kind,
		Group:      groupName,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Repos:      make([]ExportRepoEntry, 0, len(cfg.Repos)),
	}
	for _, rp := range cfg.Repos {
		manifest.Repos = append(manifest.Repos, ExportRepoEntry{
			Slug:     rp.Slug,
			Path:     rp.Path,
			Stack:    rp.Stack,
			CloneURL: rp.CloneURL,
		})
	}

	filename := fmt.Sprintf("%s-graph-%s.zip", groupName,
		time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	zw := zip.NewWriter(w)
	defer zw.Close()

	// 1) Manifest at the archive top.
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	if err := writeZipFile(zw, groupName+"/manifest.json", manifestBytes); err != nil {
		return
	}

	// 2) Fleet config copy.
	if raw, ferr := os.ReadFile(configPath); ferr == nil {
		if err := writeZipFile(zw, groupName+"/fleet.json", raw); err != nil {
			return
		}
	}

	// 3) Per-repo store directories.
	// Sort repos by slug for deterministic archives (helps test snapshots).
	repos := append([]registry.Repo(nil), cfg.Repos...)
	sort.Slice(repos, func(i, j int) bool { return repos[i].Slug < repos[j].Slug })
	for _, rp := range repos {
		stateDir := daemon.StateDirForRepo(rp.Path)
		if !dirHasContent(stateDir) {
			// Skip repos that have never been indexed — keep the archive clean.
			continue
		}
		prefix := groupName + "/store/" + rp.Slug
		if err := zipAddTree(zw, stateDir, prefix); err != nil {
			// Best-effort: response already streaming; abort.
			return
		}
	}

	// 4) kind=all also bundles generated docs (mirrors #1658 docs export).
	if kind == "all" {
		// Technical docs per repo (only the store layout — pre-#1624 in-repo
		// docs are out of scope; the canonical store wins).
		for _, rp := range repos {
			docsDir := daemon.RepoDocsDir(groupName, rp.Slug)
			if !dirHasContent(docsDir) {
				continue
			}
			if err := zipAddTree(zw, docsDir,
				groupName+"/docs/"+rp.Slug); err != nil {
				return
			}
		}
		// Business docs (group-level).
		if bizDir := daemon.BusinessDocsDir(groupName); dirHasContent(bizDir) {
			if err := zipAddTree(zw, bizDir,
				groupName+"/docs/business"); err != nil {
				return
			}
		}
	}
}

// writeZipFile is a tiny helper for stuffing in-memory bytes (manifest +
// fleet copy) into the archive. Uses Deflate to match zipAddTree.
func writeZipFile(zw *zip.Writer, name string, data []byte) error {
	fh := &zip.FileHeader{
		Name:     name,
		Method:   zip.Deflate,
		Modified: time.Now(),
	}
	w, err := zw.CreateHeader(fh)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// ──────────────────────────────────────────────────────────────────────────
// Import
// ──────────────────────────────────────────────────────────────────────────

// v2GraphImportReply is the success envelope payload.
type v2GraphImportReply struct {
	Group  string   `json:"group"`
	Repos  []string `json:"repos"`
	Forced bool     `json:"forced"`
}

// handleV2GraphImport — POST /api/v2/groups/import
//
// Body: a zip archive produced by handleV2GraphExport (any kind).
// Content-Type may be application/zip OR multipart/form-data with a `file`
// field (the WebUI uses the latter so a file picker can attach the upload).
//
// Query params:
//   - force=true        — overwrite an existing group of the same name.
//   - name=<override>   — register the archive under a different group slug.
//
// On success the daemon: writes the fleet config to its canonical path,
// restores each repo's store dir under the local daemon's store root,
// registers the group with the registry, and (when present) restores the
// docs trees. The graph cache for the imported group is invalidated so the
// next read loads fresh state.
func (s *Server) handleV2GraphImport(w http.ResponseWriter, r *http.Request) {
	force := strings.EqualFold(r.URL.Query().Get("force"), "true")
	nameOverride := strings.TrimSpace(r.URL.Query().Get("name"))

	body, cleanup, err := readImportBody(r)
	if err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	defer cleanup()

	zr, err := zip.NewReader(body, body.Size())
	if err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request",
			"invalid zip archive: "+err.Error())
		return
	}

	// Validate layout: locate manifest.json under a single top-level group
	// directory.
	manifest, archiveGroup, err := readManifestFromZip(zr)
	if err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if manifest.Version > exportManifestVersion {
		writeV2Err(w, http.StatusBadRequest, "unsupported_version",
			fmt.Sprintf("manifest version %d is newer than this daemon supports (%d)",
				manifest.Version, exportManifestVersion))
		return
	}

	// Decide on the final group name.
	finalGroup := manifest.Group
	if nameOverride != "" {
		finalGroup = nameOverride
	}
	if finalGroup == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "manifest is missing group name")
		return
	}

	// Conflict check.
	existingPath, _ := groupConfigPath(finalGroup)
	if existingPath != "" && !force {
		writeV2Err(w, http.StatusConflict, "conflict",
			fmt.Sprintf("group %q already exists; pass force=true to overwrite", finalGroup))
		return
	}

	// Load the bundled fleet config and apply the group-name override + any
	// slug-keyed restore mapping.
	bundledFleetPath := archiveGroup + "/fleet.json"
	cfg, err := readFleetFromZip(zr, bundledFleetPath)
	if err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request",
			"missing or invalid fleet.json in archive: "+err.Error())
		return
	}
	cfg.Name = finalGroup

	// Restore each repo's store dir into the local daemon's store layout.
	// We key the destination by the ORIGINAL absolute path recorded in the
	// manifest (and stored on the fleet's Repo.Path). This preserves the
	// install-local layout: re-importing onto the same host hits the same
	// store dir; importing onto a different host keeps repos addressable
	// via their original paths (and the user can rebind via the Settings
	// "Add repo" flow if the path differs on the new host).
	for _, rp := range cfg.Repos {
		archivePrefix := archiveGroup + "/store/" + rp.Slug + "/"
		destStateDir := daemon.StateDirForRepo(rp.Path)
		if destStateDir == "" {
			continue
		}
		if err := os.MkdirAll(destStateDir, 0o755); err != nil {
			writeV2Err(w, http.StatusInternalServerError, "internal_error",
				"mkdir state dir: "+err.Error())
			return
		}
		if err := extractZipPrefix(zr, archivePrefix, destStateDir); err != nil {
			writeV2Err(w, http.StatusInternalServerError, "internal_error",
				"restore store for "+rp.Slug+": "+err.Error())
			return
		}
	}

	// Restore docs trees if the archive contained them (kind=all export).
	if manifest.Kind == "all" {
		for _, rp := range cfg.Repos {
			archivePrefix := archiveGroup + "/docs/" + rp.Slug + "/"
			destDocsDir := daemon.RepoDocsDir(finalGroup, rp.Slug)
			if destDocsDir == "" || !zipPrefixExists(zr, archivePrefix) {
				continue
			}
			if err := os.MkdirAll(destDocsDir, 0o755); err != nil {
				writeV2Err(w, http.StatusInternalServerError, "internal_error",
					"mkdir docs dir: "+err.Error())
				return
			}
			if err := extractZipPrefix(zr, archivePrefix, destDocsDir); err != nil {
				writeV2Err(w, http.StatusInternalServerError, "internal_error",
					"restore docs for "+rp.Slug+": "+err.Error())
				return
			}
		}
		bizPrefix := archiveGroup + "/docs/business/"
		if zipPrefixExists(zr, bizPrefix) {
			destBiz := daemon.BusinessDocsDir(finalGroup)
			if destBiz != "" {
				_ = os.MkdirAll(destBiz, 0o755)
				if err := extractZipPrefix(zr, bizPrefix, destBiz); err != nil {
					writeV2Err(w, http.StatusInternalServerError, "internal_error",
						"restore business docs: "+err.Error())
					return
				}
			}
		}
	}

	// Persist the fleet config + register the group.
	finalConfigPath, err := registry.ConfigPathFor(finalGroup)
	if err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error",
			"resolve config path: "+err.Error())
		return
	}
	if err := registry.SaveGroupConfig(finalConfigPath, cfg); err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error",
			"save fleet: "+err.Error())
		return
	}
	if err := registry.AddGroup(finalGroup, finalConfigPath); err != nil {
		writeV2Err(w, http.StatusInternalServerError, "internal_error",
			"register group: "+err.Error())
		return
	}

	// Bust the cached graph for this group so the next read picks up the
	// freshly restored store. Non-fatal if the cache layer is absent
	// (e.g. fresh import for a never-loaded group).
	if s.graphs != nil {
		s.graphs.Invalidate(finalGroup)
	}

	repos := make([]string, 0, len(cfg.Repos))
	for _, rp := range cfg.Repos {
		repos = append(repos, rp.Slug)
	}
	writeV2JSON(w, http.StatusOK, v2OK(v2GraphImportReply{
		Group:  finalGroup,
		Repos:  repos,
		Forced: existingPath != "" && force,
	}))
}

// ──────────────────────────────────────────────────────────────────────────
// Import helpers
// ──────────────────────────────────────────────────────────────────────────

// sizedReaderAt is the zip.NewReader contract: a ReaderAt with known size.
type sizedReaderAt struct {
	io.ReaderAt
	size int64
}

func (s *sizedReaderAt) Size() int64 { return s.size }

// readImportBody pulls the zip bytes out of either a raw application/zip
// body or a multipart/form-data upload (the WebUI uses the multipart form).
// Returns a sized ReaderAt over the bytes + a cleanup callback.
//
// The body is bounded by maxImportArchiveBytes — anything bigger is
// rejected to keep the daemon from being trivially DoS-ed.
func readImportBody(r *http.Request) (*sizedReaderAt, func(), error) {
	ct := r.Header.Get("Content-Type")
	noop := func() {}

	if strings.HasPrefix(ct, "multipart/form-data") {
		// Up to maxImportArchiveBytes — multipart parses lazily so cap reader.
		r.Body = http.MaxBytesReader(nil, r.Body, maxImportArchiveBytes)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil, noop, fmt.Errorf("parse multipart: %w", err)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			return nil, noop, fmt.Errorf("missing 'file' form field: %w", err)
		}
		defer file.Close()
		tmp, err := os.CreateTemp("", "archigraph-import-*.zip")
		if err != nil {
			return nil, noop, err
		}
		if _, err := io.Copy(tmp, file); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return nil, noop, err
		}
		fi, err := tmp.Stat()
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return nil, noop, err
		}
		cleanup := func() {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
		return &sizedReaderAt{ReaderAt: tmp, size: fi.Size()}, cleanup, nil
	}

	// Default path: treat the body as raw zip bytes.
	r.Body = http.MaxBytesReader(nil, r.Body, maxImportArchiveBytes)
	tmp, err := os.CreateTemp("", "archigraph-import-*.zip")
	if err != nil {
		return nil, noop, err
	}
	if _, err := io.Copy(tmp, r.Body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, noop, err
	}
	fi, err := tmp.Stat()
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, noop, err
	}
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}
	return &sizedReaderAt{ReaderAt: tmp, size: fi.Size()}, cleanup, nil
}

// readManifestFromZip locates the top-level <group>/manifest.json entry
// and returns the parsed manifest plus the archive's top-level group dir
// (which may differ from the eventual register-as group name).
func readManifestFromZip(zr *zip.Reader) (ExportManifest, string, error) {
	var manifestEntry *zip.File
	for _, f := range zr.File {
		// Look for exactly one "<X>/manifest.json" at depth 1.
		name := filepath.ToSlash(f.Name)
		parts := strings.SplitN(name, "/", 2)
		if len(parts) == 2 && parts[1] == "manifest.json" {
			if manifestEntry != nil {
				return ExportManifest{}, "",
					errors.New("archive contains multiple manifest.json files")
			}
			manifestEntry = f
		}
	}
	if manifestEntry == nil {
		return ExportManifest{}, "",
			errors.New("archive is missing <group>/manifest.json")
	}
	rc, err := manifestEntry.Open()
	if err != nil {
		return ExportManifest{}, "", err
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return ExportManifest{}, "", err
	}
	var m ExportManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return ExportManifest{}, "", fmt.Errorf("manifest.json: %w", err)
	}
	archiveGroup := strings.SplitN(filepath.ToSlash(manifestEntry.Name), "/", 2)[0]
	return m, archiveGroup, nil
}

// readFleetFromZip reads the bundled fleet.json from the archive and
// returns the parsed config. Used to restore the per-group config on
// import.
func readFleetFromZip(zr *zip.Reader, path string) (*registry.GroupConfig, error) {
	for _, f := range zr.File {
		if filepath.ToSlash(f.Name) != path {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		raw, err := io.ReadAll(rc)
		if err != nil {
			return nil, err
		}
		cfg := &registry.GroupConfig{}
		if err := json.Unmarshal(raw, cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	return nil, fmt.Errorf("missing %s", path)
}

// zipPrefixExists reports whether any file in the zip has a name starting
// with prefix. Used to skip docs restoration when none was bundled.
func zipPrefixExists(zr *zip.Reader, prefix string) bool {
	for _, f := range zr.File {
		if strings.HasPrefix(filepath.ToSlash(f.Name), prefix) {
			return true
		}
	}
	return false
}

// extractZipPrefix copies every file under `prefix` in zr into destRoot,
// preserving the suffix path. Skips directory entries and refuses paths
// that escape destRoot (defence in depth against zip-slip).
func extractZipPrefix(zr *zip.Reader, prefix, destRoot string) error {
	absRoot, err := filepath.Abs(destRoot)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(name, prefix)
		if rel == "" || strings.HasSuffix(rel, "/") {
			continue
		}
		// zip-slip guard.
		destPath := filepath.Join(absRoot, filepath.FromSlash(rel))
		if !strings.HasPrefix(destPath+string(os.PathSeparator), absRoot+string(os.PathSeparator)) &&
			destPath != absRoot {
			return fmt.Errorf("archive entry escapes destination: %s", name)
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(destPath)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			return err
		}
		rc.Close()
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}
