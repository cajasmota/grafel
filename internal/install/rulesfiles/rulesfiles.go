// Package rulesfiles writes the canonical "how to use the grafel MCP"
// rules block into every per-repo IDE rules file convention.
//
// Different IDE coding agents read different per-repo files for their
// project-level system prompt:
//
//   - Claude Code      → AGENTS.md, CLAUDE.md
//   - Codex / OpenAI   → AGENTS.md
//   - Windsurf Cascade → .windsurfrules
//   - Cursor Composer  → .cursorrules
//   - Codeium          → .codeium/instructions.md
//   - GitHub Copilot   → .github/copilot-instructions.md
//
// `grafel install` historically only wrote the rules block into
// AGENTS.md, which meant Cascade and Cursor sessions did not learn that
// the grafel MCP exists (issue #2683). This package generalises that
// writer so the same idempotent, marker-wrapped block is written to every
// known convention.
//
// All writes are idempotent: the block is bounded by
// <!-- grafel:mcp-usage:start v=1 --> ... <!-- grafel:mcp-usage:end -->
// and an existing block is replaced in-place. Files that already contain
// content unrelated to grafel are preserved byte-for-byte outside the
// markers.
//
// "Stale predecessor" handling: the older `graphify` tool wrote its own
// guidance into the same rules files. When we encounter a file that
// references graphify but has no grafel block, we either overwrite
// (if the file is entirely about graphify — heuristic: short + every
// non-blank line references graphify or is markdown structure) or leave
// it and emit a warning so the user can migrate manually.
package rulesfiles

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// BlockVersion is the version embedded in the start marker. Bumping the
// version causes doctor to flag any file whose block uses an older
// version as OUTDATED so the next `grafel install` rewrites it.
//
// v2 (#3648): added the imperative STANDING DIRECTIVE near the top of the
// block so agents keep reaching for grafel on structural questions for
// the whole session instead of drifting back to grep after a few calls.
const BlockVersion = 2

// StartMarker / EndMarker bound the managed region inside every rules
// file. Keep this stable across releases — bumping the marker syntax
// breaks idempotent updates of files written by older binaries.
var (
	StartMarker = fmt.Sprintf("<!-- grafel:mcp-usage:start v=%d -->", BlockVersion)
	EndMarker   = "<!-- grafel:mcp-usage:end -->"
)

// startMarkerAnyVersion matches an existing grafel block regardless
// of its embedded version number, so we can replace older blocks in
// place. The end marker is unversioned and a literal string.
var startMarkerAnyVersion = regexp.MustCompile(`<!-- grafel:mcp-usage:start v=\d+ -->`)

// blockRegexAnyVersion captures the entire marker-wrapped region (start
// + content + end), with DOTALL so newlines inside the block are
// consumed.
var blockRegexAnyVersion = regexp.MustCompile(
	`(?s)<!-- grafel:mcp-usage:start v=\d+ -->.*?` +
		regexp.QuoteMeta(EndMarker))

// PredecessorTokens are the names of older tools whose guidance, if
// found in a rules file, indicates a stale file that probably misleads
// AI agents (e.g. it points them at `graphify update` or paths like
// `graphify-out/GRAPH_REPORT.md`). The list is intentionally narrow —
// adding generic words here risks false positives.
var PredecessorTokens = []string{
	"graphify",
}

// Targets is the canonical list of per-repo rules-file paths the
// installer writes to, in deterministic order. Paths use forward
// slashes; callers join them with the repo root via filepath.Join.
//
// The order is also the order in which doctor reports them, so the user
// sees a consistent grouping per repo.
var Targets = []string{
	"AGENTS.md",
	"CLAUDE.md",
	".windsurfrules",
	".cursorrules",
	".codeium/instructions.md",
	".github/copilot-instructions.md",
}

// Status is the per-file state reported by Scan / doctor.
type Status string

const (
	// StatusOK means the file contains the current grafel block.
	StatusOK Status = "ok"
	// StatusMissing means the file does not exist at all.
	StatusMissing Status = "missing"
	// StatusStale means the file references a predecessor tool but has
	// no grafel block — install would either overwrite (short, pure
	// stale content) or warn (mixed content).
	StatusStale Status = "stale"
	// StatusOutdated means the file has an grafel block but its
	// version marker is older than BlockVersion.
	StatusOutdated Status = "outdated"
)

