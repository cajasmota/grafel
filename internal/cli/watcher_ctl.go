package cli

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/install/watchers"
	"github.com/cajasmota/archigraph/internal/registry"
)

// start/stop/restart fan out across every repo in either a named group
// or every registered group. The OS-native loader is the source of
// truth for whether a watcher is actually running; we drive it via
// launchctl/systemctl/schtasks so behavior matches `archigraph status`.

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start [group]",
		Short: "Start watchers (omit group to fan out)",
		RunE:  watcherCtlRunE("start"),
	}
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [group]",
		Short: "Stop watchers",
		RunE:  watcherCtlRunE("stop"),
	}
}

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart [group]",
		Short: "Restart watchers",
		RunE:  watcherCtlRunE("restart"),
	}
}

func watcherCtlRunE(action string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		filter := ""
		if len(args) == 1 {
			filter = args[0]
		}
		groups, err := registry.Groups()
		if err != nil {
			return err
		}
		for _, g := range groups {
			if filter != "" && g.Name != filter {
				continue
			}
			cfg, err := registry.LoadGroupConfig(g.ConfigPath)
			if err != nil {
				continue
			}
			for _, r := range cfg.Repos {
				u := watchers.Unit{Group: g.Name, Repo: r.Path}
				if err := osWatcherCtl(action, u); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "watch %s %s: %v\n", action, u.Label(), err)
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", action, u.Label())
			}
		}
		return nil
	}
}

// osWatcherCtl invokes the OS-native loader. We deliberately swallow
// nothing — the caller decides how to format errors.
func osWatcherCtl(action string, u watchers.Unit) error {
	switch runtime.GOOS {
	case "darwin":
		path, err := watchers.UnitPath(u)
		if err != nil {
			return err
		}
		switch action {
		case "start":
			return exec.Command("launchctl", "load", "-w", path).Run()
		case "stop":
			return exec.Command("launchctl", "unload", path).Run()
		case "restart":
			_ = exec.Command("launchctl", "unload", path).Run()
			return exec.Command("launchctl", "load", "-w", path).Run()
		}
	case "linux":
		unit := u.Label() + ".service"
		switch action {
		case "start":
			return exec.Command("systemctl", "--user", "start", unit).Run()
		case "stop":
			return exec.Command("systemctl", "--user", "stop", unit).Run()
		case "restart":
			return exec.Command("systemctl", "--user", "restart", unit).Run()
		}
	case "windows":
		switch action {
		case "start":
			return exec.Command("schtasks", "/run", "/tn", u.Label()).Run()
		case "stop":
			return exec.Command("schtasks", "/end", "/tn", u.Label()).Run()
		case "restart":
			_ = exec.Command("schtasks", "/end", "/tn", u.Label()).Run()
			return exec.Command("schtasks", "/run", "/tn", u.Label()).Run()
		}
	}
	return fmt.Errorf("unsupported action %q on %s", action, runtime.GOOS)
}
