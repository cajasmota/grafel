package install

// registermcp.go implements the narrow "make this binary reachable as an MCP
// server" operation behind `grafel install --register-mcp`.
//
// ── The gap it closes (#6169) ────────────────────────────────────────────────
//
// install.sh downloads the archive, verifies the checksum, places the binary
// with `install -m 0755`, then runs `grafel install --refresh-state`, `grafel
// doctor` and its own daemon restart. None of those registers the MCP server:
// --refresh-state deliberately touches only install.json (and is a complete
// no-op when install.json is absent, which is exactly the first-install case),
// and doctor is read-only. The only two registration sites in the tree are
// RunCopy step 3 and ApplyToolDelta, and the curl installer reaches neither.
//
// The consequence on a first-ever install is not cosmetic. grafel's interface
// IS the MCP server; a binary on PATH with no entry in ~/.claude.json is a
// product that has not been installed. The user is then told to "run `grafel
// wizard`", which registers no MCP server either.
//
// ── Why the installer does this itself rather than printing advice ───────────
//
// The alternative fix is to detect the absent registration and print the next
// command. That command would have to be bare `grafel install`, and the
// installer cannot establish that it is safe to run at the moment it prints it:
// `grafel install` is RunCopy, whose step 4 restarts the daemon on a 60s budget
// and whose failure path rolls step 3 back — restoring MCP host configs from
// snapshots, which for a first-ever install is the absent-sentinel, i.e.
// DELETING the config file and every foreign server in it (#6168). The daemon
// state right after a binary swap is precisely the state in which that failure
// is likely. install.sh already prints that advice in its `stale` branch, which
// is the one branch where the daemon is known not to be healthy.
//
// RegisterMCP depends on no daemon, no network and no repository. It is
// idempotent, and — the point — it has NO ROLLBACK, so it can never be the
// thing that deletes a config.
//
// ── Exactly which files it writes ────────────────────────────────────────────
//
// More than the host configs, and saying "a surgical JSON merge plus a state
// record" hid that. Per registered target, mcpreg.RegisterPath also writes:
//
//	<cfg>.grafel.bak                      — the rollback sidecar. RegisterMCP
//	                                        DELETES this again immediately
//	                                        (ClearBackup), because it never
//	                                        rolls back and the sentinel form of
//	                                        this file is what a restore uses to
//	                                        delete a config outright (#6168).
//	~/.grafel/backups/mcpreg/<...>.json   — a timestamped audit copy of the
//	                                        pre-change content. This one is
//	                                        KEPT: it is grafel's own directory,
//	                                        no restore path reads it
//	                                        (RestoreSnapshot consults only the
//	                                        sidecar), and it is the only record
//	                                        of what a host config looked like
//	                                        before grafel first touched it.
//
// plus ~/.grafel/install.json, which is the state record.
//
// ── What it deliberately does NOT do ─────────────────────────────────────────
//
// No skills copy, no daemon restart, no service registration, no .gitignore
// append, no git hooks, no TTY prompt. Those are RunCopy's job and they are why
// a one-line installer must not call it: steps 5 and 7 mutate the git
// repository the user's shell happened to be sitting in, which the installer
// was never given consent to touch (#6162).

import (
	"fmt"
	"os"

	"github.com/cajasmota/grafel/internal/install/mcpreg"
)

// RegisterMCPOptions configures RegisterMCP.
type RegisterMCPOptions struct {
	// BinPath is the binary to register as the MCP command. Defaults to
	// os.Executable().
	BinPath string

	// StatePath is the path of install.json. Defaults to DefaultStatePath().
	StatePath string

	// ClaudeConfigDirs, when non-empty, overrides auto-detection of the
	// ~/.claude.json paths (the --claude-config-dirs flag). When it is set,
	// auto-detected non-Claude hosts are NOT added: an explicit target list is
	// a request for exactly those targets.
	ClaudeConfigDirs []string
}

// MCPRegisterFailure records one host config that could not be written.
type MCPRegisterFailure struct {
	Path string
	Err  error
}

// RegisterMCPResult reports what RegisterMCP did.
type RegisterMCPResult struct {
	// Registered lists the host config paths that now carry a grafel entry.
	Registered []string
	// Failed lists the hosts that could not be written. A non-empty Failed is
	// NOT an error: the remaining hosts are still registered.
	Failed []MCPRegisterFailure
	// StatePath is the install.json that was updated.
	StatePath string
	// CreatedState is true when install.json did not exist and was created to
	// record this registration.
	CreatedState bool
}

func (o *RegisterMCPOptions) applyDefaults() error {
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
	return nil
}

// registerMCPTargets resolves the host config files to write.
//
// It mirrors RunCopy step 3's target set — every detected Claude config dir
// plus any Windsurf config whose parent directory already exists — so that a
// later `grafel install` finds nothing new to do and `grafel uninstall` sweeps
// the same files. An explicit ClaudeConfigDirs list suppresses the Windsurf
// probe: a caller naming its targets gets its targets.
func registerMCPTargets(explicit []string) []string {
	dirs := mcpreg.DetectClaudeConfigDirs(explicit)
	if len(explicit) > 0 {
		return dirs
	}
	targets := make([]string, 0, len(dirs)+1)
	targets = append(targets, dirs...)
	return append(targets, mcpreg.DetectWindsurfPaths()...)
}

