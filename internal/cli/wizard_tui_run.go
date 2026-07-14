package cli

// wizard_tui_run.go — wiring between the wizard's preserved decision logic and
// the cohesive Bubble Tea TUI in internal/cli/wiztui (#5340).
//
// The TUI owns ONLY presentation + interaction. All classification, registry,
// install, and indexing logic stays here and is reused verbatim. This file
// implements wiztui.Driver and a channel-producing IndexFunc so the model can
// render a per-repo indexing view without owning any side effects.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/cajasmota/grafel/internal/cli/wiztui"
	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/detect"
	"github.com/cajasmota/grafel/internal/install/mcptools"
	"github.com/cajasmota/grafel/internal/install/tooladapter"
	"github.com/cajasmota/grafel/internal/progress"
	"github.com/cajasmota/grafel/internal/registry"
)

// mcpToolOptions builds the wiztui MCP-tools picker options from the detector
// (#5344), carrying the B+C computed default into the screen.
func mcpToolOptions() []wiztui.MCPToolOption {
	tools := mcptools.Detect()
	out := make([]wiztui.MCPToolOption, 0, len(tools))
	for _, t := range tools {
		out = append(out, wiztui.MCPToolOption{
			ID:              t.ID,
			DisplayName:     t.DisplayName,
			HasGrafel:       t.HasGrafel,
			DefaultSelected: t.DefaultSelected,
		})
	}
	return out
}

