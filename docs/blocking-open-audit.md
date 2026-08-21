# Blocking-open audit (#6416)

`os.ReadFile` and `os.Open` on a FIFO block in `open(2)` until a writer appears.
Nothing bounds that wait: no timeout, no error, no log line. An unprivileged
user plants one with a single `mkfifo`. `internal/safeio` exists to make that
impossible; this document records **which call sites still do not use it**.

## Why this document exists

PR #6468 asserted "the class is closed" three times and was wrong three times.
Each round fixed exactly the sites the previous reviewer had named and swept no
further:

| Round | Fixed | Missed, found by the next reviewer |
|---|---|---|
| 1 | `walk` entry-type gate | `install/detect`, `secrets` |
| 2 | `install/detect`, `secrets` | six sites in `javascript/aliases.go` |
| 3 | `aliases.go`, `gomod.go`, `helm.go` | ten sites in `internal/licenses` |
| 4 | `internal/licenses` | `walk`'s OWN `.grafelignore` read, and five in `internal/gitmeta` |

The missed sites were never hard to find. A grep for a read with a literal
filename under `internal/` takes seconds and would have caught every one of
them in round 1. The enumeration below is that grep, done once, properly, so
the next person does not have to rediscover the same class a fifth time.

## Classification

Every non-test `os.ReadFile` / `os.Open` / `os.OpenFile` call under `internal/`
and `cmd/` falls into one of four buckets.

- **name-chosen** — the path is built from a literal filename or a fixed list
  of them, so `mkfifo <thatname>` in a scanned tree reproduces the hang. **No
  walker sits in between, so the walker's entry-type gate cannot protect it.**
  This is the bucket that matters.
- **walker-gated** — the path comes from `walkSourceFiles`, a `filepath.WalkDir`
  with an entry-type gate, or a `files []string` derived from one. Only a TOCTOU
  residual remains, and `safeio`'s fstat-on-descriptor layer closes that at the
  sites that already use it.
- **already-safeio** — routed through `safeio` and reported. Done.
- **not-applicable** — reads of grafel's own state (`~/.grafel`, sidecars, the
  daemon log, pidfiles), writes, procfs, or a path the user typed as a CLI flag.
  A hostile FIFO in any of those means the attacker already owns the account.

A literal leaf joined onto a directory that itself came from a `ReadDir` still
counts as **name-chosen** — the leaf is what the attacker names.

## The open list: name-chosen sites not yet routed through safeio

Round 5 fixes the two groups that block `grafel index` ITSELF — the walker's
own inherited-`.grafelignore` read and all five reads in `internal/gitmeta` —
and moves those six rows to Done. The 26 below are **not** fixed, and are
deliberately left for sized follow-up work rather than swept into an
already-large PR. Every one of them is reached only after indexing has started
or through a surface other than `grafel index`, which is why they could be
deferred and the six could not.

| Site | Literal name(s) | Note |
|---|---|---|
| `internal/daemon/worktree/classify.go:125` | `.git` | `Lstat` + `!IsDir()` only; a FIFO passes. |
| `internal/daemon/worktree/worktree.go:813` | `.git` | `os.Stat` + `!IsDir()` only. |
| `internal/daemon/watch/gitignore.go:81` | `.grafel/watch.json` | Per-repo override inside the watched repo. |
| `internal/install/rulesfiles/rulesfiles.go:267` | `Targets` list | |
| `internal/install/rulesfiles/rulesfiles.go:437` | `.claude/CLAUDE.md` | |
| `internal/install/rulesfiles/rulesfiles.go:486` | `WriteTargets` list | |
| `internal/install/rulesfiles/rulesfiles.go:537` | `Targets` list | |
| `internal/install/hooks/hooks.go:65` | `HookNames` list | |
| `internal/install/hooks/hooks.go:139` | `.git` | |
| `internal/install/hooks/hooks.go:157` | `HookNames` list | |
| `internal/install/hooks_install.go:196` | `pre-push` &c. | |
| `internal/install/doctor.go:769` | `.gitignore` | |
| `internal/install/gitignore.go:31` | `.gitignore` | |
| `internal/agents/inject_map.go:216` | `AGENTS.md`/`CLAUDE.md`/`GEMINI.md` | |
| `internal/agentpatterns/sync.go:113` | `CLAUDE.md` | |
| `internal/agentpatterns/sync.go:147` | `CLAUDE.md` | |
| `internal/cli/register.go:170` | `AGENTS.md` | |
| `internal/dashboard/handlers_repo_manifest.go:226` | `AGENTS.md`/`CLAUDE.md`/`GEMINI.md` | `os.Stat` guard does not check regularity. |
| `internal/dashboard/handlers_onboard.go:299` | `.grafel/group.json` | Path is user-supplied. |
| `internal/mcp/routing.go:242` | `.grafel/group.json` | Walks up from CWD. |
| `internal/registry/registry.go:690` | `.grafel/group.json` | In a registered repo. |
| `internal/engine/http_wrapper_detection.go:151` | `.grafel/wrappers.json` | Inside the indexed repo. |
| `internal/engine/kafka_edges.go:508` | `src/main/resources/application.properties` | All segments literal. |
| `internal/engine/orm_queries_python_mongo_agg.go:1310` | module → `.py` path | Constructed, not walked. |
| `internal/coverage/enrich.go:237` | `DefaultReportPath` | Zero-config discovery from the repo root. |
| `internal/quality/fitness/rule.go:164` | `.grafel/fitness.yaml` | Inside the user repo. |

