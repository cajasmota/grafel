// Command lint-localematch is a low-false-positive guard against the
// locale-bug CLASS that broke grafel on Spanish Windows (#5317, #856): code
// that branches on a *localized, human-readable* OS error string — e.g.
// `strings.Contains(out, "cannot find")` on the output/error of a shelled-out
// command. Such matches silently stop matching when the OS speaks another
// language, producing wrong control flow.
//
// The right signals are locale-invariant: process exit codes
// (exec.ExitError.ExitCode()), typed/sentinel errors, structured output
// (CSV/JSON), or native Go APIs. This linter flags string/regexp matches that
// are data-flow-derived from command output or a command error, so a new
// occurrence fails CI before it ships.
//
// Heuristic (accuracy over recall — it is a guard, not a prover):
//
//   - A file is "in scope" only if it calls exec.Command / exec.CommandContext.
//     Pure source-parsing extractors that happen to use strings.Contains are
//     never flagged.
//   - Within such a file, a call to one of the matcher funcs
//     (strings.Contains/HasPrefix/HasSuffix/Index/EqualFold,
//     regexp.MatchString / (*Regexp).MatchString) is flagged when its *subject*
//     argument is derived from command output. The subject is recognized as:
//   - a variable whose name looks like captured output
//     (out, output, stdout, stderr, combined, b, s, ...) AND that variable
//     was assigned from a .Output()/.CombinedOutput()/.Run() call, OR
//   - an `<x>.Error()` call (matching on an error message string), where the
//     file is in scope (an exec error), OR
//   - a `string(<outputVar>)` conversion of such a variable.
//   - A `//nolint:localematch` comment on the same line (or the line above)
//     suppresses the finding — reserved for justified best-effort *race
//     fallbacks* whose PRIMARY decision is already exit-code/structured (the
//     Unload() template).
//
// Usage:
//
//	go run ./cmd/lint-localematch [packages-root ...]   # default: internal cmd
//
// Exit status is non-zero when any un-suppressed violation is found.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// matcherFuncs are the text-comparison functions we treat as control-flow
// matchers. Keyed by "pkg.Func"; (*regexp.Regexp).MatchString is handled by the
// method-name set below.
var matcherFuncs = map[string]bool{
	"strings.Contains":   true,
	"strings.HasPrefix":  true,
	"strings.HasSuffix":  true,
	"strings.Index":      true,
	"strings.EqualFold":  true,
	"regexp.MatchString": true,
}

// matcherMethods are method names (any receiver) we treat as matchers — covers
// (*regexp.Regexp).MatchString / .Match. Method receivers are not resolved
// without type info, so we additionally require an output-derived argument,
// keeping false positives low.
var matcherMethods = map[string]bool{
	"MatchString": true,
}

// outputCaptureMethods mark a value as "captured command output" when a
// variable is assigned from a call ending in one of these selector names.
var outputCaptureMethods = map[string]bool{
	"Output":         true,
	"CombinedOutput": true,
}

// outputishNames are variable names that, combined with an in-scope (exec-using)
// file, we treat as likely command output. Used as a fallback when we cannot
// trace the assignment (e.g. `s := string(out)` chains across statements).
var outputishNames = map[string]bool{
	"out": true, "output": true, "stdout": true, "stderr": true,
	"combined": true, "outBytes": true, "stdoutBytes": true,
}

type finding struct {
	file string
	line int
	col  int
	fn   string
	arg  string
}

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		roots = []string{"internal", "cmd"}
	}

	var files []string
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if d.Name() == "testdata" || d.Name() == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				files = append(files, path)
			}
			return nil
		})
	}
	sort.Strings(files)

	var findings []finding
	for _, f := range files {
		fs, err := analyzeFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lint-localematch: parse %s: %v\n", f, err)
			os.Exit(2)
		}
		findings = append(findings, fs...)
	}

	if len(findings) == 0 {
		fmt.Println("lint-localematch: ok — no localized command-output matching found")
		return
	}

	fmt.Fprintln(os.Stderr, "lint-localematch: localized command-output / error-string matching detected.")
	fmt.Fprintln(os.Stderr, "Branch on locale-invariant signals instead: exit codes (exec.ExitError.ExitCode()),")
	fmt.Fprintln(os.Stderr, "typed/sentinel errors, or structured output (CSV/JSON). See #5317.")
	fmt.Fprintln(os.Stderr, "Justified best-effort race fallbacks may add a // nolint:localematch comment.")
	fmt.Fprintln(os.Stderr, "")
	for _, fnd := range findings {
		fmt.Fprintf(os.Stderr, "  %s:%d:%d  %s(%s)\n", fnd.file, fnd.line, fnd.col, fnd.fn, fnd.arg)
	}
	os.Exit(1)
}

func analyzeFile(path string) ([]finding, error) {
	fset := token.NewFileSet()
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	// Scope gate: only files that shell out are considered.
	if !fileUsesExec(file) {
		return nil, nil
	}

	// Build the set of variables known to hold captured command output, by
	// scanning every assignment whose RHS is a *.Output()/*.CombinedOutput()
	// call (the lhs name(s) before the err) or a string(<outputVar>) conversion.
	outputVars := collectOutputVars(file)

	// Collect //nolint:localematch suppression lines.
	suppressed := collectSuppressions(fset, file)

	var findings []finding
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fnName, isMatcher := matcherName(call)
		if !isMatcher {
			return true
		}
		// The subject argument: arg 0 for strings.*; for regexp.MatchString the
		// subject (the text being searched) is the 2nd arg; for (*Regexp).Match*
		// the subject is the 1st arg. We check all string args to be safe.
		subjectArgs := matcherSubjectArgs(call, fnName)
		for _, arg := range subjectArgs {
			if !argDerivesFromOutput(arg, outputVars) {
				continue
			}
			pos := fset.Position(call.Pos())
			if suppressed[pos.Line] {
				continue
			}
			findings = append(findings, finding{
				file: path, line: pos.Line, col: pos.Column,
				fn: fnName, arg: exprString(arg),
			})
			break
		}
		return true
	})
	return findings, nil
}