// wizardUseTUI reports whether the full-screen Bubble Tea TUI should drive the
// interactive wizard. It requires BOTH stdin and the wizard's stdout to be real
// terminals, and a non-dumb $TERM, so pipes / CI / redirected output fall back
// to the plain line-based huh flow (behavior preservation, #5340).
func wizardUseTUI(out io.Writer) bool {
	if t := os.Getenv("TERM"); t == "dumb" || t == "" {
		return false
	}
	if os.Getenv("GRAFEL_NO_TUI") != "" {
		return false
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// wizardDriver implements wiztui.Driver against the live classifier + registry.
type wizardDriver struct {
	class detect.Classification
}

func (d wizardDriver) ContextLine() string {
	return "Detected: " + describeClassification(d.class)
}

func (d wizardDriver) SuggestedAction() wiztui.Action {
	switch defaultAction(d.class) {
	case actionGroup:
		return wiztui.ActionGroup
	case actionMonorepo:
		return wiztui.ActionMonorepo
	case actionAddGroup:
		return wiztui.ActionAddGroup
	default:
		return wiztui.ActionSingle
	}
}

// Candidates derives the selectable repos/packages for an action, reusing the
// same precedence as the huh flow (groupCandidates / monorepo packages).
func (d wizardDriver) Candidates(a wiztui.Action) (string, []wiztui.Candidate) {
	switch a {
	case wiztui.ActionSingle:
		// Single: the cwd repo (or nothing — caller validates).
		if d.class.IsGitRepo {
			return "Repository to index", []wiztui.Candidate{
				{Label: d.class.AbsPath, Value: d.class.AbsPath, Selected: true},
			}
		}
		return "Repository to index", nil
	case wiztui.ActionMonorepo:
		if d.class.Monorepo == detect.KindNone || len(d.class.Packages) == 0 {
			return "Packages", nil
		}
		cands := make([]wiztui.Candidate, 0, len(d.class.Packages))
		for _, p := range d.class.Packages {
			cands = append(cands, wiztui.Candidate{Label: p, Value: p, Selected: true})
		}
		return fmt.Sprintf("%d packages found", len(d.class.Packages)), cands
	default: // group / add-group
		paths := groupCandidates(d.class)
		sort.Strings(paths)
		cands := make([]wiztui.Candidate, 0, len(paths))
		for _, p := range paths {
			cands = append(cands, wiztui.Candidate{Label: p, Value: p, Selected: true})
		}
		return fmt.Sprintf("%d repos found", len(paths)), cands
	}
}

func (d wizardDriver) Groups() []wiztui.Candidate {
	groups, err := registry.Groups()
	if err != nil {
		return nil
	}
	out := make([]wiztui.Candidate, 0, len(groups))
	for _, g := range groups {
		out = append(out, wiztui.Candidate{Label: g.Name, Value: g.Name})
	}
	return out
}

func (d wizardDriver) DefaultGroupName(repos []string) string {
	rs := make([]registry.Repo, 0, len(repos))
	for _, p := range repos {
		rs = append(rs, registry.Repo{Path: p})
	}
	return defaultGroupName(rs)
}

// reposForResult maps the TUI result's chosen paths to registry.Repo records,
// matching the per-action mapping the huh flow uses. A monorepo action maps
// to EXACTLY ONE registry.Repo rooted at the monorepo path, with the chosen
// packages recorded as Modules (see monorepoRepoForChosen) — never one
// flattened repo per package (D2/D3).
func reposForResult(class detect.Classification, r wiztui.Result) []registry.Repo {
	if r.Action == wiztui.ActionMonorepo {
		return []registry.Repo{monorepoRepoForChosen(class, r.Repos)}
	}
	return reposFromPaths(r.Repos)
}

// runInteractiveTUI drives the full-screen Bubble Tea wizard. It returns the
// model's Result. The caller (runWizard) applies all side effects based on it.
// Indexing is performed INSIDE the model loop via the IndexFunc so the per-repo
// view renders live, but the GROUP is only registered (applyGroupConfig) right
// before indexing starts — never on a cancel.
func runInteractiveTUI(out, errOut io.Writer, opts wizardOptions) (wiztui.Result, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return wiztui.Result{}, err
	}
	class, _ := detect.ClassifyPath(cwd)
	drv := wizardDriver{class: class}

	// Per-tool ENABLEMENT capture (#5701). The alt-screen TUI has an MCP-tools
	// picker but no native enablement screen, so we capture the enablement set
	// with the shared huh picker (promptTools) BEFORE entering the full-screen
	// program — unless --tools already preset it. This makes the ordinary
	// interactive `grafel wizard` scaffold only the tools the human checks
	// (deselecting kiro/codium excludes them), no flag required.
	toolIDs, err := resolveInteractiveTools(out, opts)
	if err != nil {
		return wiztui.Result{}, err
	}

	idxFn := makeIndexFunc(out, errOut, class, opts, toolIDs)
	// Build the MCP-tools picker options (#5344) unless a flag already preset
	// the selection (--mcp-tools / --no-mcp) — in which case the screen is
	// skipped (empty options) and the flag selection is honoured by the
	// IndexFunc.
	var mcpOpts []wiztui.MCPToolOption
	if opts.MCPTools == nil {
		mcpOpts = mcpToolOptions()
	}
	m := wiztui.New(drv, idxFn, opts.Watchers, opts.GitHooks, mcpOpts)

	// Switch the console to UTF-8 (Windows) before the alt-screen starts so the
	// wizard's glyphs render instead of mojibake; restore on exit (#5340).
	restoreConsole := wiztui.SetupConsole()
	defer restoreConsole()

	prog := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(out))
	final, err := prog.Run()
	if err != nil {
		return wiztui.Result{}, err
	}
	res := final.(wiztui.Model).Result()
	return res, res.IndexErr()
}

// resolveInteractiveTools resolves the per-tool ENABLEMENT set for the
// interactive (alt-screen) wizard (#5701). An explicit --tools flag wins and
// skips the prompt (validated; a bad ID errors before anything is registered).
// Otherwise it runs the shared promptTools picker so the human's checkbox
// selection — all eight adapters offered, detected ones pre-checked,
// deselection honoured — becomes cfg.Tools. An empty result (deselect-all, no
// flag) is left as-is: applyGroupConfig then falls back to the empty-means-all
// default, and we print a one-line hint that --tools can pin a subset.
func resolveInteractiveTools(out io.Writer, opts wizardOptions) ([]string, error) {
	if opts.Tools != "" {
		return tooladapter.ParseToolsFlag(opts.Tools)
	}
	ids, err := promptTools()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		fmt.Fprintln(out, "no tools selected — targeting all supported tools; pass --tools to pin a subset")
	}
	return ids, nil
}

