package main

// quality_fitness.go — `archigraph quality check [--strict] [--json] <group-or-repo-path>`
//
// Evaluates the architectural fitness rules defined in
// <repo>/.archigraph/fitness.yaml against the live graph for the given group
// (or a single repo path) and prints a human-readable violation report.
//
// Exit codes:
//
//	0  all rules passed (or only warn/info violations found without --strict)
//	1  internal error
//	2  at least one error-severity rule violated
//
// With --strict, any violation (including warn/info) causes exit 2.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/quality/fitness"
	"github.com/cajasmota/archigraph/internal/registry"
)

// runQualityCheck is the entry point for `archigraph quality check ...`.
func runQualityCheck(argv []string) error {
	fs := flag.NewFlagSet("quality check", flag.ContinueOnError)
	strict := fs.Bool("strict", false, "exit non-zero on any violation (including warn/info)")
	jsonOut := fs.Bool("json", false, "emit JSON report to stdout instead of human text")
	outPath := fs.String("output", "", "write output to this file (default: stdout)")

	if err := fs.Parse(argv); err != nil {
		return err
	}

	// Collect graph documents to evaluate.
	type repoDoc struct {
		slug    string
		repoDir string
		doc     *graph.Document
	}
	var repoDocs []repoDoc

	if fs.NArg() == 0 {
		return fmt.Errorf("usage: archigraph quality check [--strict] [--json] <group-name|repo-path>")
	}

	target := fs.Arg(0)

	// Try to resolve as a group name first.
	groups, _ := registry.Groups()
	var resolvedGroup string
	for _, g := range groups {
		if g.Name == target {
			resolvedGroup = g.Name
			break
		}
	}

	if resolvedGroup != "" {
		// Load every repo in the group.
		for _, g := range groups {
			if g.Name != resolvedGroup {
				continue
			}
			cfg, err := registry.LoadGroupConfig(g.ConfigPath)
			if err != nil {
				return fmt.Errorf("load group config: %w", err)
			}
			for _, r := range cfg.Repos {
				stateDir := filepath.Join(r.Path, ".archigraph")
				doc, err := graph.LoadGraphFromDir(stateDir)
				if err != nil {
					fmt.Fprintf(os.Stderr, "quality check: skip %s (%v)\n", r.Slug, err)
					continue
				}
				repoDocs = append(repoDocs, repoDoc{slug: r.Slug, repoDir: r.Path, doc: doc})
			}
		}
		if len(repoDocs) == 0 {
			return fmt.Errorf("group %q has no indexed repos", target)
		}
	} else {
		// Treat as a direct repo path.
		absPath, err := filepath.Abs(target)
		if err != nil {
			return fmt.Errorf("resolve path: %w", err)
		}
		stateDir := filepath.Join(absPath, ".archigraph")
		doc, err := graph.LoadGraphFromDir(stateDir)
		if err != nil {
			return fmt.Errorf("load graph from %s: %w", stateDir, err)
		}
		repoDocs = append(repoDocs, repoDoc{slug: filepath.Base(absPath), repoDir: absPath, doc: doc})
	}

	// Evaluate fitness rules for each repo document.
	type repoResult struct {
		Slug   string              `json:"slug"`
		Result *fitness.EvalResult `json:"result"`
	}
	var results []repoResult
	totalErrors, totalWarns, totalInfos := 0, 0, 0

	for _, rd := range repoDocs {
		stateDir := filepath.Join(rd.repoDir, ".archigraph")
		fitCfg, err := fitness.LoadConfig(stateDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "quality check: %s: %v (skipping)\n", rd.slug, err)
			continue
		}
		if len(fitCfg.Rules) == 0 && len(repoDocs) > 1 {
			// In multi-repo mode, skip repos without a fitness config silently.
			continue
		}
		evalResult := fitness.Evaluate(fitCfg, rd.doc)
		results = append(results, repoResult{Slug: rd.slug, Result: evalResult})
		totalErrors += evalResult.ErrorCount
		totalWarns += evalResult.WarnCount
		totalInfos += evalResult.InfoCount
	}

	if len(results) == 0 {
		if *jsonOut {
			fmt.Println(`{"repos":[],"message":"no fitness.yaml found — nothing to check"}`)
			return nil
		}
		fmt.Fprintln(os.Stderr, "quality check: no .archigraph/fitness.yaml found — nothing to check")
		fmt.Fprintln(os.Stderr, "  Tip: create .archigraph/fitness.yaml in any indexed repo.")
		return nil
	}

	// Output.
	var out *os.File = os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer f.Close()
		out = f
	}

	if *jsonOut {
		type jsonRoot struct {
			Repos       []repoResult `json:"repos"`
			TotalErrors int          `json:"total_errors"`
			TotalWarns  int          `json:"total_warns"`
			TotalInfos  int          `json:"total_infos"`
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(jsonRoot{
			Repos:       results,
			TotalErrors: totalErrors,
			TotalWarns:  totalWarns,
			TotalInfos:  totalInfos,
		}); encErr != nil {
			return encErr
		}
	} else {
		for _, rr := range results {
			writeCheckHuman(out, rr.Slug, rr.Result)
		}
		// Summary line.
		fmt.Fprintf(out, "\nsummary: %d error(s), %d warn(s), %d info(s)\n", totalErrors, totalWarns, totalInfos)
		if totalErrors == 0 && totalWarns == 0 {
			fmt.Fprintln(out, "all fitness rules passed ✓")
		}

		// Print suggested rules (for repos with no existing rules).
		for _, rr := range results {
			if len(rr.Result.SuggestedRules) > 0 && rr.Result.TotalRules == 0 {
				fmt.Fprintf(out, "\nsuggested rules for %s (add to .archigraph/fitness.yaml):\n", rr.Slug)
				for _, s := range rr.Result.SuggestedRules {
					fmt.Fprintf(out, "  # %s\n  # %s\n  %s\n\n", s.Name, s.Reason, indentYAML(s.YAML, "  "))
				}
			}
		}
	}

	// Exit code.
	if totalErrors > 0 {
		os.Exit(2)
	}
	if *strict && (totalWarns > 0 || totalInfos > 0) {
		os.Exit(2)
	}
	return nil
}

func writeCheckHuman(out *os.File, slug string, r *fitness.EvalResult) {
	fmt.Fprintf(out, "repo: %s  (%d rules, %d passed, %d failed)\n", slug, r.TotalRules, r.PassedRules, r.FailedRules)
	for _, rr := range r.Results {
		if rr.Passed {
			fmt.Fprintf(out, "  [PASS] %s\n", rr.Rule.Name)
			continue
		}
		fmt.Fprintf(out, "  [FAIL] %s\n", rr.Rule.Name)
		for _, v := range rr.Violations {
			icon := severityIcon(v.Severity)
			fmt.Fprintf(out, "         %s %s\n", icon, v.Message)
			if v.FromEntity != nil && v.FromEntity.SourceFile != "" {
				fmt.Fprintf(out, "              at %s\n", v.FromEntity.SourceFile)
			}
		}
	}
}

func severityIcon(sev string) string {
	switch sev {
	case "warn":
		return "WARN"
	case "info":
		return "INFO"
	default:
		return "ERR "
	}
}

func indentYAML(yaml, prefix string) string {
	lines := strings.Split(yaml, "\n")
	for i, l := range lines {
		if i > 0 {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
