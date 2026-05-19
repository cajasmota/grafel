# Quality baseline — Phase 1 (2026-05-19)

First run of the extraction-quality framework against `main` (after PR #600
landed). Numbers below are reproducible via:

```bash
scripts/quality/run.sh
```

## python-django-mini

| Metric | Value |
|---|---|
| Entity recall (must-have) | **28 / 28 = 100.0%** |
| Relationship recall (must-have) | **12 / 12 = 100.0%** |
| Forbidden-relationship hits | **0** |
| Nice-to-have entities | 0 / 2 |
| Nice-to-have relationships | 0 / 4 |
| Extracted entity total | 90 |
| Extracted relationship total | 70 |

Fixture: `internal/quality/golden/python-django-mini/` —
11 source files exercising Django Model + custom Manager + class-based +
function-based Views + URLconf + post_save signal receiver + admin
registration + `AppConfig.ready` import side-effect.

### Outstanding nice-to-haves

These shapes the indexer doesn't yet capture; each is a candidate for a
followup chain-fix issue:

1. `@admin.register(User)` -> `DEPENDS_ON` edge from `UserAdmin` to `User`.
   Today the binding is implicit; surfacing it as an edge would make admin
   coverage discoverable from the graph.
2. `@receiver(post_save, sender=User)` -> a synthesized
   `post_save_receiver` SCOPE.Pattern entity binding receiver to model.
3. `UserDetailView.get` -> `User.full_label` cross-file method binding.
   Currently `full_label` stays as a bare-name CALLS stub because the
   resolver has no type-of-`user` inference. This is the same shape that
   shows up across the corpus as bug-resolver edges.
4. `include('users.urls')` -> direct SERVES fan-out instead of per-Route.

The framework reports these as `nice_to_have` so they don't gate CI, but
each will appear in the JSON output for trend-tracking once Phase 2 adds
the regression channel.

## Notes on methodology

- The fixture's source tree is run through the SAME indexer pipeline used
  in production (`cmd/archigraph.Index`), so a regression in any pass
  (extract / framework rules / cross-language / resolver / synthesis)
  surfaces here.
- Fixtures are tiny on purpose (<15 files) — a fixture failure points at a
  specific extractor or rule, not at a corpus-wide trend. Corpus-level
  trends are still measured by verify2 / bug-rate.
- The reporter annotates each miss with WHY (neither endpoint extracted /
  from missing / to missing / both present but edge absent). The last
  category is the most actionable.