// newGroupConfigFromResult assembles the GroupConfig for a new group from the
// TUI result, the resolved repos, the agent-hooks opt-in, and the captured
// per-tool enablement set (#5701). Pure/side-effect-free so the enablement
// wiring is unit-testable without a terminal.
func newGroupConfigFromResult(r wiztui.Result, repos []registry.Repo, agentHooks bool, toolIDs []string) *registry.GroupConfig {
	cfg := &registry.GroupConfig{Name: r.GroupName}
	cfg.Features.Watchers = r.Watchers
	cfg.Features.GitHooks = r.GitHooks
	cfg.Features.AgentHooks = agentHooks
	cfg.GroupDocs = r.GroupDocs
	cfg.Repos = repos
	cfg.Tools = toolIDs
	return cfg
}

// makeIndexFunc returns a wiztui.IndexFunc closure that, when the user confirms,
// (1) assembles + applies the group config (register/install) and (2) starts the
// daemon index, streaming broker progress.Events back to the model and the
// terminal outcome. All registration happens HERE — only on confirm — so a
// cancel registers nothing.
func makeIndexFunc(out, errOut io.Writer, class detect.Classification, opts wizardOptions, toolIDs []string) wiztui.IndexFunc {
	return func(r wiztui.Result) (<-chan progress.Event, <-chan wiztui.IndexOutcome) {
		evCh := make(chan progress.Event, 64)
		outCh := make(chan wiztui.IndexOutcome, 1)

		go func() {
			defer close(evCh)
			defer close(outCh)

			repos := reposForResult(class, r)

			// Resolve the MCP-tools selection (#5344): the picker screen sets
			// r.MCPTools; when a flag preset the choice the screen was skipped
			// and r.MCPTools is nil, so fall back to opts.MCPTools.
			mcpSel := r.MCPTools
			if mcpSel == nil {
				mcpSel = opts.MCPTools
			}

			// In the TUI, applyGroupConfig's stdout must NOT leak onto the
			// alt-screen (fix C, #5340). Capture it into a sink buffer and feed
			// the structured install.Result back into the Done screen instead.
			var sink bytes.Buffer

			// Add-to-existing-group path.
			if r.Action == wiztui.ActionAddGroup && r.AddToGroup != "" {
				if err := addReposToExistingGroupNoIndex(&sink, r.AddToGroup, repos, opts, mcpSel); err != nil {
					outCh <- wiztui.IndexOutcome{Err: err}
					return
				}
				streamIndexWithSummary(evCh, outCh, r.AddToGroup, opts.NoIndex, wiztui.InstallSummary{})
				return
			}

			// New-group path: assemble config (with the enablement set captured
			// by resolveInteractiveTools), apply (register + install).
			cfg := newGroupConfigFromResult(r, repos, opts.AgentHooks, toolIDs)
			res, err := applyGroupConfig(&sink, cfg, groupApplyOptions{RunInstall: opts.RunInstall, MCPTools: mcpSel, ProjectGuidance: opts.ProjectGuidance})
			if err != nil {
				outCh <- wiztui.IndexOutcome{Err: err}
				return
			}
			streamIndexWithSummary(evCh, outCh, cfg.Name, opts.NoIndex, installSummary(res))
		}()

		return evCh, outCh
	}
}

// installSummary maps an install.Result into the TUI's structured summary so the
// Done screen can render "installed N hooks · N watchers · N MCP" plus any
// watcher warnings, instead of those lines scattering over the alt-screen.
func installSummary(res *install.Result) wiztui.InstallSummary {
	if res == nil {
		// RunInstall was false — nothing was installed.
		return wiztui.InstallSummary{}
	}
	return wiztui.InstallSummary{
		Applied:         true,
		Hooks:           len(res.HooksInstalled),
		Watchers:        len(res.WatcherUnits),
		MCP:             len(res.MCPSettings),
		WatcherWarnings: res.WatcherWarnings,
		ConfigPath:      res.GroupConfigPath,
	}
}

