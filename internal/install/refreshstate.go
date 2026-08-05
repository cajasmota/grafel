package install

// refreshstate.go implements the narrow "record the binary that is actually on
// disk" operation behind `grafel install --refresh-state`.
//
// ── Why this exists, and why it is NOT `grafel install` ──────────────────────
//
// install.sh (the curl one-liner) places the new binary with
//
//	install -m 0755 "$TMP_DIR/grafel" "$BIN_DIR/grafel"
//
// and never runs anything that writes ~/.grafel/install.json — which is only
// ever written by RunCopy step 6. So after every curl UPGRADE the recorded
// CLI.SHA256 is the PREVIOUS binary's, RunQuickDoctor's check 1 fires, and the
// user sees "binary updated since last install" prefixed onto every single
// grafel command until they happen to re-run `grafel install` by hand.
//
// The obvious fix — have install.sh call `"$BIN_DIR/grafel" install` — is the
// wrong one. RunCopy is a seven-step transaction, and on an upgrade run from a
// `curl … | bash` pipeline four of those steps are actively undesirable:
//
//	step 3  rewrites every detected .claude.json (MCP re-registration);
//	step 4  restarts the daemon with a 60s readiness budget — install.sh
//	        already does its own restart_daemon with version verification
//	        immediately afterwards, so this both duplicates the work and
//	        races it;
//	step 5  appends /.grafel/ to the .gitignore of whatever git repo the
//	        user's shell happened to be sitting in when they ran the curl;
//	step 7  installs four git hooks (post-checkout, post-merge, post-rewrite,
//	        pre-push) into that same unrelated repository.
//
// Steps 5 and 7 are the disqualifying ones: a one-line installer must not
// mutate a repository the user never mentioned. (The `grafel install` CLI path
// additionally runs the interactive tool-selection wizard when stdin is a TTY —
// `bash install.sh` from a terminal would stop and prompt mid-install.)
//
// RefreshState is therefore the narrow alternative: it rewrites ONLY the CLI
// record of an ALREADY-EXISTING install.json and leaves every other field
// untouched. It performs no service, filesystem, config, or network work.

import (
	"fmt"
	"os"
)

// RefreshOptions configures RefreshState.
type RefreshOptions struct {
	// BinPath is the binary to record. Defaults to os.Executable().
	BinPath string

	// StatePath is the path of install.json. Defaults to DefaultStatePath().
	StatePath string
}

// RefreshResult reports what RefreshState did.
type RefreshResult struct {
	// HadState is false when there was no install.json to refresh.
	HadState bool
	// Changed is true only when the CLI record actually differed and was
	// rewritten.
	Changed bool
	// StatePath is the install.json that was considered.
	StatePath string
	// Path is the binary that was recorded (empty when HadState is false).
	Path string
	// SHA256 is the hex SHA-256 recorded for Path.
	SHA256 string
}

func (o *RefreshOptions) applyDefaults() error {
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

// RefreshState makes install.json's CLI record agree with the binary on disk.
//
// It is a no-op (HadState=false, no file created) when install.json does not
// exist: an absent state file means `grafel install` has never run, and
// quick-doctor is already silent in that condition. Fabricating a state there
// would be worse than doing nothing — a synthetic record with no skills
// manifest and no MCP registration would make `grafel doctor` start reporting
// drift that does not exist.
//
// Every other field of the state — install mode, InstalledAt, skills manifest,
// MCP registration, gitignore record, partial-install flags — is preserved
// exactly as read. The sole exception is DaemonVersion, which an in-place
// binary swap definitely invalidates; see the comment at the write below.
func RefreshState(opts RefreshOptions) (*RefreshResult, error) {
	if err := opts.applyDefaults(); err != nil {
		return nil, err
	}

	res := &RefreshResult{StatePath: opts.StatePath}

	state, err := ReadState(opts.StatePath)
	if err != nil {
		return nil, err
	}
	if state == nil {
		// Never installed — nothing to refresh, and nothing to invent.
		return res, nil
	}
	res.HadState = true

	// Hash BEFORE touching the state file: a missing/unreadable binary must
	// leave the previous (still meaningful) CLI record intact rather than
	// blanking it, which checkCLI would report as a CRITICAL partial install.
	sha, err := sha256File(opts.BinPath)
	if err != nil {
		return nil, fmt.Errorf("hash binary %s: %w", opts.BinPath, err)
	}
	res.Path = opts.BinPath
	res.SHA256 = sha

	if state.CLI.Path == opts.BinPath && state.CLI.SHA256 == sha && state.DaemonVersion == "" {
		// Already current — do not rewrite the file, so the operation is
		// genuinely idempotent and the installer can call it unconditionally.
		return res, nil
	}

	// Path as well as SHA: the recorded path can be wrong on its own (an
	// install that moved prefix), and correcting it is the sole remedy for
	// quick-doctor's "install.json records <gone path>" warning.
	state.CLI = CLIRecord{Path: opts.BinPath, SHA256: sha}

	// InstalledAt is deliberately NOT touched: no install happened here, and
	// stamping "now" would make the field assert something false.

	// DaemonVersion IS cleared, and it is the one field an in-place binary
	// swap definitely invalidates: install.sh replaces the binary and then
	// restarts the daemon, so the recorded version describes a process that no
	// longer exists and checkDaemon would report a version mismatch on every
	// `grafel doctor`. checkDaemon early-returns on "", so dropping a
	// known-wrong value is strictly better than preserving it; the next real
	// `grafel install` / `grafel update` repopulates it from the live probe.
	state.DaemonVersion = ""

	if err := WriteState(opts.StatePath, state); err != nil {
		return nil, err
	}
	res.Changed = true
	return res, nil
}
