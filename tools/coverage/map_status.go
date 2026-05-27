package main

// `map-status` subcommand: show the capability-map.yaml entry for a
// single capability coordinate, with on-disk existence checks for each
// cited symbol and test. Useful when investigating drift between the
// registry and the implementing code.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MapStatusReport is the JSON shape emitted by `map-status --json`.
// Fields are oriented around answering "is this mapping entry still
// in sync with the code?" — every cited path carries its existence
// outcome inline.
type MapStatusReport struct {
	RecordID    string             `json:"record_id"`
	Group       string             `json:"group,omitempty"`
	Capability  string             `json:"capability"`
	Found       bool               `json:"found"`
	Status      string             `json:"status,omitempty"`
	VerifiedAt  string             `json:"verified_at,omitempty"`
	Issues      []string           `json:"issues_implemented,omitempty"`
	Symbols     []MapStatusSymbol  `json:"symbols,omitempty"`
	Tests       []MapStatusTest    `json:"tests,omitempty"`
	SymbolsOK   int                `json:"symbols_ok"`
	SymbolsBad  int                `json:"symbols_missing"`
	FuncsOK     int                `json:"functions_ok"`
	FuncsBad    int                `json:"functions_missing"`
	TestsOK     int                `json:"tests_ok"`
	TestsBad    int                `json:"tests_missing"`
}

// MapStatusSymbol is one symbol citation enriched with per-function
// existence results.
type MapStatusSymbol struct {
	File      string                  `json:"file"`
	Exists    bool                    `json:"exists"`
	Functions []MapStatusFunction     `json:"functions,omitempty"`
}

// MapStatusFunction is one (file, function) tuple's existence check.
type MapStatusFunction struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
}

// MapStatusTest is one test-file citation's existence check.
type MapStatusTest struct {
	File   string `json:"file"`
	Exists bool   `json:"exists"`
}

func cmdMapStatus(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("map-status", flag.ContinueOnError)
	repoRoot := fs.String("repo-root", ".", "repository root used to resolve cite paths")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("map-status: expected exactly one argument <record-id>[/<group>]/<capability>")
	}
	recID, group, capKey, err := parseMapStatusKey(fs.Arg(0))
	if err != nil {
		return err
	}
	cm, err := LoadCapabilityMap(*repoRoot)
	if err != nil {
		return err
	}
	if cm == nil {
		return fmt.Errorf("map-status: no capability-map.yaml found under %s", *repoRoot)
	}
	mapRec, ok := cm.Records[recID]
	if !ok {
		return fmt.Errorf("map-status: record %q not in capability-map.yaml", recID)
	}
	entry, ok := mapRec.Lookup(group, capKey)
	report := MapStatusReport{
		RecordID:   recID,
		Group:      group,
		Capability: capKey,
		Found:      ok,
	}
	if !ok {
		if *asJSON {
			return printJSON(out, report)
		}
		fmt.Fprintf(out, "no mapping entry for %s\n", formatMapKey(recID, group, capKey))
		return nil
	}
	report.Status = entry.Status
	report.VerifiedAt = entry.VerifiedAt
	report.Issues = entry.IssuesImplemented
	for _, sym := range entry.Symbols {
		fullPath := filepath.Join(*repoRoot, sym.File)
		fileExists := pathExists(fullPath)
		sr := MapStatusSymbol{File: sym.File, Exists: fileExists}
		if fileExists {
			report.SymbolsOK++
		} else {
			report.SymbolsBad++
		}
		var decls map[string]bool
		if fileExists && len(sym.Functions) > 0 {
			decls, _ = scanDeclarations(fullPath)
		}
		for _, fn := range sym.Functions {
			exists := decls[fn]
			sr.Functions = append(sr.Functions, MapStatusFunction{Name: fn, Exists: exists})
			if exists {
				report.FuncsOK++
			} else {
				report.FuncsBad++
			}
		}
		report.Symbols = append(report.Symbols, sr)
	}
	for _, t := range entry.Tests {
		exists := pathExists(filepath.Join(*repoRoot, t.File))
		report.Tests = append(report.Tests, MapStatusTest{File: t.File, Exists: exists})
		if exists {
			report.TestsOK++
		} else {
			report.TestsBad++
		}
	}
	if *asJSON {
		return printJSON(out, report)
	}
	printMapStatusText(out, report)
	return nil
}

// parseMapStatusKey accepts either "<id>/<key>" (flat) or
// "<id>/<group>/<key>" (grouped) and returns the three components.
// Records IDs are dotted slugs and never contain "/", so "/" is an
// unambiguous separator.
func parseMapStatusKey(arg string) (recID, group, capKey string, err error) {
	parts := strings.Split(arg, "/")
	switch len(parts) {
	case 2:
		return parts[0], "", parts[1], nil
	case 3:
		return parts[0], parts[1], parts[2], nil
	default:
		return "", "", "", fmt.Errorf("map-status: argument %q must be <record-id>/<capability> or <record-id>/<group>/<capability>", arg)
	}
}

// formatMapKey is the inverse of parseMapStatusKey, used in text output.
func formatMapKey(recID, group, capKey string) string {
	if group == "" {
		return recID + "/" + capKey
	}
	return recID + "/" + group + "/" + capKey
}

// pathExists returns true when path exists and is not a directory.
func pathExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// printMapStatusText is the human-readable renderer for `map-status`.
// Tags each cited path with ok or MISSING so a quick visual scan
// surfaces drift.
func printMapStatusText(out io.Writer, r MapStatusReport) {
	fmt.Fprintf(out, "%s\n", formatMapKey(r.RecordID, r.Group, r.Capability))
	fmt.Fprintf(out, "  status:       %s\n", r.Status)
	if r.VerifiedAt != "" {
		fmt.Fprintf(out, "  verified_at:  %s\n", r.VerifiedAt)
	}
	if len(r.Issues) > 0 {
		fmt.Fprintf(out, "  issues:       %s\n", strings.Join(r.Issues, ", "))
	}
	if len(r.Symbols) > 0 {
		fmt.Fprintln(out, "  symbols:")
		for _, sym := range r.Symbols {
			fmt.Fprintf(out, "    %s  %s\n", existsTag(sym.Exists), sym.File)
			for _, fn := range sym.Functions {
				fmt.Fprintf(out, "      %s  %s\n", existsTag(fn.Exists), fn.Name)
			}
		}
	}
	if len(r.Tests) > 0 {
		fmt.Fprintln(out, "  tests:")
		for _, t := range r.Tests {
			fmt.Fprintf(out, "    %s  %s\n", existsTag(t.Exists), t.File)
		}
	}
	fmt.Fprintf(out, "  checks: symbols %d/%d, functions %d/%d, tests %d/%d\n",
		r.SymbolsOK, r.SymbolsOK+r.SymbolsBad,
		r.FuncsOK, r.FuncsOK+r.FuncsBad,
		r.TestsOK, r.TestsOK+r.TestsBad)
}

// existsTag formats a per-line existence indicator. Plain ASCII keeps
// the output greppable and stable across terminals.
func existsTag(ok bool) string {
	if ok {
		return "[ok]     "
	}
	return "[MISSING]"
}

