# Working with multiple branches in archigraph

archigraph indexes one graph snapshot per `(repository, git ref)` pair. When
you switch branches or use `git worktree`, the daemon detects the change,
keeps the old graph in cache, and begins indexing the new ref — so your AI
agent always queries the graph that matches the code it is looking at.

This guide explains what the feature does, how to use it from the CLI, the
dashboard, and MCP, and what to expect for storage and performance.

---

## How it works

### Per-ref graph snapshots

Each indexed `(repo, ref)` pair gets its own directory inside the archigraph
store:

```
~/.archigraph/store/<repo-slug>/refs/<ref-safe>/graph.fb
```

`<ref-safe>` is the branch name with `/` replaced by `%2F` so that
`feature/foo` stores as `feature%2Ffoo` on every OS. The store directory is
determined by `StateDirForRepoRef` in `internal/daemon/state_path.go`.

### Automatic ref tracking

When you run:

```sh
git checkout feature/my-thing
```

…the daemon detects the HEAD change via its `.git/HEAD` watcher and
automatically starts indexing `feature/my-thing`. The previous branch's graph
stays in the cache during indexing, so you never have a gap.

For faster detection, install the archigraph git hooks:

```sh
archigraph install-hooks
```

This adds `post-checkout`, `post-merge`, and `post-rewrite` hooks that signal
the daemon immediately on branch switch, merge, or rebase — reducing the
detection lag to near-zero.

### HOT / WARM / COLD tiers

archigraph keeps graphs in memory according to how recently they were used:

| Tier    | Description                                                   |
|---------|---------------------------------------------------------------|
| HOT     | In RAM, graph.fb mmap'd. Instant queries.                    |
| WARM    | In RAM, idle ≥5 min. Will be released soon.                  |
| COLD    | On disk only. Auto-wakes in ≤100 ms on first query.          |
| EXPIRED | Deleted from disk (non-pinned refs only, after long idle).   |

The **default branch** (`main` / `master` / the branch set in
`origin/HEAD`) is always pinned HOT and never evicted. Feature branches and
worktree branches age through WARM → COLD → EXPIRED based on idle time.

You can view the tier for every indexed ref:

```sh
archigraph branches              # lists all indexed refs with tier + stats
archigraph branches --evict feature/old-branch   # force-evict a ref
```

---

## CLI usage

### Querying a specific ref

Most read commands accept `--ref <name>`:

```sh
archigraph status --ref feature/my-thing        # status for that ref's graph
archigraph index  --ref feature/my-thing        # force reindex that ref
archigraph doctor --ref feature/my-thing        # health check for that ref
archigraph remove --ref feature/my-thing        # delete that ref's snapshot
```

### Listing all indexed refs

```sh
archigraph branches                        # all indexed refs for all groups
archigraph status --all-refs               # per-group status for every ref
```

Example output:

```
Group: my-api
  main              HOT    23.4 MB   indexed 2 min ago   1 243 entities
  feature/auth      WARM   24.1 MB   indexed 18 min ago  1 289 entities
  hotfix/login-bug  COLD   23.5 MB   indexed 3 h ago     1 243 entities
```

### Forcing a reindex on branch switch

The daemon reindexes automatically, but if you want to trigger it immediately:

```sh
archigraph rebuild --ref feature/my-thing
```

---

## Dashboard usage

### Switching refs in the UI

The topbar in the dashboard contains a **ref selector** (labelled with the
currently-active ref name). Click it to open a dropdown listing every indexed
ref for the current group. Selecting a ref appends `?ref=<name>` to the URL
and updates all dashboard surfaces (Graph, Paths, Topology, Flows, Quality,
Pending) to show data for that ref.

The selected ref persists across navigation within the group — navigate from
Graph to Paths and back, the ref stays the same.

To return to the current HEAD view, select **@current** from the dropdown or
clear the `?ref=` parameter from the URL.

### Comparing two refs (graph diff)

Navigate to the **Graph** surface and click **Compare refs** in the toolbar.
Select two refs and click **Compare**. The diff view shows:

- Entities added, removed, or structurally modified between the two refs.
- Relationships added or removed.
- A summary counter (`+N entities / -M entities / ~K modified`).

The underlying API endpoint is:

```
GET /api/v2/groups/:group/repos/:repo/diff?refA=main&refB=feature%2Fmy-thing
```

---

## MCP / agent usage

### How refs work in MCP queries

The MCP server resolves the correct ref from the caller's current working
directory. When an agent runs inside a repo that has `feature/my-thing`
checked out (or inside a linked worktree for that branch), all
`archigraph_*` tool calls automatically target the `feature/my-thing`
graph. No flag or parameter is needed.

To explicitly target a ref, pass `ref` in the tool arguments (supported by
all graph-query tools):

```json
{
  "tool": "archigraph_find",
  "arguments": {
    "query": "authentication handler",
    "ref": "feature/auth"
  }
}
```

If the requested ref is COLD, the daemon wakes it automatically (≤100 ms).
If it has never been indexed, the daemon returns a `ref_not_indexed` error
with a list of available refs.

### Cross-ref queries

