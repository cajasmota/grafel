# `<module-slug>`

> One paragraph: what this module does and why it exists. Concrete, not abstract — name the central entity in backticks.

## Key entities

| Entity | Kind | File | Role |
| ------ | ---- | ---- | ---- |
| `<entity>` | Component\|Module\|Function\|Class | `<file:line>` | <one line> |

> Pulled from `grafel_find` + `grafel_subgraph`. Sort by centrality. Entity names always backticked.

## Responsibilities

- <bullet>
- <bullet>

## Inbound edges

> What calls into this module. Each bullet names the source entity in backticks and links to its module's `README.md` (or to its repo's overview if cross-repo).

- `<source entity>` (in [`<module>`](../<module>/README.md)) — <how>.

## Outbound edges

> What this module calls. Same format as inbound.

## Internal flows

> If the module has runtime flows worth diagramming, link to [`flows.md`](./flows.md). Otherwise omit this section.

## Configuration

> Env vars, feature flags, or settings consumed by this module. Each name in backticks; cross-link to [`../../reference/config.md`](../../reference/config.md).

## Tests

> One paragraph on how to run this module's tests and what they cover. Skip if test coverage is uniform across the repo.

## Known gaps

> If `grafel_docgen_apply(kind="enrichments", action=list)` returned anything blocking accurate documentation of this module, list it here. Each item: candidate id, what is unknown, what would unblock it.