// streamIndexWithSummary triggers a daemon index of group and forwards broker
// progress events onto evCh, then sends the terminal outcome on outCh — with the
// captured install summary attached so the Done screen renders it. A down daemon
// or --no-index is a soft completion (DaemonDown), not an error.
func streamIndexWithSummary(evCh chan<- progress.Event, outCh chan<- wiztui.IndexOutcome, group string, noIndex bool, summary wiztui.InstallSummary) {
	if noIndex {
		outCh <- wiztui.IndexOutcome{DaemonDown: true, Install: summary}
		return
	}

	c, err := client.Dial()
	if err != nil {
		if errors.Is(err, client.ErrDaemonNotRunning) {
			outCh <- wiztui.IndexOutcome{DaemonDown: true, Install: summary}
			return
		}
		outCh <- wiztui.IndexOutcome{Err: err, Install: summary}
		return
	}
	defer c.Close()

	dashPort := 0
	if st, stErr := c.Status(); stErr == nil && st.DashboardPort > 0 {
		dashPort = st.DashboardPort
	}

	token := progressToken()

	// SPLIT mode: the Rebuild RPC returns at ENQUEUE time (fire-and-forget), so
	// it can NEVER signal real completion. Completion is keyed on the rebuild-
	// request ack (runSplitIndex), REGARDLESS of whether a dashboard is up — a
	// missing dashboard only means no live SSE bars, never a fake instant Done
	// (#5729 blocker #10). We still establish the broker SSE subscription first
	// (when a dashboard exists) so no early per-repo events are lost (#5340);
	// with no dashboard sseCh stays nil and runSplitIndex simply forwards nothing.
	if daemon.SplitModeEnabled() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var sseCh <-chan sseEvent
		if dashPort > 0 {
			if ch, sseErr := subscribeSSE(ctx, dashPort, group); sseErr == nil {
				sseCh = ch
			}
		}
		o := runSplitIndex(ctx, cancel, c, group, token, sseCh, evCh)
		outCh <- toIndexOutcome(o, summary)
		return
	}

	// MONOLITH mode (unchanged): the Rebuild RPC is synchronous, so its return
	// IS completion and its reply carries the stats. When a dashboard is up we
	// forward broker SSE events until the RPC returns; otherwise we just wait on
	// the RPC. subscribeSSE connects synchronously (it returns only once the HTTP
	// stream is open), so by the time we fire the Rebuild RPC the broker is
	// listening and per-repo events are guaranteed to be delivered.
	if dashPort > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if sseCh, sseErr := subscribeSSE(ctx, dashPort, group); sseErr == nil {
			rpcCh := triggerRebuild(c, group, token)
			o := forwardBrokerToChannel(ctx, sseCh, rpcCh, evCh)
			cancel()
			outCh <- toIndexOutcome(o, summary)
			return
		}
	}

	// No dashboard broker — just trigger the rebuild and wait for the outcome.
	rpcCh := triggerRebuild(c, group, token)
	o := <-rpcCh
	outCh <- toIndexOutcome(o, summary)
}

// triggerRebuild fires the daemon Rebuild RPC for group on a goroutine and
// returns a buffered channel that delivers the single rebuild outcome. It is
// invoked AFTER the broker SSE subscription is established so the per-repo
// extraction events that race the RPC are not dropped (#5340).
func triggerRebuild(c *client.Client, group string, token string) <-chan rebuildOutcome {
	rpcCh := make(chan rebuildOutcome, 1)
	go func() {
		// #5328: the wizard is human-awaited → foreground (priority + higher CPU cap).
		reply, rpcErr := c.Rebuild(proto.RebuildArgs{Group: group, ProgressToken: token, Interactive: true})
		rpcCh <- rebuildOutcome{
			repos:    reply.Repos,
			warning:  reply.Warning,
			elapsed:  reply.ElapsedSec,
			entities: reply.TotalEntities,
			rels:     reply.TotalRels,
			err:      rpcErr,
		}
	}()
	return rpcCh
}

