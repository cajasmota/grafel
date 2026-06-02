// Tests for the CLI command entry-point detection pass (epic #3628).
//
// Each framework has a value-asserting happy-path test that checks the exact
// SCOPE.Command entity ID AND the HANDLES_COMMAND edge to the handler function
// (not just len>0). Negative tests cover dynamic command names and non-CLI
// `.command()` / `.action()` call sites on unrelated objects.
//
// Tests call applyCLICommandEdges directly (same pattern as
// scheduled_jobs_edges_test.go) so they run without the full YAML-rule compiler.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// runCLIDetect is a lightweight in-process driver.
func runCLIDetect(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	res := applyCLICommandEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

// hasCommand asserts a SCOPE.Command entity with Name==cmdID exists.
func hasCommand(ents []types.EntityRecord, cmdID string) bool {
	for _, e := range ents {
		if e.Kind == cliCommandKind && e.Name == cmdID {
			return true
		}
	}
	return false
}

// hasHandlesCommandEdge asserts a HANDLES_COMMAND edge
// SCOPE.Command:<cmdID> -> Function:<handler> exists.
func hasHandlesCommandEdge(rels []types.RelationshipRecord, cmdID, handler string) bool {
	wantFrom := cliCommandKind + ":" + cmdID
	wantTo := "Function:" + handler
	for _, r := range rels {
		if r.Kind == handlesCommandEdgeKind && r.FromID == wantFrom && r.ToID == wantTo {
			return true
		}
	}
	return false
}

// countHandlesCommandEdges returns the number of HANDLES_COMMAND edges.
func countHandlesCommandEdges(rels []types.RelationshipRecord) int {
	n := 0
	for _, r := range rels {
		if r.Kind == handlesCommandEdgeKind {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Python — click
// ---------------------------------------------------------------------------

func TestCLI_PyClick_ExplicitName(t *testing.T) {
	src := `import click

@click.group()
def cli():
    pass

@cli.command('migrate')
def migrate():
    run_migrations()
`
	ents, rels := runCLIDetect(t, "python", "manage.py", src)
	cmdID := "click:manage.py:migrate"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "migrate") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:migrate, rels=%v", cmdID, rels)
	}
}

func TestCLI_PyClick_DefaultNameFromFunc(t *testing.T) {
	src := `import click

@click.command()
def sync_data():
    pass
`
	ents, rels := runCLIDetect(t, "python", "cli.py", src)
	// click derives the command name from the function: sync_data -> sync-data.
	cmdID := "click:cli.py:sync-data"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "sync_data") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:sync_data, rels=%v", cmdID, rels)
	}
}

// ---------------------------------------------------------------------------
// Python — argparse
// ---------------------------------------------------------------------------

func TestCLI_PyArgparse_AddParserSetDefaults(t *testing.T) {
	src := `import argparse

def do_sync(args):
    pass

parser = argparse.ArgumentParser()
subparsers = parser.add_subparsers()
p_sync = subparsers.add_parser('sync')
p_sync.set_defaults(func=do_sync)
`
	ents, rels := runCLIDetect(t, "python", "tool.py", src)
	cmdID := "argparse:tool.py:sync"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "do_sync") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:do_sync, rels=%v", cmdID, rels)
	}
}

// ---------------------------------------------------------------------------
// Python — typer
// ---------------------------------------------------------------------------

func TestCLI_PyTyper_Command(t *testing.T) {
	src := `import typer

app = typer.Typer()

@app.command()
def deploy():
    pass
`
	ents, rels := runCLIDetect(t, "python", "main.py", src)
	cmdID := "typer:main.py:deploy"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "deploy") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:deploy, rels=%v", cmdID, rels)
	}
}

// ---------------------------------------------------------------------------
// Node — commander
// ---------------------------------------------------------------------------

