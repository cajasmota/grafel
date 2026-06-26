package treesitter_test

// Real-file, time-bounded parse canary (issue #5659, Phase 0 of the 0.1.7
// ABI-15 effort; refs #5473 / Wave A #5654).
//
// WHY THIS EXISTS
//
// The existing TestSupportedLanguages_EachLanguageParsesSnippet only proves each
// grammar LOADS and parses a TRIVIAL one-liner. That is exactly the blind spot
// the Wave A runtime bump (go-tree-sitter v0.24 -> v0.25) fell into: the runtime
// moved to ABI-15 while the grammars stayed at ABI-14, which is still inside the
// supported window, so SetLanguage did not error and the trivial abiGuard
// snippet parsed fine — CI stayed green — yet a REAL file drove the parser into
// an unbounded error-recovery loop (26 min pinned at the parse cap; ts_parser_parse
// / ts_node_child hot). ADR-0023 §6 names this class: "compiles + loads + trivial
// input works, real input crashes/loops".
//
// WHAT THIS CATCHES
//
//   1. TestParseCanary_RealFiles_TimeBounded — per language, parse a NON-trivial
//      representative sample under a per-file wall-clock bound. A grammar/runtime
//      mismatch that error-loops blows the time bound (the core catch); a
//      recovery-storm blows the ERROR-node ratio (the tree is dominated by
//      recovery nodes). Either path fails loudly.
//
//   2. TestParseCanary_CorpusSettles_TimeBounded — parse the WHOLE multi-language
//      corpus through one factory under a single aggregate bound, folding results
//      into the existing ParseErrorCanary. This is a CI-safe proxy for a
//      daemon-level parse loop: a full daemon is too heavy for CI, but every real
//      parse funnels through the same in-process chokepoint a runaway daemon
//      parse would hold (parseMu #481 + the parse slot #5630), so a corpus that
//      never settles is the same signal as a daemon that never settles.
//
// BOUNDS / MARGIN (deterministic, generous-but-finite)
//
// A healthy parse of any sample below is sub-millisecond to low-single-digit
// milliseconds even on a loaded machine. perFileBound is 10s (>1000x headroom)
// and corpusBound is 90s for the full ~28-file corpus. Both are far below the
// minutes-long hang a real loop produces, so the bound is finite enough to trip
// a non-terminating parse yet generous enough not to flake on slow 3-OS CI
// runners. We assert on wall-clock COMPLETION rather than on parse latency, so
// ordinary CI slowness never fails the gate — only an actual loop does.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/treesitter"
)

const (
	// perFileBound is the wall-clock budget for a single real-file parse.
	// Healthy parses finish in <~5ms; 10s is >1000x headroom. A real
	// error-recovery loop runs for minutes, so this trips it without flaking
	// on slow CI.
	perFileBound = 10 * time.Second

	// corpusBound is the wall-clock budget for parsing the entire corpus
	// sequentially through one factory. ~28 files * (parse + node walk),
	// serialised on parseMu; 90s is comfortable on the slowest CI runner yet
	// well under the minutes-long hang a daemon-level loop produces.
	corpusBound = 90 * time.Second

	// minSaneNodes is the floor a real sample's tree must clear to count as a
	// genuine, non-trivial parse (proves the root is reachable and the grammar
	// actually walked the file, not a near-empty bail-out tree).
	minSaneNodes = 30

	// maxDominatedRatio is the ERROR-node fraction above which we consider the
	// tree "dominated by recovery" — the symptom of a grammar that no longer
	// understands the input. Valid idiomatic samples sit at ~0; a recovery
	// storm approaches ~1.0. 0.50 means "more than half the tree is errors",
	// which idiomatic code never reaches, so this is not flaky.
	maxDominatedRatio = 0.50
)

