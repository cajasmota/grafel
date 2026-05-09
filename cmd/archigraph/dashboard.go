package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cajasmota/archigraph/internal/dashboard"
)

// runDashboard is wired into cli.Hooks and dispatches the `dashboard`
// subcommand verbs. Currently only `serve` is implemented; future verbs
// (open, status) plug in here.
func runDashboard(argv []string) error {
	if len(argv) == 0 || argv[0] == "-h" || argv[0] == "--help" {
		fmt.Fprintln(os.Stderr, "usage: archigraph dashboard <serve> [flags]")
		return nil
	}
	switch argv[0] {
	case "serve":
		return runDashboardServe(argv[1:])
	default:
		return fmt.Errorf("unknown dashboard verb: %s", argv[0])
	}
}

// runDashboardServe parses serve-only flags, picks a free port, prints it
// to stdout, and blocks until SIGINT/SIGTERM.
func runDashboardServe(argv []string) error {
	fs := flag.NewFlagSet("dashboard serve", flag.ContinueOnError)
	bindOverride := fs.String("bind", "", "override Bind (default from ~/.archigraph/dashboard.json)")
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