// forwardBrokerToChannel reads broker SSE events and forwards parsed
// progress.Events onto evCh until the RPC completes (rpcCh delivers) or the SSE
// stream closes, then returns the RPC outcome.
func forwardBrokerToChannel(
	ctx context.Context,
	sseCh <-chan sseEvent,
	rpcCh <-chan rebuildOutcome,
	evCh chan<- progress.Event,
) rebuildOutcome {
	for {
		select {
		case <-ctx.Done():
			return <-rpcCh
		case o := <-rpcCh:
			// Drain a final batch of SSE events so the last "done" rows render.
			drainBrokerToChannel(ctx, sseCh, evCh, 300)
			return o
		case ev, ok := <-sseCh:
			if !ok {
				// SSE closed before RPC — wait for the RPC outcome.
				return <-rpcCh
			}
			if ev.name != "progress" || ev.data == "" {
				continue
			}
			var e progress.Event
			if jsonUnmarshalEvent(ev.data, &e) {
				select {
				case evCh <- e:
				default:
				}
			}
		}
	}
}

// drainBrokerToChannel reads from sseCh for up to maxWaitMS, forwarding final
// progress events onto evCh (so the last "done" rows render before exit).
func drainBrokerToChannel(ctx context.Context, sseCh <-chan sseEvent, evCh chan<- progress.Event, maxWaitMS int) {
	deadline := time.After(time.Duration(maxWaitMS) * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			return
		case ev, ok := <-sseCh:
			if !ok {
				return
			}
			if ev.name != "progress" || ev.data == "" {
				continue
			}
			var e progress.Event
			if jsonUnmarshalEvent(ev.data, &e) {
				select {
				case evCh <- e:
				default:
				}
			}
		}
	}
}

// jsonUnmarshalEvent parses an SSE data payload into a progress.Event.
func jsonUnmarshalEvent(data string, e *progress.Event) bool {
	return json.Unmarshal([]byte(data), e) == nil
}

// toIndexOutcome maps a rebuildOutcome to the TUI's terminal IndexOutcome,
// attaching the captured install summary so the Done screen renders it.
func toIndexOutcome(o rebuildOutcome, summary wiztui.InstallSummary) wiztui.IndexOutcome {
	if o.err != nil {
		return wiztui.IndexOutcome{Err: o.err, Install: summary}
	}
	elapsed := ""
	if o.elapsed > 0 {
		elapsed = fmtDuration(time.Duration(o.elapsed * float64(time.Second)))
	}
	return wiztui.IndexOutcome{
		Entities: o.entities,
		Rels:     o.rels,
		Elapsed:  elapsed,
		Install:  summary,
	}
}

// addReposToExistingGroupNoIndex appends repos to an existing group and applies
// the config WITHOUT triggering an index (the TUI streams the index itself via
// streamIndex right after, so we don't want a double index here).
func addReposToExistingGroupNoIndex(out io.Writer, group string, repos []registry.Repo, opts wizardOptions, mcpSel *[]string) error {
	if len(repos) == 0 {
		return errors.New("no repos selected to add")
	}
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		return err
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load group %q: %w", group, err)
	}
	existing := map[string]struct{}{}
	for _, r := range cfg.Repos {
		existing[r.Path] = struct{}{}
	}
	added := 0
	for _, r := range repos {
		if _, dup := existing[r.Path]; dup {
			fmt.Fprintf(out, "skipping %s (already in group)\n", r.Path)
			continue
		}
		cfg.Repos = append(cfg.Repos, r)
		existing[r.Path] = struct{}{}
		added++
	}
	if added == 0 {
		return errors.New("all selected repos are already in the group")
	}
	if _, err := applyGroupConfig(out, cfg, groupApplyOptions{RunInstall: opts.RunInstall, MCPTools: mcpSel, ProjectGuidance: opts.ProjectGuidance}); err != nil {
		return err
	}
	fmt.Fprintf(out, "added %d repo(s) to group %q\n", added, group)
	return nil
}
