// Command verify-dashboard is the CI / release guard for #4468: it fails when
// the embedded dashboard bundle (internal/dashboard/dist) is STALE relative to
// the freshly built SPA (webui-v2/dist).
//
// `make dashboard-build` builds webui-v2/dist and then copies it into
// internal/dashboard/dist so `//go:embed dist` picks up the fresh UI. A manual
// or CI `vite build && go build` that skips the copy silently re-embeds the OLD
// bundle — the daemon then serves a months-old UI while reporting the new
// commit. This guard catches exactly that drift.
//
// Usage:
//
//	verify-dashboard [-root <repo-root>]
//
// Exit 0 when the embed is current (or no fresh build exists to compare);
// exit 1 when the embed is stale.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cajasmota/archigraph/internal/dashboard"
)

func main() {
	root := flag.String("root", ".", "repo root containing webui-v2/ and internal/dashboard/")
	flag.Parse()

	abs, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify-dashboard:", err)
		os.Exit(2)
	}
	if err := dashboard.VerifyDashboardEmbed(abs); err != nil {
		fmt.Fprintln(os.Stderr, "verify-dashboard: FAIL —", err)
		os.Exit(1)
	}
	fmt.Println("verify-dashboard: OK — embedded dashboard bundle is current")
}