// FileStatus reports the install state of a single rules file.
type FileStatus struct {
	// Path is the absolute path of the file (whether it exists or not).
	Path string
	// Target is the relative path under the repo root (e.g. ".windsurfrules").
	Target string
	// Status is the high-level state (OK/Missing/Stale/Outdated).
	Status Status
	// Detail is a one-line human-readable explanation when Status != OK.
	Detail string
}

// WriteOptions tunes per-repo write behaviour.
type WriteOptions struct {
	// GroupName is the grafel group this repo belongs to. Embedded
	// in the rendered block when non-empty.
	GroupName string
	// Logger receives info/warning lines; defaults to os.Stderr.
	Logger io.Writer
}

// WriteResult is the outcome of writing rules files into a single repo.
type WriteResult struct {
	// Written lists the rules files that were created or updated.
	Written []string
	// SkippedMixedStale lists files left untouched because they mix
	// grafel-unrelated content with predecessor references. The
	// installer warns the user to migrate these manually.
	SkippedMixedStale []string
	// ReplacedStale lists files that were entirely predecessor content
	// and got overwritten with the canonical grafel block.
	ReplacedStale []string
}

// WriteAll renders the canonical block and upserts it into every Target
// under repoRoot. It is safe to call repeatedly; updates are idempotent
// and predecessor handling is best-effort (failures are logged, never
// fatal).
//
// Errors are returned only for genuine I/O problems (permission denied,
// disk full); per-file decisions (skip-mixed-stale, replace-stale) are
// reported via the returned WriteResult.
func WriteAll(repoRoot string, opts WriteOptions) (*WriteResult, error) {
	return WriteTargets(repoRoot, opts, Targets)
}

// WriteTargets is WriteAll restricted to an explicit subset of rules-file
// targets. It is used by the per-tool ToolAdapter model so that a given
// tool only writes the rules file(s) it actually reads, instead of the
// full canonical set. The block content and per-file decision tree are
// identical to WriteAll — only the set of paths differs.
//
// Passing rulesfiles.Targets reproduces WriteAll exactly. Unknown paths
// are written verbatim under repoRoot (callers pass values from this
// package's Targets list).
func WriteTargets(repoRoot string, opts WriteOptions, targets []string) (*WriteResult, error) {
	if opts.Logger == nil {
		opts.Logger = os.Stderr
	}
	block := RenderBlock(opts.GroupName)

	res := &WriteResult{}
	for _, target := range targets {
		abs := filepath.Join(repoRoot, target)
		action, err := upsert(abs, block)
		if err != nil {
			return res, fmt.Errorf("rulesfiles: %s: %w", target, err)
		}
		switch action {
		case actionWroteFresh, actionReplacedBlock, actionAppended:
			res.Written = append(res.Written, target)
		case actionReplacedStale:
			res.Written = append(res.Written, target)
			res.ReplacedStale = append(res.ReplacedStale, target)
			fmt.Fprintf(opts.Logger,
				"grafel install: replaced stale graphify content in %s\n", abs)
		case actionSkippedMixedStale:
			res.SkippedMixedStale = append(res.SkippedMixedStale, target)
			fmt.Fprintf(opts.Logger,
				"grafel install: file %s contains stale graphify content; please migrate manually or remove\n", abs)
		}
	}
	return res, nil
}

// Scan returns the FileStatus for every Target under repoRoot without
// writing anything. Used by `grafel doctor`.
func Scan(repoRoot string) []FileStatus {
	out := make([]FileStatus, 0, len(Targets))
	for _, target := range Targets {
		abs := filepath.Join(repoRoot, target)
		st := classify(abs)
		st.Target = target
		out = append(out, st)
	}
	return out
}

