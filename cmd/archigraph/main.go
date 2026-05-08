package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/cajasmota/archigraph/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "-v", "--version", "version":
		fmt.Println(version.String())
		return
	case "-h", "--help", "help":
		usage()
		return
	case "index":
		if err := runIndex(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "archigraph index: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fmt.Fprintf(os.Stderr, "archigraph: unknown command: %s\n\n", os.Args[1])
	usage()
	os.Exit(2)
}

func usage() {
	fmt.Fprintln(os.Stderr, "archigraph — multi-repo code knowledge graphs for AI agents")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  archigraph index <repo>      Walk a repository and write graph.json.")
	fmt.Fprintln(os.Stderr, "  archigraph version           Print the build version.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Run 'archigraph index --help' for index-specific options.")
}

// runIndex parses flags for the `index` subcommand and runs the indexer.
func runIndex(argv []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	out := fs.String("out", "", "output path for graph.json (default: <repo>/.archigraph/graph.json)")
	repoTag := fs.String("repo-tag", "", "repository tag stored on entities (default: dirname of repo path)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("missing <repo> argument")
	}
	repoPath := fs.Arg(0)
	return Index(repoPath, *out, *repoTag)
}
