// CLI command entry-point detection (epic #3628).
//
// This pass scans file content for every major command-line framework and
// emits synthetic SCOPE.Command entities plus HANDLES_COMMAND edges from each
// command to its handler function — the CLI sibling of an HTTP endpoint's
// route → handler join. A SCOPE.Command models a statically-declared
// command-line command (or subcommand path); the HANDLES_COMMAND edge points
// at the function that runs when that command is invoked.
//
// Frameworks covered:
//
//	Python click      — @cli.command('name') / @click.command() on a def; groups
//	Python argparse    — subparsers.add_parser('name') + set_defaults(func=handler)
//	Python typer       — @app.command() on a def
//	Node commander     — program.command('serve').action(handler)
//	Node yargs         — .command('build', desc, builder, handler)
//	Node oclif         — a class extends Command → its run() method
//	Go cobra           — &cobra.Command{Use: "serve", Run: serveFn}
//	Java picocli       — @Command(name="serve") class → its call()/run()
//	Java Spring Shell  — @ShellMethod on a method
//	Ruby Thor          — desc 'build ...' + def build
//	Ruby Rake          — task :build do ... end
//
// Honest-partial: dynamic command names (`command(nameVar)`) and dynamic
// handler references are skipped — we never fabricate an edge to a name we
// cannot resolve statically. Each detector is gated behind a framework-import
// pre-filter so a non-CLI `.command()` / `.action()` on an unrelated object
// (e.g. a DB driver) does not mint a phantom command.
//
// All emissions are append-only — existing entities and edges are never
// modified or removed, so this pass cannot regress surrounding passes.
//
// Refs epic #3628.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// cliCommandKind is the entity kind for CLI command entry points.
const cliCommandKind = string(types.EntityKindCommand) // "SCOPE.Command"

// handlesCommandEdgeKind is the edge from a Command to its handler function.
const handlesCommandEdgeKind = string(types.RelationshipKindHandlesCommand) // "HANDLES_COMMAND"