// RenderBlock returns the canonical marker-wrapped guidance block for
// the given group. The block is the same across all rules files — IDEs
// have different conventions for *which* file to read, but the content
// inside the markers is identical, so an agent that sees the block in
// any one file gets the full guidance.
func RenderBlock(groupName string) string {
	var b strings.Builder
	if groupName == "" {
		groupName = "<group-name>"
	}

	fmt.Fprintln(&b, StartMarker)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## grafel MCP")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "This repo is part of grafel group **%s**. grafel is an "+
		"architecture knowledge graph available via MCP. When you (an AI coding "+
		"agent) need to understand how this codebase fits together, prefer the "+
		"grafel MCP tools over `grep` + reading files.\n", groupName)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### STANDING DIRECTIVE — query the graph, don't grep your way around it")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- **Default to grafel for STRUCTURAL questions**: where is `X` defined, who calls/uses `Y`, how does a request flow end-to-end, what is the blast radius of a change, what are the modules. Reach for `grafel_find` / `grafel_inspect` / `grafel_neighbors` / `grafel_traces` / `grafel_impact_radius` for these — **not** `grep` + reading files.")
	fmt.Fprintln(&b, "- **This holds for the WHOLE session, not just your first few calls.** If you notice you have been grepping or opening files to answer a structural question, stop and query the graph instead — it is faster and more accurate, and it stays that way on call 50 as much as on call 1.")
	fmt.Fprintln(&b, "- **`grep` is still right for**: raw string / substring / TODO / FIXME sweeps, and content that is not in the graph (comments, config values, log strings).")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### When to use grafel instead of grep")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Question shape | Prefer |")
	fmt.Fprintln(&b, "|---|---|")
	fmt.Fprintln(&b, "| \"Where is `X` defined?\" | `grafel_find` |")
	fmt.Fprintln(&b, "| \"What does `X` look like + its neighbors?\" | `grafel_inspect` |")
	fmt.Fprintln(&b, "| \"Who calls `X`?\" | `grafel_expand` / `grafel_find_callers` |")
	fmt.Fprintln(&b, "| \"End-to-end flow when user does X?\" | `grafel_traces` |")
	fmt.Fprintln(&b, "| \"How does the frontend talk to the backend?\" | `grafel_cross_links` |")
	fmt.Fprintln(&b, "| \"Show me the source of `X`\" | `grafel_get_source` |")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### When grep IS still better")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- Substring search across all files for non-entity strings (comments, TODOs).")
	fmt.Fprintln(&b, "- Anything where you need raw file contents in bulk.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Anti-patterns")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- Don't read an entire file to find one function — `grafel_inspect` returns it directly.")
	fmt.Fprintln(&b, "- Don't glob for a class name across the repo — `grafel_find` indexes it.")
	fmt.Fprintln(&b, "- Don't traverse imports manually — `grafel_expand` does it via the IMPORTS edge.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "The full agent guide is delivered automatically in the MCP `instructions` handshake when you connect.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "_Do not edit between the markers — this block is auto-updated by `grafel install`._")
	fmt.Fprintln(&b)
	fmt.Fprint(&b, EndMarker)
	return b.String()
}

// ── internal helpers ─────────────────────────────────────────────────

// action codes returned by upsert; surfaced to WriteAll so callers can
// log per-file decisions.
type action int

const (
	actionUnknown action = iota
	actionWroteFresh
	actionReplacedBlock
	actionAppended
	actionReplacedStale
	actionSkippedMixedStale
)

// upsert reads path, decides what to do, and writes back atomically.
// See package doc for the decision tree.
func upsert(path, block string) (action, error) {
	existing, err := os.ReadFile(path)
	switch {
	case err != nil && !os.IsNotExist(err):
		return actionUnknown, fmt.Errorf("read %s: %w", path, err)

	case os.IsNotExist(err) || len(existing) == 0:
		if err := atomicWrite(path, []byte(block)); err != nil {
			return actionUnknown, err
		}
		return actionWroteFresh, nil

	case blockRegexAnyVersion.Match(existing):
		// Existing grafel block — replace in-place. This also covers
		// the OUTDATED case (block exists with older version marker).
		out := blockRegexAnyVersion.ReplaceAll(existing, []byte(strings.TrimRight(block, "\n")))
		if err := atomicWrite(path, out); err != nil {
			return actionUnknown, err
		}
		return actionReplacedBlock, nil

	case hasPredecessorRef(existing):
		// File has no grafel block but references a predecessor.
		// Decide between full-overwrite (pure stale) and skip-with-warning
		// (mixed content).
		if isPureStaleFile(existing) {
			if err := atomicWrite(path, []byte(block)); err != nil {
				return actionUnknown, err
			}
			return actionReplacedStale, nil
		}
		return actionSkippedMixedStale, nil

	default:
		// File exists with unrelated content — append the block with a
		// blank-line separator, preserving every byte of user content.
		buf := make([]byte, 0, len(existing)+len(block)+2)
		buf = append(buf, existing...)
		if existing[len(existing)-1] != '\n' {
			buf = append(buf, '\n')
		}
		buf = append(buf, '\n')
		buf = append(buf, []byte(block)...)
		if err := atomicWrite(path, buf); err != nil {
			return actionUnknown, err
		}
		return actionAppended, nil
	}
}