// realSamples maps each supported language to a NON-trivial representative
// source file: functions/classes, comments, strings, and nested structures —
// deliberately not a one-liner, so a grammar that only copes with trivial input
// is exposed. Keep these idiomatic and within the currently-pinned grammars'
// syntax (no bleeding-edge constructs) so the canary stays a regression net, not
// a feature gate. "terraform" reuses the hcl sample (it is an alias).
var realSamples = map[string]string{
	"bash": `#!/usr/bin/env bash
# greet iterates over arguments and prints a banner.
set -euo pipefail

greet() {
  local name="$1"
  if [[ -z "${name}" ]]; then
    echo "usage: greet <name>" >&2
    return 1
  fi
  for i in 1 2 3; do
    printf 'hello, %s (%d)\n' "${name}" "${i}"
  done
}

main() {
  greet "${1:-world}"
}

main "$@"
`,
	"c": `#include <stdio.h>
#include <stdlib.h>

/* A tiny stack of ints. */
typedef struct {
  int *data;
  size_t len;
  size_t cap;
} Stack;

static int stack_push(Stack *s, int v) {
  if (s->len == s->cap) {
    s->cap = s->cap ? s->cap * 2 : 4;
    s->data = realloc(s->data, s->cap * sizeof(int));
    if (!s->data) return -1;
  }
  s->data[s->len++] = v;
  return 0;
}

int main(void) {
  Stack s = {0};
  for (int i = 0; i < 5; i++) {
    stack_push(&s, i * i);
  }
  printf("top = %d\n", s.data[s.len - 1]);
  free(s.data);
  return 0;
}
`,
	"cpp": `#include <iostream>
#include <vector>
#include <string>

namespace geo {

// Point is a 2D coordinate.
class Point {
 public:
  Point(double x, double y) : x_(x), y_(y) {}
  double norm() const { return x_ * x_ + y_ * y_; }

 private:
  double x_, y_;
};

template <typename T>
T sum(const std::vector<T> &xs) {
  T acc{};
  for (const auto &v : xs) acc += v;
  return acc;
}

}  // namespace geo

int main() {
  std::vector<int> xs{1, 2, 3, 4};
  std::cout << "sum = " << geo::sum(xs) << "\n";
  geo::Point p(3.0, 4.0);
  std::cout << "norm = " << p.norm() << "\n";
  return 0;
}
`,
	"css": `/* Card component styles. */
:root {
  --gap: 16px;
  --fg: #1a1a1a;
}

.card {
  display: flex;
  flex-direction: column;
  gap: var(--gap);
  padding: 1rem 1.5rem;
  color: var(--fg);
}

.card__title {
  font-size: 1.25rem;
  font-weight: 600;
}

@media (max-width: 600px) {
  .card {
    padding: 0.5rem;
  }
  .card__title::after {
    content: "\2026";
  }
}
`,
	"csharp": `using System;
using System.Collections.Generic;
using System.Linq;

namespace Demo
{
    // A repository over an in-memory list.
    public class Repository<T>
    {
        private readonly List<T> _items = new();

        public void Add(T item) => _items.Add(item);

        public IEnumerable<T> Where(Func<T, bool> pred)
        {
            foreach (var x in _items)
            {
                if (pred(x)) yield return x;
            }
        }
    }

    public static class Program
    {
        public static void Main()
        {
            var repo = new Repository<int>();
            foreach (var i in Enumerable.Range(0, 10)) repo.Add(i);
            var evens = repo.Where(x => x % 2 == 0).ToList();
            Console.WriteLine($"evens: {string.Join(", ", evens)}");
        }
    }
}
`,
	"dockerfile": `# Multi-stage build for a Go service.
FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/app ./cmd/app

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app"]
`,
	"elixir": `defmodule Demo.Account do
  @moduledoc "A toy account with a running balance."

  defstruct id: nil, balance: 0

  @doc "Deposit a positive amount, returning the updated account."
  def deposit(%__MODULE__{balance: b} = acct, amount) when amount > 0 do
    %{acct | balance: b + amount}
  end

  def history(transactions) do
    transactions
    |> Enum.filter(fn t -> t.amount != 0 end)
    |> Enum.map(& &1.amount)
    |> Enum.sum()
  end
end
`,
	"go": `package demo

import (
	"fmt"
	"sort"
)

// Account holds a running balance for a named owner.
type Account struct {
	Owner   string
	Balance int
}

// Deposit adds amount and returns the new balance.
func (a *Account) Deposit(amount int) (int, error) {
	if amount <= 0 {
		return a.Balance, fmt.Errorf("non-positive amount: %d", amount)
	}
	a.Balance += amount
	return a.Balance, nil
}

func TopOwners(accounts []*Account) []string {
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].Balance > accounts[j].Balance
	})
	owners := make([]string, 0, len(accounts))
	for _, a := range accounts {
		owners = append(owners, a.Owner)
	}
	return owners
}
`,
	"groovy": `package demo

// A small builder-style class.
class Greeter {
    String prefix = "Hello"

    String greet(String name) {
        return "${prefix}, ${name}!"
    }

    List<String> greetAll(List<String> names) {
        names.collect { greet(it) }
    }
}

def g = new Greeter(prefix: "Hi")
[ "ada", "linus" ].each { n ->
    println g.greet(n)
}
`,
	"hcl": `# An S3 bucket with versioning enabled.
variable "bucket_name" {
  type        = string
  description = "Name of the bucket"
}

resource "aws_s3_bucket" "assets" {
  bucket = var.bucket_name

  tags = {
    Environment = "prod"
    ManagedBy   = "terraform"
  }
}

resource "aws_s3_bucket_versioning" "assets" {
  bucket = aws_s3_bucket.assets.id
  versioning_configuration {
    status = "Enabled"
  }
}

output "bucket_arn" {
  value = aws_s3_bucket.assets.arn
}
`,
	"html": `<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>Demo</title>
    <style>
      body { font-family: sans-serif; }
    </style>
  </head>
  <body>
    <!-- main content -->
    <header><h1>Welcome</h1></header>
    <main>
      <ul class="items">
        <li data-id="1">First</li>
        <li data-id="2">Second</li>
      </ul>
    </main>
    <script>console.log("ready");</script>
  </body>
</html>
`,
	"java": `package demo;

import java.util.List;
import java.util.stream.Collectors;

/** A simple immutable-ish account. */
public class Account {
    private final String owner;
    private int balance;

    public Account(String owner) {
        this.owner = owner;
        this.balance = 0;
    }

    public int deposit(int amount) {
        if (amount <= 0) {
            throw new IllegalArgumentException("non-positive: " + amount);
        }
        this.balance += amount;
        return this.balance;
    }

    public static List<String> owners(List<Account> accounts) {
        return accounts.stream()
                .map(a -> a.owner)
                .collect(Collectors.toList());
    }
}
`,
	"javascript": `// A small event emitter.
class Emitter {
  constructor() {
    this.handlers = new Map();
  }

  on(event, fn) {
    const list = this.handlers.get(event) || [];
    list.push(fn);
    this.handlers.set(event, list);
    return this;
  }

  emit(event, ...args) {
    for (const fn of this.handlers.get(event) || []) {
      fn(...args);
    }
  }
}

const e = new Emitter();
e.on("tick", (n) => console.log(` + "`tick ${n}`" + `));
[1, 2, 3].forEach((n) => e.emit("tick", n));

export default Emitter;
`,
	"kotlin": `package demo

// A sealed result type with two cases.
sealed class Result<out T> {
    data class Ok<T>(val value: T) : Result<T>()
    data class Err(val message: String) : Result<Nothing>()
}

fun parseInt(s: String): Result<Int> {
    val n = s.toIntOrNull() ?: return Result.Err("not an int: $s")
    return Result.Ok(n)
}

fun main() {
    val inputs = listOf("1", "two", "3")
    for (s in inputs) {
        when (val r = parseInt(s)) {
            is Result.Ok -> println("ok ${r.value}")
            is Result.Err -> println("err ${r.message}")
        }
    }
}
`,
	"lua": `-- A small queue implemented with a table.
local Queue = {}
Queue.__index = Queue

function Queue.new()
  return setmetatable({ items = {}, head = 1 }, Queue)
end

function Queue:push(v)
  self.items[#self.items + 1] = v
end

function Queue:pop()
  if self.head > #self.items then
    return nil
  end
  local v = self.items[self.head]
  self.head = self.head + 1
  return v
end

local q = Queue.new()
for i = 1, 3 do
  q:push(i * 10)
end
print(q:pop(), q:pop())
`,
	"ocaml": `(* A small binary tree with an in-order fold. *)
type 'a tree =
  | Leaf
  | Node of 'a tree * 'a * 'a tree

let rec insert t x =
  match t with
  | Leaf -> Node (Leaf, x, Leaf)
  | Node (l, v, r) ->
    if x < v then Node (insert l x, v, r)
    else Node (l, v, insert r x)

let rec fold f acc t =
  match t with
  | Leaf -> acc
  | Node (l, v, r) -> fold f (f (fold f acc l) v) r

let () =
  let t = List.fold_left insert Leaf [ 5; 3; 8; 1 ] in
  let sum = fold (fun a v -> a + v) 0 t in
  Printf.printf "sum = %d\n" sum
`,
	"php": `<?php

namespace Demo;

// A simple value object.
final class Money
{
    public function __construct(
        private int $cents,
        private string $currency = "USD"
    ) {}

    public function add(Money $other): Money
    {
        return new Money($this->cents + $other->cents, $this->currency);
    }

    public function format(): string
    {
        return sprintf("%.2f %s", $this->cents / 100, $this->currency);
    }
}

$total = (new Money(150))->add(new Money(350));
echo $total->format() . PHP_EOL;
`,
	"proto": `syntax = "proto3";

package demo.v1;

option go_package = "github.com/example/demo/v1;demov1";

// A user account.
message Account {
  string id = 1;
  string owner = 2;
  int64 balance_cents = 3;
  repeated string tags = 4;
}

message GetAccountRequest {
  string id = 1;
}

service Accounts {
  // Fetch a single account by id.
  rpc GetAccount(GetAccountRequest) returns (Account);
}
`,
	"python": `"""A tiny ledger module."""
from dataclasses import dataclass, field
from typing import List


@dataclass
class Account:
    owner: str
    balance: int = 0
    history: List[int] = field(default_factory=list)

    def deposit(self, amount: int) -> int:
        if amount <= 0:
            raise ValueError(f"non-positive amount: {amount}")
        self.balance += amount
        self.history.append(amount)
        return self.balance


def top_owners(accounts: List[Account]) -> List[str]:
    return [a.owner for a in sorted(accounts, key=lambda a: -a.balance)]
`,
	"ruby": `# frozen_string_literal: true

# A small account with a running balance.
module Demo
  class Account
    attr_reader :owner, :balance

    def initialize(owner)
      @owner = owner
      @balance = 0
    end

    def deposit(amount)
      raise ArgumentError, "non-positive: #{amount}" if amount <= 0

      @balance += amount
    end
  end

  def self.top_owners(accounts)
    accounts.sort_by { |a| -a.balance }.map(&:owner)
  end
end
`,
	"rust": `use std::collections::HashMap;

/// An account with a running balance.
#[derive(Debug, Default)]
struct Account {
    owner: String,
    balance: i64,
}

impl Account {
    fn deposit(&mut self, amount: i64) -> Result<i64, String> {
        if amount <= 0 {
            return Err(format!("non-positive amount: {}", amount));
        }
        self.balance += amount;
        Ok(self.balance)
    }
}

fn main() {
    let mut book: HashMap<String, Account> = HashMap::new();
    for owner in ["ada", "linus"] {
        let acct = book.entry(owner.to_string()).or_default();
        acct.owner = owner.to_string();
        let _ = acct.deposit(100);
    }
    println!("accounts: {}", book.len());
}
`,
	"scala": `package demo

// A sealed hierarchy plus a small fold.
sealed trait Shape {
  def area: Double
}

case class Circle(r: Double) extends Shape {
  def area: Double = math.Pi * r * r
}

case class Rect(w: Double, h: Double) extends Shape {
  def area: Double = w * h
}

object Demo {
  def totalArea(shapes: List[Shape]): Double =
    shapes.foldLeft(0.0)((acc, s) => acc + s.area)

  def main(args: Array[String]): Unit = {
    val shapes = List(Circle(1.0), Rect(2.0, 3.0))
    println(s"total = ${totalArea(shapes)}")
  }
}
`,
	"sql": `-- Schema and a reporting query.
CREATE TABLE accounts (
    id         BIGINT PRIMARY KEY,
    owner      TEXT NOT NULL,
    balance    BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE INDEX idx_accounts_owner ON accounts (owner);

INSERT INTO accounts (id, owner, balance) VALUES
    (1, 'ada', 100),
    (2, 'linus', 250);

SELECT owner, SUM(balance) AS total
FROM accounts
WHERE balance > 0
GROUP BY owner
HAVING SUM(balance) >= 100
ORDER BY total DESC;
`,
	"swift": `import Foundation

// A protocol plus a conforming value type.
protocol Greetable {
    var name: String { get }
    func greet() -> String
}

struct Person: Greetable {
    let name: String

    func greet() -> String {
        return "Hello, \(name)!"
    }
}

func greetAll(_ people: [Greetable]) -> [String] {
    return people.map { $0.greet() }
}

let people = [Person(name: "Ada"), Person(name: "Linus")]
for line in greetAll(people) {
    print(line)
}
`,
	"toml": `# Service configuration.
title = "demo"

[server]
host = "0.0.0.0"
port = 8080
tags = ["prod", "api"]

[database]
url = "postgres://localhost/demo"
max_connections = 25
timeout_seconds = 30.5

[[workers]]
name = "indexer"
concurrency = 4

[[workers]]
name = "reporter"
concurrency = 2
`,
	"typescript": `// A generic in-memory repository.
interface Entity {
  id: string;
}

export class Repository<T extends Entity> {
  private readonly items = new Map<string, T>();

  add(item: T): void {
    this.items.set(item.id, item);
  }

  find(pred: (item: T) => boolean): T[] {
    const out: T[] = [];
    for (const item of this.items.values()) {
      if (pred(item)) out.push(item);
    }
    return out;
  }
}

type User = Entity & { name: string };

const repo = new Repository<User>();
repo.add({ id: "1", name: "Ada" });
const found = repo.find((u) => u.name.startsWith("A"));
console.log(found.length);
`,
	"yaml": `# CI pipeline definition.
name: build
on:
  push:
    branches: [main]
jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        go: ["1.21", "1.22"]
    steps:
      - uses: actions/checkout@v4
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: ${{ matrix.go }}
      - name: Test
        run: go test ./...
`,
}