// applyCLICommandEdges is the per-file entry point. Appends SCOPE.Command
// entities + HANDLES_COMMAND edges; never modifies or removes existing
// entities or edges. Language dispatches to per-framework helpers.
func applyCLICommandEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	src := string(content)

	seenCmd := map[string]bool{}

	// emitCommand appends a SCOPE.Command entity keyed `<framework>:<path>:<name>`
	// and, when a handler is resolved, a HANDLES_COMMAND edge to Function:<handler>
	// (the same function-target convention scheduled_jobs_edges.go uses for
	// TRIGGERS). An empty handler emits the command node only (honest-partial).
	emitCommand := func(cmdID, name, handler, framework string, extra map[string]string) {
		if cmdID == "" || name == "" {
			return
		}
		if seenCmd[cmdID] {
			return
		}
		seenCmd[cmdID] = true
		props := map[string]string{
			"command":      name,
			"handler":      handler,
			"framework":    framework,
			"pattern_type": "cli_command_synthesis",
		}
		for k, v := range extra {
			if v != "" {
				props[k] = v
			}
		}
		entities = append(entities, types.EntityRecord{
			Name:               cmdID,
			Kind:               cliCommandKind,
			SourceFile:         path,
			Language:           lang,
			Properties:         props,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
		if handler == "" {
			return
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID: cliCommandKind + ":" + cmdID,
			ToID:   "Function:" + handler,
			Kind:   handlesCommandEdgeKind,
			Properties: map[string]string{
				"command":      name,
				"framework":    framework,
				"pattern_type": "cli_command_synthesis",
			},
		})
	}

	switch lang {
	case "python":
		synthesizePyClick(src, path, emitCommand)
		synthesizePyTyper(src, path, emitCommand)
		synthesizePyArgparse(src, path, emitCommand)
	case "javascript", "typescript":
		synthesizeNodeCommander(src, path, emitCommand)
		synthesizeNodeYargs(src, path, emitCommand)
		synthesizeNodeOclif(src, path, emitCommand)
	case "go":
		synthesizeGoCobra(src, path, emitCommand)
	case "java", "kotlin":
		synthesizeJavaPicocli(src, path, emitCommand)
		synthesizeJavaSpringShell(src, path, emitCommand)
	case "ruby":
		synthesizeRubyThor(src, path, emitCommand)
		synthesizeRubyRake(src, path, emitCommand)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// ---------------------------------------------------------------------------
// Python — click / typer (decorator forms)
// ---------------------------------------------------------------------------

// pyClickCommandRe matches `@<group>.command('name')` / `@click.command("name")`
// / `@cli.command()` (no name) immediately above a `def <handler>(`. The
// decorator's optional explicit name wins; otherwise the handler def name is the
// command name (click derives it from the function, dashes for underscores).
// Group 1 = explicit command name (may be empty), group 2 = handler def name.
var pyClickCommandRe = regexp.MustCompile(`(?m)@(?:\w+)\.command\s*\(\s*(?:['"]([^'"]+)['"])?[^\n)]*\)\s*(?:\n\s*@[^\n]*)*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)

// synthesizePyClick emits a SCOPE.Command per @<x>.command(...) decorated def.
// Requires a click import so a generic `.command()` on a non-click object does
// not fabricate a command (the negative-test guard).
func synthesizePyClick(
	src, path string,
	emit func(cmdID, name, handler, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "click") {
		return
	}
	for _, m := range pyClickCommandRe.FindAllStringSubmatch(src, -1) {
		explicit := m[1]
		handler := m[2]
		name := explicit
		if name == "" {
			// click's default: function name with underscores → dashes.
			name = strings.ReplaceAll(handler, "_", "-")
		}
		cmdID := "click:" + path + ":" + name
		emit(cmdID, name, handler, "click", nil)
	}
}

// pyTyperCommandRe matches `@app.command()` / `@app.command("name")` above a
// `def <handler>(`. Typer apps conventionally bind the decorator to a variable
// named `app`, but any `@<x>.command(` is accepted when a typer import is
// present. Group 1 = explicit name (may be empty), group 2 = handler.
var pyTyperCommandRe = regexp.MustCompile(`(?m)@(?:\w+)\.command\s*\(\s*(?:['"]([^'"]+)['"])?[^\n)]*\)\s*(?:\n\s*@[^\n]*)*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)

// synthesizePyTyper emits a SCOPE.Command per typer @app.command() def. Gated on
// a typer import. click and typer share the `.command()` decorator shape, so the
// framework attribution is import-driven: this runs only when `typer` is present
// and (to avoid double-emitting on a file that imports both) click is NOT.
func synthesizePyTyper(
	src, path string,
	emit func(cmdID, name, handler, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "typer") {
		return
	}
	if strings.Contains(src, "click") {
		// Ambiguous file imports both; let synthesizePyClick own it.
		return
	}
	for _, m := range pyTyperCommandRe.FindAllStringSubmatch(src, -1) {
		explicit := m[1]
		handler := m[2]
		name := explicit
		if name == "" {
			name = strings.ReplaceAll(handler, "_", "-")
		}
		cmdID := "typer:" + path + ":" + name
		emit(cmdID, name, handler, "typer", nil)
	}
}

// ---------------------------------------------------------------------------
// Python — argparse subparsers
// ---------------------------------------------------------------------------

// pyArgparseAddParserRe matches `<sub>.add_parser('name')` and captures the
// variable the returned sub-parser is assigned to plus the command name:
//
//	p_sync = subparsers.add_parser('sync')
//
// Group 1 = assigned variable (may be empty for an un-assigned add_parser),
// group 2 = command name.
var pyArgparseAddParserRe = regexp.MustCompile(`(?m)(?:(\w+)\s*=\s*)?\w+\.add_parser\s*\(\s*['"]([^'"]+)['"]`)

// pyArgparseSetDefaultsRe matches `<sub>.set_defaults(func=do_sync)` and binds
// the sub-parser variable to its handler. Group 1 = sub-parser variable,
// group 2 = handler function name.
var pyArgparseSetDefaultsRe = regexp.MustCompile(`(?m)(\w+)\.set_defaults\s*\([^)]*func\s*=\s*([\w.]+)`)

// synthesizePyArgparse joins argparse `add_parser('name')` declarations to their
// `set_defaults(func=handler)` handler binding via the shared sub-parser
// variable. Only commands whose handler is statically resolvable get an edge;
// the command node is still emitted for handler-less sub-parsers.
func synthesizePyArgparse(
	src, path string,
	emit func(cmdID, name, handler, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "add_parser") {
		return
	}
	// Map sub-parser variable → handler function (from set_defaults).
	varToHandler := map[string]string{}
	for _, m := range pyArgparseSetDefaultsRe.FindAllStringSubmatch(src, -1) {
		parserVar := m[1]
		handler := m[2]
		// Last dotted segment is the bare function name (mod.do_sync → do_sync).
		parts := strings.Split(handler, ".")
		varToHandler[parserVar] = parts[len(parts)-1]
	}
	for _, m := range pyArgparseAddParserRe.FindAllStringSubmatch(src, -1) {
		parserVar := m[1]
		name := m[2]
		handler := ""
		if parserVar != "" {
			handler = varToHandler[parserVar]
		}
		cmdID := "argparse:" + path + ":" + name
		emit(cmdID, name, handler, "argparse", nil)
	}
}

// ---------------------------------------------------------------------------
// Node — commander
// ---------------------------------------------------------------------------

// nodeCommanderRe matches `program.command('serve')....action(handler)` where
// the command(...) and a same-statement .action(handler) name a static handler
// identifier. The chain may carry intervening .description()/.option() calls, so
// the regex tolerates any non-action text up to the .action(. Group 1 = command
// name, group 2 = handler identifier.
var nodeCommanderRe = regexp.MustCompile(`\.command\s*\(\s*['"\x60]([^'"\x60\n]+?)['"\x60][^;]*?\.action\s*\(\s*([A-Za-z_$][\w$]*)\s*\)`)

// nodeCommanderNoActionRe matches `program.command('serve')` with no resolvable
// handler in the same chain (e.g. an inline arrow function or a deferred
// .action). Group 1 = command name. Used to still mint the command node.
var nodeCommanderNoActionRe = regexp.MustCompile(`\.command\s*\(\s*['"\x60]([^'"\x60\n]+?)['"\x60]`)

// synthesizeNodeCommander emits a SCOPE.Command per commander `.command('name')`
// with a HANDLES_COMMAND edge when a static `.action(handler)` is chained. Gated
// on a commander import so a `.command()` on an unrelated object is ignored. The
// command name's first token is used (commander allows `command('clone <src>')`
// with positional-arg syntax after the verb).
func synthesizeNodeCommander(
	src, path string,
	emit func(cmdID, name, handler, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "commander") {
		return
	}
	seenName := map[string]bool{}
	// First pass: command+action with a resolved handler.
	for _, m := range nodeCommanderRe.FindAllStringSubmatch(src, -1) {
		name := commandVerb(m[1])
		handler := m[2]
		if name == "" {
			continue
		}
		seenName[name] = true
		cmdID := "commander:" + path + ":" + name
		emit(cmdID, name, handler, "commander", nil)
	}
	// Second pass: command nodes with no resolvable handler.
	for _, m := range nodeCommanderNoActionRe.FindAllStringSubmatch(src, -1) {
		name := commandVerb(m[1])
		if name == "" || seenName[name] {
			continue
		}
		cmdID := "commander:" + path + ":" + name
		emit(cmdID, name, "", "commander", nil)
	}
}

// commandVerb returns the leading verb token of a CLI command spec, stripping
// any positional-argument syntax: `clone <source> [dest]` → `clone`.
func commandVerb(spec string) string {
	spec = strings.TrimSpace(spec)
	if i := strings.IndexAny(spec, " \t<["); i >= 0 {
		spec = spec[:i]
	}
	return strings.TrimSpace(spec)
}

// ---------------------------------------------------------------------------
// Node — yargs
// ---------------------------------------------------------------------------

// nodeYargsRe matches `.command('build', 'desc', builder, handler)` — the
// positional-args form where the 4th argument is a bare handler identifier.
// Group 1 = command name. The handler is resolved separately because yargs
// arglists vary (the builder arg is optional). We match the command name + the
// last bare identifier before the closing paren as the handler.
var nodeYargsRe = regexp.MustCompile(`\.command\s*\(\s*['"\x60]([^'"\x60\n]+?)['"\x60]\s*,[^)]*?,\s*([A-Za-z_$][\w$.]*)\s*\)`)

// nodeYargsObjRe matches the yargs object form
// `.command({ command: 'build', handler: buildHandler })`. Groups capture the
// command and handler in either order via two narrow sub-extractions below.
var nodeYargsObjCommandRe = regexp.MustCompile(`command\s*:\s*['"\x60]([^'"\x60\n]+?)['"\x60]`)
var nodeYargsObjHandlerRe = regexp.MustCompile(`handler\s*:\s*([A-Za-z_$][\w$.]*)`)

// synthesizeNodeYargs emits a SCOPE.Command per yargs `.command(...)`. Handles
// the positional-args form and the `{ command, handler }` object form. Gated on
// a yargs import. Inline arrow handlers (no bare identifier) yield a command
// node with no edge (honest-partial).
func synthesizeNodeYargs(
	src, path string,
	emit func(cmdID, name, handler, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "yargs") {
		return
	}
	seenName := map[string]bool{}
	// Positional-args form.
	for _, m := range nodeYargsRe.FindAllStringSubmatch(src, -1) {
		name := commandVerb(m[1])
		handler := lastDotted(m[2])
		if name == "" {
			continue
		}
		seenName[name] = true
		cmdID := "yargs:" + path + ":" + name
		emit(cmdID, name, handler, "yargs", nil)
	}
	// Object form: `.command({ command: 'x', handler: fn })`.
	for _, loc := range regexp.MustCompile(`\.command\s*\(\s*\{`).FindAllStringIndex(src, -1) {
		win := src[loc[0]:min(loc[0]+400, len(src))]
		cm := nodeYargsObjCommandRe.FindStringSubmatch(win)
		if len(cm) < 2 {
			continue
		}
		name := commandVerb(cm[1])
		if name == "" || seenName[name] {
			continue
		}
		handler := ""
		if hm := nodeYargsObjHandlerRe.FindStringSubmatch(win); len(hm) >= 2 {
			handler = lastDotted(hm[1])
		}
		seenName[name] = true
		cmdID := "yargs:" + path + ":" + name
		emit(cmdID, name, handler, "yargs", nil)
	}
}

// lastDotted returns the last dotted segment of an identifier (a.b.fn → fn).
func lastDotted(ident string) string {
	parts := strings.Split(ident, ".")
	return parts[len(parts)-1]
}

// ---------------------------------------------------------------------------
// Node — oclif
// ---------------------------------------------------------------------------

// nodeOclifClassRe matches an oclif command class:
// `export class Build extends Command {`. The command name is the lower-cased
// class name; oclif's runtime entry point is the class's `run()` method, so the
// handler is the synthetic `<Class>.run`. Group 1 = class name.
var nodeOclifClassRe = regexp.MustCompile(`(?m)class\s+(\w+)\s+extends\s+Command\b`)

// nodeOclifStaticIdRe captures an explicit `static id = 'build'` override that
// renames the command away from the class name. Group 1 = command id.
var nodeOclifStaticIdRe = regexp.MustCompile(`static\s+id\s*=\s*['"\x60]([^'"\x60\n]+)['"\x60]`)

// synthesizeNodeOclif emits a SCOPE.Command per oclif command class. Gated on an
// @oclif import so a generic `class X extends Command` (a different Command
// type) is not mis-attributed. The handler is the class's `run` method.
func synthesizeNodeOclif(
	src, path string,
	emit func(cmdID, name, handler, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "@oclif") {
		return
	}
	for _, m := range nodeOclifClassRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[m[2]:m[3]]
		name := strings.ToLower(className)
		// An explicit `static id = '...'` within the class body wins.
		win := src[m[0]:min(m[0]+600, len(src))]
		if im := nodeOclifStaticIdRe.FindStringSubmatch(win); len(im) >= 2 {
			name = commandVerb(im[1])
		}
		cmdID := "oclif:" + path + ":" + name
		emit(cmdID, name, "run", "oclif", map[string]string{
			"command_class": className,
		})
	}
}

// ---------------------------------------------------------------------------
// Go — cobra
// ---------------------------------------------------------------------------

// goCobraCommandRe matches a cobra command struct literal:
//
//	&cobra.Command{Use: "serve", Run: serveFn}
//
// The struct fields can appear in any order across multiple lines, so this
// matches the `cobra.Command{` opener and the Use/Run fields are extracted from
// the literal body separately. Group 0 = the literal start offset.
var goCobraCommandRe = regexp.MustCompile(`cobra\.Command\s*\{`)

// goCobraUseRe captures `Use: "serve"` or `Use: "serve [flags]"`. Group 1 = use spec.
var goCobraUseRe = regexp.MustCompile(`Use\s*:\s*"([^"\n]+)"`)

// goCobraRunRe captures `Run: serveFn` / `RunE: serveFn` where the handler is a
// bare function reference (not an inline func literal). Group 1 = handler name.
var goCobraRunRe = regexp.MustCompile(`Run[E]?\s*:\s*([A-Za-z_]\w*)\b`)

// synthesizeGoCobra emits a SCOPE.Command per `&cobra.Command{Use:...,Run:...}`
// literal. The Use field's leading token is the command name; Run/RunE names the
// handler when it is a bare function reference. Inline `Run: func(...) {...}`
// literals yield a command node with no edge (the handler regex requires an
// identifier, and `func` is filtered).
func synthesizeGoCobra(
	src, path string,
	emit func(cmdID, name, handler, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "cobra") {
		return
	}
	for _, loc := range goCobraCommandRe.FindAllStringIndex(src, -1) {
		// Scan the struct-literal body. Bound the window to the matching brace
		// depth so a later command's fields don't bleed into this one.
		body := goCobraLiteralBody(src, loc[1])
		um := goCobraUseRe.FindStringSubmatch(body)
		if len(um) < 2 {
			continue // no Use field — not a named command (root cmd etc.)
		}
		name := commandVerb(um[1])
		if name == "" {
			continue
		}
		handler := ""
		if rm := goCobraRunRe.FindStringSubmatch(body); len(rm) >= 2 {
			if rm[1] != "func" {
				handler = rm[1]
			}
		}
		cmdID := "cobra:" + path + ":" + name
		emit(cmdID, name, handler, "cobra", nil)
	}
}

// goCobraLiteralBody returns the source slice of a struct-literal body starting
// just after the opening brace at `open`, ending at the matching closing brace
// (brace-depth balanced). Bounds protect against unbalanced input.
func goCobraLiteralBody(src string, open int) string {
	depth := 1
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open:i]
			}
		}
	}
	return src[open:min(open+800, len(src))]
}