func TestCLI_NodeCommander_CommandAction(t *testing.T) {
	src := `const { program } = require('commander');

program
  .command('build')
  .description('build the project')
  .action(buildHandler);
`
	ents, rels := runCLIDetect(t, "javascript", "cli.js", src)
	cmdID := "commander:cli.js:build"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "buildHandler") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:buildHandler, rels=%v", cmdID, rels)
	}
}

func TestCLI_NodeCommander_PositionalArgsStripped(t *testing.T) {
	src := `const { program } = require('commander');
program.command('clone <source> [dest]').action(cloneHandler);
`
	ents, rels := runCLIDetect(t, "javascript", "cli.js", src)
	cmdID := "commander:cli.js:clone"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q (verb only), entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "cloneHandler") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:cloneHandler, rels=%v", cmdID, rels)
	}
}

// ---------------------------------------------------------------------------
// Node — yargs
// ---------------------------------------------------------------------------

func TestCLI_NodeYargs_PositionalForm(t *testing.T) {
	src := `const yargs = require('yargs');
yargs.command('serve', 'start the server', builder, serveHandler).argv;
`
	ents, rels := runCLIDetect(t, "javascript", "bin.js", src)
	cmdID := "yargs:bin.js:serve"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "serveHandler") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:serveHandler, rels=%v", cmdID, rels)
	}
}

func TestCLI_NodeYargs_ObjectForm(t *testing.T) {
	src := `const yargs = require('yargs');
yargs.command({ command: 'lint', describe: 'lint files', handler: lintHandler });
`
	ents, rels := runCLIDetect(t, "javascript", "bin.js", src)
	cmdID := "yargs:bin.js:lint"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "lintHandler") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:lintHandler, rels=%v", cmdID, rels)
	}
}

// ---------------------------------------------------------------------------
// Node — oclif
// ---------------------------------------------------------------------------

func TestCLI_NodeOclif_CommandClass(t *testing.T) {
	src := `import { Command } from '@oclif/core';

export default class Build extends Command {
  async run() {
    // ...
  }
}
`
	ents, rels := runCLIDetect(t, "typescript", "commands/build.ts", src)
	cmdID := "oclif:commands/build.ts:build"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "run") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:run, rels=%v", cmdID, rels)
	}
}

// ---------------------------------------------------------------------------
// Go — cobra
// ---------------------------------------------------------------------------

func TestCLI_GoCobra_UseRun(t *testing.T) {
	src := `package cmd

import "github.com/spf13/cobra"

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the server",
	Run:   serve,
}
`
	ents, rels := runCLIDetect(t, "go", "cmd/serve.go", src)
	cmdID := "cobra:cmd/serve.go:serve"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "serve") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:serve, rels=%v", cmdID, rels)
	}
}

func TestCLI_GoCobra_RunE(t *testing.T) {
	src := `package cmd
import "github.com/spf13/cobra"
var migrateCmd = &cobra.Command{Use: "migrate [flags]", RunE: runMigrate}
`
	ents, rels := runCLIDetect(t, "go", "cmd/migrate.go", src)
	cmdID := "cobra:cmd/migrate.go:migrate"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q (verb only), entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "runMigrate") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:runMigrate, rels=%v", cmdID, rels)
	}
}

// ---------------------------------------------------------------------------
// Java — picocli
// ---------------------------------------------------------------------------

func TestCLI_JavaPicocli_Command(t *testing.T) {
	src := `import picocli.CommandLine.Command;

@Command(name = "serve", description = "Start the server")
public class ServeCommand implements Runnable {
    public void run() {
        // ...
    }
}
`
	ents, rels := runCLIDetect(t, "java", "ServeCommand.java", src)
	cmdID := "picocli:ServeCommand.java:serve"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "run") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:run, rels=%v", cmdID, rels)
	}
}

// ---------------------------------------------------------------------------
// Java — Spring Shell
// ---------------------------------------------------------------------------