An agent can ask for the structural diff between two refs in a single call:

```json
{
  "tool": "archigraph_diff_refs",
  "arguments": {
    "repo": "my-repo",
    "ref_a": "main",
    "ref_b": "feature/auth"
  }
}
```

This is useful for PR review workflows: the agent can check what changed
structurally before looking at the line-level diff.

---

## Git worktree workflows

`git worktree add` creates a parallel checkout at a secondary path. archigraph
detects linked worktrees automatically at daemon startup and on each watcher
cycle via `git worktree list --porcelain`. Each discovered worktree is
registered as a separate `(path, ref)` slot and indexed like any other branch.

```sh
# Create a worktree for a hotfix while staying on main
git worktree add ../my-repo-hotfix hotfix/login-bug

# The daemon detects the new worktree and begins indexing it.
# You can confirm:
archigraph branches
#   main              HOT    23.4 MB   ...
#   hotfix/login-bug  HOT    23.5 MB   indexing (42%)...
```

When you remove a worktree (`git worktree remove`), the daemon transitions the
slot to COLD on the next discovery cycle and eventually evicts it (EXPIRED).

Worktree slots use a more aggressive eviction schedule — WARM→COLD at 30 min
instead of 1 h — because linked worktrees tend to be short-lived.

---

## Storage growth and eviction

### How much storage does multi-branch indexing use?

Each `(repo, ref)` slot stores one `graph.fb` file. Typical sizes:

| Repo size  | graph.fb    |
|------------|-------------|
| Small (~500 entities)   | 1–3 MB   |
| Medium (~5 k entities)  | 5–15 MB  |
| Large (~50 k entities)  | 30–80 MB |

If you have 5 indexed branches on a medium repo, expect 25–75 MB of extra
store usage. The default-branch snapshot is always kept; the oldest
non-default refs are evicted first.

### Tuning the eviction schedule

The tier TTLs are controlled by environment variables (set them in your shell
profile or before starting the daemon):

| Variable                             | Default | Effect                               |
|--------------------------------------|---------|--------------------------------------|
| `ARCHIGRAPH_TIER_HOT_TO_WARM_MIN`    | 5       | Minutes idle before HOT → WARM       |
| `ARCHIGRAPH_TIER_WARM_TO_COLD_MIN`   | 60      | Minutes idle before WARM → COLD      |
| `ARCHIGRAPH_TIER_COLD_TO_EXPIRED_H`  | 168     | Hours idle before COLD → EXPIRED (disk delete) |

Setting `ARCHIGRAPH_TIER_COLD_TO_EXPIRED_H=0` disables disk eviction entirely
(refs are kept on disk forever until manually removed).

### Manually inspecting and evicting refs

```sh
# Show all indexed refs with size + tier
archigraph branches

# Evict (delete from disk) a specific ref
archigraph branches --evict feature/old-branch

# Remove all non-default refs for a group
archigraph branches --evict-all --group my-api
```

---

## Cache invalidation on cross-repo links

When your group contains multiple repos, archigraph builds cross-repo links
(import chains, shared type references, HTTP call graphs) between them.
With multi-ref indexing, these links are keyed by `(repoA, refA, repoB,
refB)`. When you switch a ref in any member repo, the daemon automatically
invalidates the cache entries that involve that `(repo, old-ref)` pair.
The next cross-repo query recomputes the links from the new ref's graph.

No user action is required. The invalidation is synchronous — a query that
races with a ref switch will find a cache miss and wait for the recompute
rather than serving stale data.

---

## Troubleshooting

**"ref not indexed" error in MCP**

The ref you requested has never been indexed. Run:

```sh
archigraph index --ref feature/my-thing
```

Or wait for the daemon's background HEAD watcher to pick it up after the next
`git checkout`.

**Graph looks stale after branch switch**

If the git hooks are not installed, the daemon relies on the fsnotify HEAD
watcher, which has a short delay. Install the hooks for instant detection:

```sh
archigraph install-hooks
```

Check that the hooks were installed successfully:

```sh
archigraph doctor
# Should show: git-hooks: post-checkout ✓  post-merge ✓  post-rewrite ✓
```

**Branches command shows an unexpected ref**

Detached HEAD checkouts appear as the abbreviated commit SHA (e.g.
`abc1234`). The daemon indexes them as a normal ref and evicts them following
the same TTL schedule as feature branches.

**Too much disk space used**

Lower `ARCHIGRAPH_TIER_COLD_TO_EXPIRED_H` to evict stale refs sooner, or
manually evict with `archigraph branches --evict-all`. The default-branch
snapshot is never evicted regardless of this setting.

---

## See also

- [ADR-0020](../adrs/0020-multi-branch-worktree.md) — architectural record for the multi-branch + worktree design
- [ADR-0017](../adrs/0017-single-binary-daemon-architecture.md) — daemon architecture (tiered hibernation foundations)
- [ADR-0016](../adrs/0016-binary-graph-format.md) — FlatBuffers graph format (the on-disk format for per-ref snapshots)
- [SSE endpoints](../sse-endpoints.md) — real-time event stream; `WSEvent.Ref` carries the ref for filtering
