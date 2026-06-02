<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `cli.command-extraction` — CLI command entry-points (click/argparse/typer, commander/yargs/oclif, cobra, picocli/Spring Shell, Thor/Rake)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [protocol](../by-category/protocol.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Cross repo linkage | — `not_applicable` | — | — | — | CLI commands are a single-process entry-point (binary invoked by a user/shell), not a cross-repo wire protocol — there is no remote producer/consumer pair to join, so cross-repo linkage does not apply (mirrors the scheduled-job entry-point model in scheduled_jobs_edges.go). |
| Method attribution | ✅ `full` | `2026-06-02` | — | `internal/engine/cli_command_edges.go`<br>`internal/engine/cli_command_edges_test.go` | Epic #3628: each SCOPE.Command joins its handler via a HANDLES_COMMAND edge (SCOPE.Command:<id> -> Function:<handler>) — the CLI analog of an HTTP endpoint's HANDLES edge. Handler resolution: click/typer -> decorated def; argparse -> set_defaults(func=do_sync) bound via the shared sub-parser variable; commander -> .action(handler); yargs -> 4th positional arg / handler: key; oclif -> the class run() method; cobra -> Run/RunE bare fn ref; picocli -> the class call()/run(); Spring Shell -> the annotated method; Thor -> the def itself. Honest-partial (command node, NO edge): cobra inline Run:func(...){} literals, Rake anonymous task blocks, and yargs/commander inline arrow handlers — asserted by value-asserting negative tests (TestCLI_GoCobra_InlineFuncNoHandlerEdge, TestCLI_RubyRake_Task) that the handler edge count is 0. |
| Service extraction | ✅ `full` | `2026-06-02` | — | `internal/engine/cli_command_edges.go`<br>`internal/engine/cli_command_edges_test.go`<br>`internal/types/kinds.go` | Epic #3628: command-line command entry-point detection — the CLI sibling of an HTTP endpoint. New LIVE engine pass applyCLICommandEdges (registered in detector.go after applyScheduledJobEdges) mints a SCOPE.Command node per statically-declared command, keyed `<framework>:<path>:<name>` (props: command, handler, framework). Frameworks: click (@cli.command('migrate') / @click.command() default-name func->dashes), typer (@app.command()), argparse (subparsers.add_parser('sync')) [Python]; commander (program.command('serve')...action(h)), yargs (positional .command('build',desc,builder,h) + object {command,handler} forms), oclif (class X extends Command) [Node]; cobra (&cobra.Command{Use:"serve",Run:serve}) [Go]; picocli (@Command(name="serve") class), Spring Shell (@ShellMethod(key="...")) [Java]; Thor (desc '...' + def name), Rake (task :build do) [Ruby]. Positional-arg syntax stripped to the leading verb (command('clone <src>') -> clone; Use:"migrate [flags]" -> migrate). Honest-partial: dynamic command names (command(nameVar)) emit nothing; every detector is gated behind a framework-import pre-filter so a non-CLI .command()/.action() on an unrelated object (e.g. a DB driver) mints nothing. Value-asserting tests assert the exact SCOPE.Command node ID per framework. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update cli.command-extraction ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