// ---------------------------------------------------------------------------
// Java / Kotlin — picocli
// ---------------------------------------------------------------------------

// javaPicocliCommandRe matches a picocli `@Command(name = "serve")` annotation.
// Group 1 = command name. The handler is the implementing class's run()/call()
// method (picocli invokes Runnable.run or Callable.call), so we resolve the
// class declared after the annotation and use its run/call as the handler.
var javaPicocliCommandRe = regexp.MustCompile(`@Command\s*\(\s*[^)]*name\s*=\s*"([^"\n]+)"`)

// javaPicocliClassRe finds the `class <Name> ...` declaration following an
// annotation offset. Group 1 = class name.
var javaPicocliClassRe = regexp.MustCompile(`(?m)\bclass\s+(\w+)`)

// synthesizeJavaPicocli emits a SCOPE.Command per picocli @Command(name=...)
// annotated class. Gated on a picocli import. The handler is the class's
// run/call method (whichever it declares); we pick `call` if a `call(` method is
// present, else `run`.
func synthesizeJavaPicocli(
	src, path string,
	emit func(cmdID, name, handler, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "picocli") {
		return
	}
	for _, m := range javaPicocliCommandRe.FindAllStringSubmatchIndex(src, -1) {
		name := commandVerb(src[m[2]:m[3]])
		if name == "" {
			continue
		}
		// Find the class declared after this annotation.
		rest := src[m[1]:]
		className := ""
		if cm := javaPicocliClassRe.FindStringSubmatch(rest); len(cm) >= 2 {
			className = cm[1]
		}
		handler := "run"
		// picocli Callable<Integer> commands implement call(); prefer it when present.
		if strings.Contains(src, "implements Callable") || strings.Contains(src, "Integer call(") ||
			regexp.MustCompile(`\bInteger\s+call\s*\(`).MatchString(src) {
			handler = "call"
		}
		cmdID := "picocli:" + path + ":" + name
		emit(cmdID, name, handler, "picocli", map[string]string{
			"command_class": className,
		})
	}
}