One judgement call worth stating: `internal/custom/javascript/prisma.go:176`
takes its leaf from a `ReadDir` filtered on the `.prisma` suffix with only an
`IsDir()` gate, so `mkfifo evil.prisma` reproduces the hang identically. It is
listed as name-chosen rather than allowed to fall between buckets.

Two sites sit on the boundary and are currently **not-applicable** on a stated
threat model rather than on their shape: `internal/quality/expected.go:108`
(`expected.json` under a `--fixture-dir` the user typed) and
`internal/dashboard/handlers_skills.go:252` (`SKILL.md` in grafel's own skills
cache). If a hostile `--fixture-dir` enters the threat model, the first moves.

## Judgement: does living under `.git/` lower the severity?

Round 5 had to decide whether the five `internal/gitmeta` reads deserved
guarding at all, since `.git/` is not attacker-controlled the way a tracked
file is. The answer is that it lowers the *likelihood* and RAISES the *impact*,
so the net severity is higher than the walker's, not lower.

- **Nothing enforces the file types inside `.git/`.** git writes HEAD, commondir
  and loose refs as regular files, but the directory is an ordinary directory
  with ordinary permissions; anything that can write there can `mkfifo HEAD`.
  A hostile repo is one shape of that (`.git` itself is a FIFO — the layout
  `git clone` never produces but a tarball, a rsync mirror or an artefact
  restore can), and so is a botched restore that recreated a path as a pipe.
  "Unlikely" is not "impossible", and the whole class exists because an
  unbounded wait has no recovery: no timeout fires, no error is returned, no
  line is logged.
- **The blast radius is the whole run.** Every one of these five reads is on the
  `CaptureCached` / `CaptureCachedFresh` / sparse-detection path, which executes
  on EVERY repo BEFORE any walk begins. A block here is not "one file missing
  from the index" — it is an index that never starts, a `grafel_whoami` that
  never answers, and a daemon goroutine parked for the life of the process. No
  downstream gate can help, because nothing downstream has run.
- **The cost of guarding is a stat.** safeio's gate is one `os.Stat` on files
  that are read a handful of times per repo. There is no throughput argument on
  the other side of the ledger, so even a low likelihood clears the bar.

No subset was dropped. All five are routed.

## Routing is not enough on its own

A site only leaves the **name-chosen, open** bucket when it is BOTH routed
through `safeio` AND reports what it skipped. Rounds 2 and 3 routed
`internal/install/detect` and `internal/secrets` but left all four call sites
mapping `ErrNotRegular` and `ErrWouldBlock` to a bare `return nil` — the hang
was closed and the silence was kept, so `mkfifo creds.go` produced a secrets
scan that answered "clean" without ever reading `creds.go`. That is the #6338
shape this PR invokes as its own rationale for the walker's report, so the
`Done` table below means routed *and* reported, and nothing is listed there
until both hold.

The reporting convention is the same at every site: always-on to stderr,
deduplicated by path, capped at 16 with a suppression notice, and gated to
`ErrNotRegular` / `ErrWouldBlock` only — ENOENT is the ordinary case at every
one of these reads and stays silent. `ErrWouldBlock` is returned BARE, carrying
no path, so each reporter decorates it via a local `withPath` helper; a warning
that names no file is not a safety net.