func fileUsesExec(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "exec" {
			if sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext" {
				found = true
			}
		}
		return true
	})
	return found
}

// collectOutputVars returns the set of identifier names assigned from a command
// output capture (.Output()/.CombinedOutput()) or a string()/[]byte conversion
// chain of such a variable.
func collectOutputVars(file *ast.File) map[string]bool {
	vars := map[string]bool{}
	// Iterate to a fixed point so `out := cmd.Output(); s := string(out)` both
	// land in the set regardless of statement order within a func.
	for changed := true; changed; {
		changed = false
		ast.Inspect(file, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, rhs := range assign.Rhs {
				if !rhsIsOutput(rhs, vars) {
					continue
				}
				// Map to the corresponding lhs name (1:1), or — for the common
				// `out, err := cmd.Output()` — the first lhs.
				lhsIdx := i
				if len(assign.Lhs) == 1 {
					lhsIdx = 0
				}
				if lhsIdx < len(assign.Lhs) {
					if id, ok := assign.Lhs[lhsIdx].(*ast.Ident); ok && id.Name != "_" {
						if !vars[id.Name] {
							vars[id.Name] = true
							changed = true
						}
					}
				}
			}
			return true
		})
	}
	return vars
}

// rhsIsOutput reports whether an RHS expression yields captured command output:
// a call to *.Output()/*.CombinedOutput(), or string(<knownOutputVar>) /
// strings.TrimSpace(<knownOutputVar>) and similar single-arg wrappers.
func rhsIsOutput(rhs ast.Expr, known map[string]bool) bool {
	call, ok := rhs.(*ast.CallExpr)
	if !ok {
		return false
	}
	// *.Output() / *.CombinedOutput()
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if outputCaptureMethods[sel.Sel.Name] {
			return true
		}
	}
	// string(out) / []byte(out) / strings.TrimSpace(out) — propagate from a
	// known output var passed as the sole interesting arg.
	for _, a := range call.Args {
		if id, ok := a.(*ast.Ident); ok && known[id.Name] {
			return true
		}
	}
	return false
}

// matcherName classifies a call expression as a known matcher. Returns the
// canonical "pkg.Func" (or method name) and whether it matched.
func matcherName(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	// Package-qualified: strings.Contains, regexp.MatchString.
	if pkg, ok := sel.X.(*ast.Ident); ok {
		full := pkg.Name + "." + sel.Sel.Name
		if matcherFuncs[full] {
			return full, true
		}
	}
	// Method call: re.MatchString(...). Require it to be a recognized matcher
	// method name; the output-derived-arg check downstream keeps this precise.
	if matcherMethods[sel.Sel.Name] {
		return sel.Sel.Name, true
	}
	return "", false
}

// matcherSubjectArgs returns the argument expressions of a matcher call that
// represent the text being searched (the "haystack").
func matcherSubjectArgs(call *ast.CallExpr, fnName string) []ast.Expr {
	switch fnName {
	case "regexp.MatchString":
		// MatchString(pattern, s) — subject is arg 1.
		if len(call.Args) >= 2 {
			return call.Args[1:2]
		}
	case "MatchString":
		// (*Regexp).MatchString(s) — subject is arg 0.
		if len(call.Args) >= 1 {
			return call.Args[0:1]
		}
	default:
		// strings.Contains(s, substr) etc. — subject is arg 0.
		if len(call.Args) >= 1 {
			return call.Args[0:1]
		}
	}
	return nil
}

// argDerivesFromOutput reports whether an argument expression is data-flow
// derived from command output or a command error message.
func argDerivesFromOutput(arg ast.Expr, outputVars map[string]bool) bool {
	switch e := arg.(type) {
	case *ast.Ident:
		// `strings.Contains(s, ...)` where s := string(out) etc.
		return outputVars[e.Name] || outputishNames[e.Name]
	case *ast.CallExpr:
		// string(out) inline, strings.ToLower(string(out)), err.Error().
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Error" {
			// <something>.Error() — matching on an error message string. Since
			// the file is in scope (uses exec), this is an exec/OS error.
			return true
		}
		for _, a := range e.Args {
			if argDerivesFromOutput(a, outputVars) {
				return true
			}
		}
	case *ast.ParenExpr:
		return argDerivesFromOutput(e.X, outputVars)
	}
	return false
}

// collectSuppressions returns the set of line numbers carrying a
// //nolint:localematch (or //nolint:localematch,...) directive, on the matcher
// line or the line immediately above.
func collectSuppressions(fset *token.FileSet, file *ast.File) map[int]bool {
	lines := map[int]bool{}
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
			if !strings.HasPrefix(text, "nolint") {
				continue
			}
			if !strings.Contains(text, "localematch") {
				continue
			}
			ln := fset.Position(c.Pos()).Line
			lines[ln] = true   // same-line trailing comment
			lines[ln+1] = true // comment on the line above the statement
		}
	}
	return lines
}

func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.CallExpr:
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
			return exprString(sel.X) + "." + sel.Sel.Name + "(…)"
		}
		return "…(…)"
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	case *ast.ParenExpr:
		return "(" + exprString(v.X) + ")"
	}
	return "…"
}