// classify returns the FileStatus for a single absolute path.
func classify(abs string) FileStatus {
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return FileStatus{Path: abs, Status: StatusMissing, Detail: "file does not exist"}
		}
		return FileStatus{Path: abs, Status: StatusMissing, Detail: fmt.Sprintf("read: %v", err)}
	}

	if m := startMarkerAnyVersion.FindSubmatch(data); m != nil {
		// Parse the version embedded in the start marker.
		v := extractBlockVersion(string(m[0]))
		if v == BlockVersion {
			return FileStatus{Path: abs, Status: StatusOK}
		}
		return FileStatus{
			Path:   abs,
			Status: StatusOutdated,
			Detail: fmt.Sprintf("grafel block v=%d, current v=%d", v, BlockVersion),
		}
	}

	if hasPredecessorRef(data) {
		mixed := !isPureStaleFile(data)
		detail := "contains predecessor (graphify) references"
		if mixed {
			detail += " mixed with unrelated content; manual migration recommended"
		} else {
			detail += "; next `grafel install` will overwrite"
		}
		return FileStatus{Path: abs, Status: StatusStale, Detail: detail}
	}

	// File exists but has no grafel block and no stale refs. From
	// the doctor's point of view this is still a problem — the rules
	// block is missing.
	return FileStatus{
		Path:   abs,
		Status: StatusMissing,
		Detail: "file exists but has no grafel block; run `grafel install`",
	}
}

// extractBlockVersion parses "v=N" out of the start marker. Returns 0
// if the marker is malformed (treated as outdated).
var versionRe = regexp.MustCompile(`v=(\d+)`)

func extractBlockVersion(marker string) int {
	m := versionRe.FindStringSubmatch(marker)
	if len(m) < 2 {
		return 0
	}
	var n int
	for _, c := range m[1] {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// hasPredecessorRef returns true when data references any predecessor
// tool name. Case-insensitive substring match.
func hasPredecessorRef(data []byte) bool {
	lower := strings.ToLower(string(data))
	for _, tok := range PredecessorTokens {
		if strings.Contains(lower, strings.ToLower(tok)) {
			return true
		}
	}
	return false
}

// isPureStaleFile is the "the entire file is predecessor content"
// heuristic. We treat a file as pure-stale when:
//
//   - It is short (≤ 30 lines total), AND
//   - Every non-blank, non-markdown-structural line references at
//     least one predecessor token.
//
// "Markdown structural" lines are headings, list bullets, code-fence
// markers, horizontal rules — content that frames the stale text but
// doesn't carry independent meaning.
func isPureStaleFile(data []byte) bool {
	const maxLines = 30
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lines := 0
	for scanner.Scan() {
		lines++
		if lines > maxLines {
			return false
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if isMarkdownStructural(line) {
			continue
		}
		if !lineMentionsPredecessor(line) {
			return false
		}
	}
	return true
}

func isMarkdownStructural(line string) bool {
	switch {
	case strings.HasPrefix(line, "#"):
		return true
	case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ "):
		// Even bullet lines must still mention the predecessor to count
		// as stale — but here we treat the bullet *marker itself* as
		// structural. The caller still checks lineMentionsPredecessor
		// on the full line, which includes the bullet content. So bullet
		// lines whose payload doesn't reference graphify will correctly
		// flip isPureStaleFile to false.
		return false
	case line == "---" || line == "***" || line == "___":
		return true
	case strings.HasPrefix(line, "```"):
		return true
	case strings.HasPrefix(line, ">"):
		return false
	}
	return false
}

func lineMentionsPredecessor(line string) bool {
	lower := strings.ToLower(line)
	for _, tok := range PredecessorTokens {
		if strings.Contains(lower, strings.ToLower(tok)) {
			return true
		}
	}
	return false
}

// atomicWrite writes data to path via a tmp-file + rename. Parent dirs
// are created as needed so .codeium/ and .github/ are handled
// transparently.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