One gap is known and deliberate. `internal/secrets.ScanPath` returns
`([]Finding, error)` to the daemon's MCP secrets tool and to an HTTP dashboard
handler, and its skip line goes to stderr — the daemon log — not into the
returned result. So the record exists, but the MCP/HTTP *caller* cannot see
that the tree was only partly read. Closing that means changing `ScanPath`'s
signature at both entry points; it is a follow-up, not something to smuggle
into a reporting change.

## Confirmed already safe

`internal/daemon/walk` was swept for sibling reads of the same shape as
`walker.go:343`, since a package that missed one is likely to have missed two.
It had not: `ParseIgnoreFile` reads `.gitignore` / `.grafelignore` through
`openWithDeadline`, which `os.Lstat`s and returns `ErrIgnoreFileTimeout` for any
non-regular entry BEFORE opening anything — in `gitignore_unix.go` (which also
opens `O_NONBLOCK`) and in `gitignore_windows.go` (plain `os.Open` after the same
Lstat) alike. Both were read rather than assumed. `walker.go:343` was the only
ungated read in the package.

## Done

| Package | Site |
|---|---|
| `internal/extractors/javascript` | `aliases.go:144` |
| `internal/extractors/golang` | `gomod.go:94` |
| `internal/extractors/yaml` | `helm.go:115` |
| `internal/install/detect` | `detect.go` — one `readManifest` choke point covering all three parsers |
| `internal/secrets` | `secrets.go` — `scanFile`'s `safeio.Open` |
| `internal/licenses` | `licenses.go:106` — one `readLicenseFile` choke point covering all ten former sites |
| `internal/daemon/walk` | `walker.go:343` — `readIgnoreFile` (`saferead.go`). The gate this PR added covers entries the WALKER produced; this read is name-chosen and runs before the walk. |
| `internal/gitmeta` | `cache.go` `commondir` / `refs/…` / `HEAD` / `.git`, and `sparse.go`'s `info/sparse-checkout` — one `readGitMetaFile` choke point covering all five |

## Counts

| Bucket | Count |
|---|---|
| name-chosen, open | 26 |
| name-chosen, fixed (already-safeio) | 14 |
| walker-gated | 40 |
| not-applicable | 243 |
| **total non-test call sites** | **324** |

Across 206 non-test files of the 2118 `.go` files under `internal/` + `cmd/`.
599 further hits in `_test.go` files are out of scope.

The 243 not-applicable and 40 walker-gated rows are summarised rather than
listed. Enumerating them in a checked-in document would rot within weeks and
add no signal: the actionable list is the 26 above, and the method below
regenerates the rest on demand.

## How to redo this

```sh
# NEVER run this from the repo root — .claude/worktrees/ holds full checkouts,
# which times the grep out and inflates the counts about fivefold.
grep -rn --include='*.go' -e 'os\.ReadFile(' -e 'os\.Open(' -e 'os\.OpenFile(' \
    internal/ cmd/ | grep -v '_test.go'
```

Then trace each path variable back to its assignment. A grep line alone does
not classify a site: `os.ReadFile(p)` says nothing until you know where `p`
came from, and the majority of the name-chosen sites above are of exactly that
two-line shape.

## Could a lint keep this closed?

Yes, and the repo already has the shape for it — `internal/atomicfile/guard_6018_test.go`,
`internal/install/home_isolation_guard_6171_test.go` and
`internal/mcp/schema_contract_ast_test.go` are all AST-walking guard tests with
an embedded allowlist that fail when a new violating site appears.

The same approach works here: parse every non-test file under `internal/` and
`cmd/`, find calls to `os.ReadFile`/`os.Open`/`os.OpenFile`, and resolve the
path argument one level back to its assignment. Flag any site whose path
expression contains a string literal that looks like a filename, unless
`file:line` appears in an allowlist carrying a reason string. The rule is
deliberately syntactic and over-broad — it would flag grafel's own state reads
too — which is why the allowlist has to carry reasons rather than bare paths.

What it **cannot** decide is the interesting question. "Is this directory inside
a repo an attacker can write to, or inside `~/.grafel`?" is a judgement about
provenance that no AST pass answers; that is what the 243 not-applicable rows
encode, and a checker would need every one of them in its allowlist on day one.
So the honest framing is that the lint pins the *boundary* — no new
literal-named read appears without someone writing down why it is safe — rather
than proving anything about the sites already there. That is still worth
having, because every one of the four missed rounds above was a NEW site, or an
old one nobody had looked at, going in unannotated.

It is not built in this commit. It should be sized as its own piece of work,
because the allowlist is the expensive part and it has to be right or it
becomes a rubber stamp.
