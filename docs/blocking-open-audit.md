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

As of `c3e3720a8` plus this commit. `internal/licenses` is fixed here; the
32 below are **not**, and are deliberately left for sized follow-up work rather
than swept into an already-large PR.

| Site | Literal name(s) | Note |
|---|---|---|
| `internal/daemon/walk/walker.go:343` | `.grafelignore` | **Highest severity.** An unguarded read *inside the walker package itself*, so the entry-type gate #6468 added never applies to it. `mkfifo .grafelignore` in any subdirectory of an indexed repo hangs the walk. |
| `internal/gitmeta/cache.go:135` | `commondir` | Runs on every repo before any walk. |
| `internal/gitmeta/cache.go:171` | `refs/…` from HEAD | |
| `internal/gitmeta/cache.go:206` | `HEAD` | |
| `internal/gitmeta/cache.go:279` | `.git` | |
| `internal/gitmeta/sparse.go:105` | `info/sparse-checkout` | |
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

## Done

| Package | Site |
|---|---|
| `internal/extractors/javascript` | `aliases.go:144` |
| `internal/extractors/golang` | `gomod.go:94` |
| `internal/extractors/yaml` | `helm.go:115` |
| `internal/install/detect` | `detect.go:554`, `:588`, `:604` |
| `internal/secrets` | `secrets.go:443` |
| `internal/licenses` | `licenses.go:106` — one `readLicenseFile` choke point covering all ten former sites |

## Counts

| Bucket | Count |
|---|---|
| name-chosen, open | 32 |
| name-chosen, fixed (already-safeio) | 8 |
| walker-gated | 40 |
| not-applicable | 243 |
| **total non-test call sites** | **324** |

Across 206 non-test files of the 2118 `.go` files under `internal/` + `cmd/`.
599 further hits in `_test.go` files are out of scope.

The 243 not-applicable and 40 walker-gated rows are summarised rather than
listed. Enumerating them in a checked-in document would rot within weeks and
add no signal: the actionable list is the 32 above, and the method below
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
