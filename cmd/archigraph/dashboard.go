package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/dashboard"
)

// runDashboard is wired into cli.Hooks and dispatches the `dashboard`
// subcommand verbs. With no verb it opens the running (or auto-started)
// daemon's dashboard in the user's default browser. The `serve` verb
// starts a standalone dev server (existing behaviour, preserved as-is).
func runDashboard(argv []string) error {
	// No args → open-browser flow.
	if len(argv) == 0 {
		return runDashboardOpen()
	}
	if argv[0] == "-h" || argv[0] == "--help" {
		fmt.Fprintln(os.Stderr, dashboardHelpText)
		return nil
	}
	switch argv[0] {
	case "serve":
		return runDashboardServe(argv[1:])
	case "open":
		// Explicit `dashboard open` alias.
		return runDashboardOpen()
	default:
		return fmt.Errorf("unknown dashboard verb %q\n\n%s", argv[0], dashboardHelpText)
	}
}

const dashboardHelpText = `usage: archigraph dashboard [serve] [flags]

With no arguments, opens the dashboard in your default browser,
auto-starting the daemon if it is not already running.

Subcommands:
  (none)         Open dashboard in browser (same as: open)
  open           Open dashboard in browser
  serve          Run a standalone dashboard HTTP server (dev/advanced)

Flags for 'serve':
  --bind string  Override bind address
  --port int     Pin to a specific port (useful for Vite proxy)

The daemon (started via 'archigraph start') owns MCP, indexer,
watchers, and the dashboard on port 47274 by default.
Set ARCHIGRAPH_DASHBOARD_PORT to override the port.

Use 'archigraph dashboard serve --port N' for standalone dev workflows.`

// runDashboardOpen ensures the daemon is running, resolves the dashboard
// URL, prints it to stdout, and opens it in the user's default browser.
func runDashboardOpen() error {
	port, err := ensureDaemonAndGetPort()
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	fmt.Printf("Dashboard: %s\n", url)
	if openErr := openBrowser(url); openErr != nil {
		// Non-fatal: headless server, missing desktop environment, etc.
		// The URL was already printed so the user can paste it manually.
		fmt.Fprintf(os.Stderr, "note: could not open browser (%v); visit the URL above\n", openErr)
	}
	return nil
}

// ensureDaemonAndGetPort returns the dashboard TCP port, starting the
// daemon first if it is not already running. Port resolution order:
//  1. Daemon Status RPC (single source of truth once daemon is up)
//  2. ARCHIGRAPH_DASHBOARD_PORT env var
//  3. Hard-coded default (defaultDashboardPort = 47274)
func ensureDaemonAndGetPort() (int, error) {
	c, dialErr := client.Dial()
	if dialErr != nil {
		// Daemon not running — fork the current binary with "start" so
		// the exact same start logic runs (binary-mismatch check, readiness
		// poll, etc.). This avoids duplicating the start implementation here.
		fmt.Fprintln(os.Stderr, "daemon not running — starting…")
		if err := execStart(); err != nil {
			return 0, fmt.Errorf("auto-start daemon: %w", err)
		}
		// execStart blocks until the daemon is ready (5 s poll in the start
		// subcommand). Re-dial to get a client for the Status call.
		var retryErr error
		for i := 0; i < 10; i++ {
			c, retryErr = client.Dial()
			if retryErr == nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if retryErr != nil {
			return 0, fmt.Errorf("daemon started but socket still unreachable: %w", retryErr)
		}
	}
	defer c.Close()

	st, err := c.Status()
	if err == nil && st.DashboardPort > 0 {
		return st.DashboardPort, nil
	}
	// Fallback: env var or compiled-in default.
	return resolveDefaultDashboardPort(), nil
}

// execStart runs the current binary with the "start" subcommand and waits
// for it to return. This piggy-backs on all the existing start logic
// (binary-mismatch check, readiness poll, etc.) without duplicating it.
func execStart() error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve binary: %w", err)
	}
	cmd := exec.Command(bin, "start")
	cmd.Stdout = os.Stderr // route daemon's "daemon started" message to stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// resolveDefaultDashboardPort returns the dashboard port from the env var
// ARCHIGRAPH_DASHBOARD_PORT, falling back to defaultDashboardPort (47274).
func resolveDefaultDashboardPort() int {
	if v := os.Getenv("ARCHIGRAPH_DASHBOARD_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p <= 65535 {
			return p
		}
	}
	return defaultDashboardPort
}

// openBrowser launches the platform's default browser for url.
// Returns an error if the launch command cannot be started; callers
// treat this as non-fatal (no desktop environment, CI, etc.).
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		// Linux / other Unix: prefer xdg-open, fall back to sensible-browser.
		if _, err := exec.LookPath("xdg-open"); err == nil {
			cmd = exec.Command("xdg-open", url)
		} else {
			cmd = exec.Command("sensible-browser", url)
		}
	}
	// Start is non-blocking — we don't wait for the browser to exit.
	return cmd.Start()
}

// runDashboardServe parses serve-only flags, picks a free port, prints it
// to stdout, and blocks until SIGINT/SIGTERM.
//
// This is the advanced / dev workflow. The daemon's embedded dashboard
// (started via `archigraph start`) is the recommended path for normal use.
func runDashboardServe(argv []string) error {
	fs := flag.NewFlagSet("dashboard serve", flag.ContinueOnError)
	bindOverride := fs.String("bind", "", "override Bind (default from ~/.archigraph/dashboard.json)")
	portOverride := fs.Int("port", 0, "pin to a specific port instead of picking from port_range (useful for Vite proxy)")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	cfg, err := dashboard.LoadConfig()
	if err != nil {
		return fmt.Errorf("load dashboard config: %w", err)
	}
	if *bindOverride != "" {
		cfg.Bind = *bindOverride
	}
	if *portOverride > 0 {
		// Pin the range to a single port so Listen() always picks it.
		cfg.PortRange.Min = *portOverride
		cfg.PortRange.Max = *portOverride
	}

	srv, err := dashboard.NewServer(cfg, dashboard.NewLiveStore())
	if err != nil {
		return err
	}
	port, err := srv.Listen()
	if err != nil {
		return err
	}
	// Per spec: chosen port -> stdout (single line, easy to capture from
	// shell scripts). Logs (including the human-readable banner) -> stderr.
	fmt.Fprintln(os.Stdout, port)
	fmt.Fprintf(os.Stderr, "archigraph dashboard listening on http://%s/\n", srv.Addr())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return srv.Serve(ctx)
}
