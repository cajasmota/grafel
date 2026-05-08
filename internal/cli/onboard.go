package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/install"
	"github.com/cajasmota/archigraph/internal/install/detect"
	"github.com/cajasmota/archigraph/internal/registry"
)

func newOnboardCmd() *cobra.Command {
	var (
		nonInteractive bool
		parentDir      string
	)
	cmd := &cobra.Command{
		Use:   "onboard [path]",
		Short: "Join a teammate's existing group",
		RunE: func(cmd *cobra.Command, args []string) error {
			start, err := os.Getwd()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				start = args[0]
			}
			return runOnboard(cmd.OutOrStdout(), start, parentDir, nonInteractive)
		},
	}
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "skip prompts; clone via clone_url for any missing repo")
	cmd.Flags().StringVar(&parentDir, "parent", "", "parent directory to host sibling repos (defaults to dirname of [path])")
	return cmd
}

func runOnboard(out io.Writer, start, parentDir string, nonInteractive bool) error {
	manifest, err := registry.LoadManifest(start)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}
	if parentDir == "" {
		// If start is a repo, sibling repos live alongside it.
		fi, err := os.Stat(start)
		if err == nil && fi.IsDir() {
			parentDir = filepath.Dir(start)
		}
	}
	cfg := &registry.GroupConfig{Name: manifest.Group}
	cfg.Features.Watchers = true
	cfg.Features.GitHooks = true

	for _, r := range manifest.Repos {
		path := filepath.Join(parentDir, r.Slug)
		if _, err := os.Stat(path); err != nil {
			// Missing — clone or prompt.
			if r.CloneURL != "" {
				if !nonInteractive {
					confirm := false
					_ = huh.NewConfirm().
						Title(fmt.Sprintf("Clone %s into %s?", r.CloneURL, path)).
						Value(&confirm).Run()
					if !confirm {
						continue
					}
				}
				if err := clone(r.CloneURL, path); err != nil {
					fmt.Fprintf(out, "clone %s: %v\n", r.Slug, err)
					continue
				}
			} else if !nonInteractive {
				_ = huh.NewInput().
					Title(fmt.Sprintf("Path for sibling repo %q", r.Slug)).
					Value(&path).Run()
			} else {
				fmt.Fprintf(out, "skipping %s: no clone_url and no local path\n", r.Slug)
				continue
			}
		}
		stack := r.Stack
		if stack == "" {
			stack = detect.Stack(path)
		}
		cfg.Repos = append(cfg.Repos, registry.Repo{
			Slug:     r.Slug,
			Path:     path,
			Stack:    stack,
			CloneURL: r.CloneURL,
		})
	}

	cfgPath, err := registry.ConfigPathFor(cfg.Name)
	if err != nil {
		return err
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		return err
	}
	if err := registry.AddGroup(cfg.Name, cfgPath); err != nil {
		return err
	}
	bin, _ := os.Executable()
	res, err := install.Apply(install.Options{Group: cfg.Name, Config: cfg, BinPath: bin})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "onboarded group %q (%d repos, %d hooks, %d watchers)\n",
		cfg.Name, len(cfg.Repos), len(res.HooksInstalled), len(res.WatcherUnits))
	return nil
}

func clone(url, dst string) error {
	cmd := exec.Command("git", "clone", url, dst)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