func init() {
	// terraform is an alias for hcl; reuse the same representative file.
	realSamples["terraform"] = realSamples["hcl"]
}

// parseWithBound runs one parse under a wall-clock watchdog. It returns the
// ParseResult (and any factory error) on completion, or signals timedOut=true if
// the bound elapses first — the signature of a non-terminating parse loop.
//
// NOTE: on timeout the parse goroutine is intentionally abandoned (a true C-side
// loop cannot be interrupted from Go today — that is the very bug this gate
// guards against). The test fails loudly and the process exits, reaping it. We
// never hide a timeout behind a skip.
func parseWithBound(f *treesitter.ParserFactory, source, language string, bound time.Duration) (res *treesitter.ParseResult, err error, timedOut bool) {
	type out struct {
		res *treesitter.ParseResult
		err error
	}
	done := make(chan out, 1)
	go func() {
		r, e := f.Parse(context.Background(), []byte(source), language)
		done <- out{r, e}
	}()
	select {
	case o := <-done:
		return o.res, o.err, false
	case <-time.After(bound):
		return nil, nil, true
	}
}

// TestParseCanary_RealFiles_TimeBounded is the core gate: for every registered
// language, parse a non-trivial representative file under a per-file wall-clock
// bound and assert the tree is sane. This is what would have caught Wave A —
// the trivial-snippet smoke test could not.
func TestParseCanary_RealFiles_TimeBounded(t *testing.T) {
	f := treesitter.NewParserFactory(nil)

	for _, lang := range treesitter.SupportedLanguages() {
		lang := lang
		t.Run(lang, func(t *testing.T) {
			src, ok := realSamples[lang]
			if !ok {
				t.Fatalf("no real-file canary sample for language %q — add one to realSamples", lang)
			}

			start := time.Now()
			res, err, timedOut := parseWithBound(f, src, lang, perFileBound)
			elapsed := time.Since(start)

			if timedOut {
				t.Fatalf("parse(%s) did NOT complete within %s — non-terminating parse loop suspected "+
					"(grammar/runtime ABI mismatch, the Wave A failure class)", lang, perFileBound)
			}

			// ErrHighSyntaxErrorRate still returns a populated ParseResult, so we
			// tolerate it here and inspect the tree directly — but any OTHER error
			// (unsupported language, init failure, nil tree) is a hard failure.
			if err != nil && !errors.Is(err, treesitter.ErrHighSyntaxErrorRate) {
				t.Fatalf("parse(%s) returned unexpected error: %v", lang, err)
			}
			if res == nil || res.TSTree == nil {
				t.Fatalf("parse(%s) returned no tree (res=%v)", lang, res)
			}

			// Root reachable + genuinely non-trivial parse.
			if res.NodeCount < minSaneNodes {
				t.Fatalf("parse(%s) produced only %d nodes (< %d) — sample is not exercising the grammar, "+
					"or the parser bailed out early", lang, res.NodeCount, minSaneNodes)
			}

			// Tree not dominated by ERROR/recovery nodes. A recovery storm (the
			// non-crash manifestation of a grammar that no longer understands the
			// input) drives this toward 1.0; idiomatic code sits near 0.
			if res.ErrorRatio > maxDominatedRatio {
				t.Fatalf("parse(%s) tree dominated by errors: error_ratio=%.4f > %.2f — grammar likely "+
					"no longer understands this input", lang, res.ErrorRatio, maxDominatedRatio)
			}

			t.Logf("parse(%s): %d nodes, error_ratio=%.4f, %s", lang, res.NodeCount, res.ErrorRatio, elapsed)
		})
	}
}