// ---------------------------------------------------------------------------
// Java — Spring Shell
// ---------------------------------------------------------------------------

// javaSpringShellRe matches a Spring Shell `@ShellMethod(...)` annotation; the
// handler is the method declared immediately below it. An optional `key = "..."`
// attribute names the command, else the method name is the command.
var javaSpringShellRe = regexp.MustCompile(`@ShellMethod\b`)

// javaSpringShellKeyRe captures an explicit `key = "build"` / `value = "build"`
// command key. Group 1 = command key.
var javaSpringShellKeyRe = regexp.MustCompile(`(?:key|value)\s*=\s*"([^"\n]+)"`)

// synthesizeJavaSpringShell emits a SCOPE.Command per @ShellMethod annotated
// method. Gated on a shell import. The handler is the annotated method itself;
// the command name is the explicit key (first token) or the method name.
func synthesizeJavaSpringShell(
	src, path string,
	emit func(cmdID, name, handler, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "ShellMethod") {
		return
	}
	for _, loc := range javaSpringShellRe.FindAllStringIndex(src, -1) {
		method := findFollowingMethod(src, loc[0])
		if method == "" {
			continue
		}
		name := method
		// An explicit key/value on the annotation line wins.
		annLine := src[loc[0]:min(loc[0]+200, len(src))]
		if km := javaSpringShellKeyRe.FindStringSubmatch(annLine); len(km) >= 2 {
			name = commandVerb(km[1])
		}
		if name == "" {
			continue
		}
		cmdID := "spring_shell:" + path + ":" + name
		emit(cmdID, name, method, "spring_shell", nil)
	}
}

