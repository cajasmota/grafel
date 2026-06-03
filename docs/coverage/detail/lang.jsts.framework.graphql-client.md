<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.graphql-client` — GraphQL Client (Apollo Client / urql / Relay / graphql-request)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 36

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | 🔴 `missing` | — | 3642 | — | — |
| Context extraction | 🔴 `missing` | — | 3642 | — | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | 🔴 `missing` | — | 3642 | — | — |
| Data fetching | ✅ `full` | `2026-06-03` | 3642 | `internal/engine/http_endpoint_client_synthesis.go`<br>`internal/engine/http_endpoint_graphql_client.go`<br>`internal/engine/http_endpoint_graphql_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Apollo Client / urql / Relay (react-relay) / graphql-request client GraphQL operations -> one http_endpoint_call (consumer) per (operation, root field) keyed to the server endpoint shape http:GRAPHQL:/graphql/<RootType>/<field> + FETCHES from the enclosing component (synthesizeGraphQLClientCalls). Root type Query/Mutation/Subscription + root fields parsed from the gql/graphql tagged or raw template document (shared gqlRootFieldsFromDoc with the Dart pass). Idioms: gql-const + useQuery/useMutation/useSubscription/useLazyQuery (Apollo), useQuery({query}) (urql), useLazyLoadQuery/usePreloadedQuery/useClientQuery first-positional-arg doc (Relay), request(endpoint, raw-template) + client.query({query}) (graphql-request). Identical entity shape to the GraphQL server producer so the cross-repo linker pairs the client component with the backend resolver on reindex. Value-asserted: useQuery(GET_USERS) -> http:GRAPHQL:/graphql/Query/users + FETCHES Function:UserList; useMutation -> Mutation/createUser; multi-root urql; Relay usePreloadedQuery(const) -> Query/viewer+notifications; negatives (REST fetch, Relay useFragment fragment-only) emit nothing. Honest-partial: a gql const imported cross-file emits an op-name-keyed graphql_client_unresolved call (field not statically resolvable). |
| Prop extraction | 🔴 `missing` | — | 3642 | — | — |
| State management | 🔴 `missing` | — | 3642 | — | — |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | 🔴 `missing` | — | 3642 | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 3642 | — | — |
| Interface extraction | 🔴 `missing` | — | 3642 | — | — |
| Type alias extraction | 🔴 `missing` | — | 3642 | — | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | 🔴 `missing` | — | 3642 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 3642 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 3642 | — | — |
| Config consumption | 🔴 `missing` | — | 3642 | — | — |
| Constant propagation | 🔴 `missing` | — | 3642 | — | — |
| DB effect | 🔴 `missing` | — | 3642 | — | — |
| Dead code detection | 🔴 `missing` | — | 3642 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 3642 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 3642 | — | — |
| Error flow | 🔴 `missing` | — | 3642 | — | — |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic JS/TS engine pass, fires regardless of framework). Verified to attribute to the enclosing function: LaunchDarkly ldClient.variation/boolVariation/stringVariation, Unleash unleash.isEnabled, OpenFeature client.getBooleanValue, Unleash-React useFlag, Split.io getTreatment, Flagsmith hasFeature, plus GrowthBook gb.isOn/isOff/getFeatureValue and ConfigCat configCatClient.getValue/getValueAsync (receiver-gated). Honest-partial: dynamic keys + non-flag receivers (button.isOn, formData.getValue) emit nothing. |
| Fs effect | 🔴 `missing` | — | 3642 | — | — |
| HTTP effect | ✅ `full` | `2026-06-03` | 3642 | `internal/engine/http_endpoint_graphql_client.go`<br>`internal/engine/http_endpoint_graphql_client_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | Client GraphQL operations surface as http_endpoint_call (consumer) entities with the synthetic GRAPHQL verb on canonical path /graphql/<RootType>/<field>, matching the server producer so the cross-repo HTTP linker forms client->server links + FETCHES from the enclosing component. See data_fetching for the per-idiom detail. |
| Import resolution quality | 🔴 `missing` | — | 3642 | — | — |
| Module cycle detection | 🔴 `missing` | — | 3642 | — | — |
| Mutation effect | 🔴 `missing` | — | 3642 | — | — |
| Pure function tagging | 🔴 `missing` | — | 3642 | — | — |
| Reachability analysis | 🔴 `missing` | — | 3642 | — | — |
| Request shape extraction | 🔴 `missing` | — | 3642 | — | — |
| Response shape extraction | 🔴 `missing` | — | 3642 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 3642 | — | — |
| Schema drift detection | 🔴 `missing` | — | 3642 | — | — |
| Taint sink detection | 🔴 `missing` | — | 3642 | — | — |
| Taint source detection | 🔴 `missing` | — | 3642 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 3642 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 3642 | — | — |

## Related extraction records

This record provides code-level coverage for the
[`protocol.graphql`](./protocol.graphql.md) hub record (GraphQL),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.graphql-client ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