func TestCLI_JavaSpringShell_ShellMethod(t *testing.T) {
	src := `import org.springframework.shell.standard.ShellMethod;

@ShellComponent
public class Commands {
    @ShellMethod(key = "translate", value = "Translate text")
    public String translate(String text) {
        return doTranslate(text);
    }
}
`
	ents, rels := runCLIDetect(t, "java", "Commands.java", src)
	cmdID := "spring_shell:Commands.java:translate"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "translate") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:translate, rels=%v", cmdID, rels)
	}
}

// ---------------------------------------------------------------------------
// Ruby — Thor
// ---------------------------------------------------------------------------

func TestCLI_RubyThor_DescDef(t *testing.T) {
	src := `require "thor"

class CLI < Thor
  desc "build", "Build the project"
  def build
    do_build
  end
end
`
	ents, rels := runCLIDetect(t, "ruby", "cli.rb", src)
	cmdID := "thor:cli.rb:build"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	if !hasHandlesCommandEdge(rels, cmdID, "build") {
		t.Fatalf("expected HANDLES_COMMAND %s -> Function:build, rels=%v", cmdID, rels)
	}
}

// ---------------------------------------------------------------------------
// Ruby — Rake
// ---------------------------------------------------------------------------

func TestCLI_RubyRake_Task(t *testing.T) {
	src := `require 'rake'

task :build do
  sh "make"
end
`
	ents, _ := runCLIDetect(t, "ruby", "Rakefile", src)
	cmdID := "rake:Rakefile:build"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	// Rake blocks are anonymous: no HANDLES_COMMAND edge (honest-partial).
}

// ---------------------------------------------------------------------------
// Negative — dynamic command name yields no command
// ---------------------------------------------------------------------------

func TestCLI_NodeCommander_DynamicNameNoEdge(t *testing.T) {
	src := `const { program } = require('commander');
program.command(nameVar).action(handler);
`
	ents, rels := runCLIDetect(t, "javascript", "cli.js", src)
	for _, e := range ents {
		if e.Kind == cliCommandKind {
			t.Fatalf("expected NO SCOPE.Command for dynamic name, got %q", e.Name)
		}
	}
	if n := countHandlesCommandEdges(rels); n != 0 {
		t.Fatalf("expected 0 HANDLES_COMMAND edges for dynamic name, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Negative — non-CLI .command()/.action() on an unrelated object
// ---------------------------------------------------------------------------

func TestCLI_NodeNonCLI_DBCommandNoEdge(t *testing.T) {
	// No commander/yargs/oclif import: a DB driver's `.command()` must NOT
	// be mistaken for a CLI command.
	src := `const db = require('mongodb');
db.command({ ping: 1 }).action(onResult);
`
	ents, rels := runCLIDetect(t, "javascript", "db.js", src)
	for _, e := range ents {
		if e.Kind == cliCommandKind {
			t.Fatalf("expected NO SCOPE.Command for non-CLI .command(), got %q", e.Name)
		}
	}
	if n := countHandlesCommandEdges(rels); n != 0 {
		t.Fatalf("expected 0 HANDLES_COMMAND edges for non-CLI object, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Negative — Go cobra inline func literal handler yields command, no fn edge
// ---------------------------------------------------------------------------

func TestCLI_GoCobra_InlineFuncNoHandlerEdge(t *testing.T) {
	src := `package cmd
import "github.com/spf13/cobra"
var rootCmd = &cobra.Command{
	Use: "gen",
	Run: func(cmd *cobra.Command, args []string) {
		generate()
	},
}
`
	ents, rels := runCLIDetect(t, "go", "cmd/gen.go", src)
	cmdID := "cobra:cmd/gen.go:gen"
	if !hasCommand(ents, cmdID) {
		t.Fatalf("expected SCOPE.Command %q, entities=%v", cmdID, ents)
	}
	// Inline func literal is not a bare handler reference: no HANDLES_COMMAND edge.
	if n := countHandlesCommandEdges(rels); n != 0 {
		t.Fatalf("expected 0 HANDLES_COMMAND edges for inline func, got %d", n)
	}
}