// ---------------------------------------------------------------------------
// Ruby — Thor
// ---------------------------------------------------------------------------

// rubyThorDescRe matches a Thor `desc 'build [opts]', 'description'` declaration
// followed by the `def <name>` it documents. Thor's command name is the method
// name; the desc's first token is the usage verb (which should match). We bind
// the command to the def that follows the desc. Group 1 = method name.
var rubyThorDescRe = regexp.MustCompile(`(?m)^\s*desc\s+['"][^'"]+['"]\s*,[^\n]*\n(?:\s*(?:method_option|option|long_desc)[^\n]*\n)*\s*def\s+([A-Za-z_]\w*)`)

// synthesizeRubyThor emits a SCOPE.Command per Thor `desc '...'` + `def name`
// pair. Gated on a Thor marker (`< Thor` superclass or `require "thor"`). The
// handler IS the def, so the command name and handler are the method name.
func synthesizeRubyThor(
	src, path string,
	emit func(cmdID, name, handler, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "Thor") && !strings.Contains(src, "thor") {
		return
	}
	for _, m := range rubyThorDescRe.FindAllStringSubmatch(src, -1) {
		method := m[1]
		name := method
		cmdID := "thor:" + path + ":" + name
		emit(cmdID, name, method, "thor", nil)
	}
}