// TestParseCanary_CorpusSettles_TimeBounded is the daemon-settles proxy: it
// parses the entire multi-language corpus sequentially through one factory under
// a single aggregate wall-clock bound, folding every result into the existing
// ParseErrorCanary. A full daemon is too heavy for CI, but every real parse
// funnels through the same in-process chokepoint a runaway daemon parse holds
// (parseMu #481 + the parse slot #5630), so a corpus that never settles is the
// same signal as a daemon that never settles. It also exercises the aggregate
// canary path end-to-end (Observe across many files of many languages).
func TestParseCanary_CorpusSettles_TimeBounded(t *testing.T) {
	f := treesitter.NewParserFactory(nil)
	canary := treesitter.NewParseErrorCanary()

	langs := treesitter.SupportedLanguages()

	type result struct {
		files int
	}
	done := make(chan result, 1)
	go func() {
		files := 0
		for _, lang := range langs {
			src, ok := realSamples[lang]
			if !ok {
				continue
			}
			res, err := f.Parse(context.Background(), []byte(src), lang)
			if err != nil && !errors.Is(err, treesitter.ErrHighSyntaxErrorRate) {
				// Record but keep going; the per-language test reports specifics.
				t.Errorf("corpus parse(%s) unexpected error: %v", lang, err)
				continue
			}
			if res != nil {
				canary.ObserveResult(res)
				files++
			}
		}
		done <- result{files: files}
	}()

	select {
	case r := <-done:
		if r.files == 0 {
			t.Fatal("corpus settle: no files were parsed")
		}
		// Aggregate sanity: no language should have settled into a recovery storm.
		snap := canary.Snapshot()
		rep := treesitter.Classify(snap, &treesitter.Baseline{}, treesitter.SpikeThresholds{
			AbsDelta:  maxDominatedRatio, // trip only on a true storm, not minor grammar quirks
			RelFactor: treesitter.DefaultThresholds().RelFactor,
		})
		for _, l := range rep.Languages {
			if l.CurrentRate > maxDominatedRatio {
				t.Errorf("corpus settle: language %s aggregate error_ratio=%.4f exceeds %.2f",
					l.Language, l.CurrentRate, maxDominatedRatio)
			}
		}
		t.Logf("corpus settled: %d files across %d languages", r.files, len(snap))
	case <-time.After(corpusBound):
		t.Fatalf("corpus did NOT settle within %s — daemon-level non-termination suspected "+
			"(a single looping parse holds parseMu/the parse slot and stalls the whole pipeline, "+
			"the Wave A daemon symptom)", corpusBound)
	}
}