// RegisterMCP registers the grafel MCP server in every detected host config and
// records the registration in install.json.
//
// A host that cannot be written is reported in Result.Failed and the remaining
// hosts are still registered; the call succeeds. That is the opposite of
// RunCopy step 3 on purpose — see the rollback note in the file header.
//
// It returns an error only for conditions that make the whole operation
// meaningless: an unhashable binary (there is no honest CLI record for a binary
// that is not on disk, and registering a `command` that points at nothing would
// produce a config that fails at every Claude Code start) or an unwritable
// install.json.
func RegisterMCP(opts RegisterMCPOptions) (*RegisterMCPResult, error) {
	if err := opts.applyDefaults(); err != nil {
		return nil, err
	}

	// Hash FIRST, before writing anything. A missing binary must leave both the
	// host configs and install.json exactly as they were.
	sha, err := sha256File(opts.BinPath)
	if err != nil {
		return nil, fmt.Errorf("hash binary %s: %w", opts.BinPath, err)
	}

	res := &RegisterMCPResult{StatePath: opts.StatePath}

	for _, cfgPath := range registerMCPTargets(opts.ClaudeConfigDirs) {
		if _, rerr := mcpreg.RegisterPath(cfgPath, opts.BinPath); rerr != nil {
			res.Failed = append(res.Failed, MCPRegisterFailure{Path: cfgPath, Err: rerr})
			continue
		}
		// Discard the rollback snapshot RegisterPath just planted.
		//
		// backupOnce writes `<cfg>.grafel.bak` before its first modification —
		// on a first-ever install that is the absent-sentinel, the value whose
		// restore DELETES the config file and every foreign server in it. This
		// function never rolls back, so it has no use for a snapshot, and
		// leaving one behind would make "RegisterMCP cannot be the thing that
		// deletes a config" depend on the two RestoreSnapshot call sites
		// (copy.go, dev.go) continuing to run discardStaleMCPBackups first.
		// That is coordination between three files; clearing it here is a
		// property of this one. Per-target rather than after the loop, so a
		// later host failing cannot strand a live sentinel on an earlier
		// success.
		mcpreg.ClearBackup(cfgPath)
		res.Registered = append(res.Registered, cfgPath)
	}

	if err := recordMCPRegistration(res, opts.BinPath, sha); err != nil {
		return res, err
	}
	return res, nil
}

// recordMCPRegistration folds the registration into install.json.
//
// `grafel uninstall` deregisters strictly from state.MCP.RegisteredPaths, so a
// registration that is not recorded there can never be removed — the entry
// would outlive the uninstall and leave Claude Code launching a binary that is
// gone. Recording is therefore not bookkeeping; it is what makes the write
// reversible.
//
// When install.json is ABSENT it is created. This is the one place that
// deliberately differs from RefreshState, which refuses to fabricate a state
// file — and the difference is that RefreshState has nothing to record (no
// install happened; it only re-reads a binary) whereas RegisterMCP has just
// performed a real, user-visible mutation. The record it writes asserts only
// what is true: this binary, this checksum, these MCP paths, and SkillsSkipped.
//
// On SkillsSkipped, precisely — an earlier draft of this comment claimed it
// makes `grafel doctor` read the state as an advisory binary-only install
// rather than a broken one. That was false. Repo-wide the field is written
// (copy.go step 2, here) and cleared (copy.go's state merge) and read by
// nothing outside tests; no doctor check consults it. It is set here for one
// honest reason: it is the field whose meaning is "step 2 copied no skills",
// that is exactly what happened, and RunCopy writes it in the same situation —
// so the state this produces is the state `grafel install` would produce for
// the same facts, rather than a shape unique to this path. It buys consistency
// and a true record, not behaviour. A later real `grafel install` overwrites it
// wholesale (the state merge clears SkillsSkipped once step 2 runs).
func recordMCPRegistration(res *RegisterMCPResult, binPath, sha string) error {
	state, err := ReadState(res.StatePath)
	if err != nil {
		return err
	}
	if state == nil {
		state = NewState(ModeCopy)
		// Nothing here copied skills, and this is the field that says so. No
		// production code reads it today — see the note above; it is set to
		// keep the record true and identical in shape to RunCopy's, not
		// because anything downstream branches on it.
		state.SkillsSkipped = true
		res.CreatedState = true
	}

	// The CLI record is refreshed for the same reason --refresh-state exists:
	// the installer just replaced the binary, and the MCP entry we wrote names
	// that binary, so the two must agree.
	state.CLI = CLIRecord{Path: binPath, SHA256: sha}

	// InstalledAt is left alone on an existing state (no install happened
	// here); DaemonVersion likewise — RegisterMCP restarts no daemon, so it has
	// no grounds to invalidate the record of one. install.sh's own
	// record_install_state runs --refresh-state, which owns that clearing.

	state.MCP.Name = mcpreg.ServerName
	state.MCP.RegisteredPaths = unionPaths(state.MCP.RegisteredPaths, res.Registered)

	return WriteState(res.StatePath, state)
}

// unionPaths appends the paths of b that a does not already have, preserving
// a's order. The installer calls RegisterMCP on every upgrade, so appending
// blindly would grow the list without bound.
func unionPaths(a, b []string) []string {
	seen := make(map[string]bool, len(a))
	out := make([]string, 0, len(a)+len(b))
	for _, p := range a {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, p := range b {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