// ---------------------------------------------------------------------------
// Ruby — Rake
// ---------------------------------------------------------------------------

// rubyRakeTaskRe matches `task :build do` / `task :build => [:deps] do` /
// `task "build" do`. The task name is the command; the do...end block body is
// the handler. Rake tasks have no named handler function, so the handler is the
// synthetic `<task>_task` (the block). Group 1 = task name.
var rubyRakeTaskRe = regexp.MustCompile(`(?m)^\s*task\s+[:'"]([A-Za-z_][\w:]*)['"]?\s*(?:=>|do\b|,)`)

// synthesizeRubyRake emits a SCOPE.Command per Rake `task :name do` block. Gated
// on a Rake marker. Rake task blocks are anonymous, so no HANDLES_COMMAND edge is
// emitted (honest-partial) — the command node records the task name + that it is
// a rake target.
func synthesizeRubyRake(
	src, path string,
	emit func(cmdID, name, handler, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "Rake") && !strings.Contains(src, "task ") &&
		!strings.HasSuffix(path, ".rake") && !strings.HasSuffix(path, "Rakefile") {
		return
	}
	// Require a rake context to avoid matching an unrelated `task` method.
	if !strings.Contains(src, "Rake") && !strings.HasSuffix(path, ".rake") &&
		!strings.HasSuffix(path, "Rakefile") {
		return
	}
	for _, m := range rubyRakeTaskRe.FindAllStringSubmatch(src, -1) {
		name := m[1]
		if name == "" {
			continue
		}
		cmdID := "rake:" + path + ":" + name
		// Rake blocks are anonymous — no resolvable handler fn.
		emit(cmdID, name, "", "rake", nil)
	}
}
