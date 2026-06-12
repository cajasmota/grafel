<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.framework.wcf` — WCF

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 54

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Federation extraction | — `not_applicable` | — | — | — | Apollo GraphQL Federation directives do not exist in SOAP/WCF RPC. |
| Procedure extraction | 🟢 `partial` | — | 4968 | `internal/custom/csharp/wcf.go`<br>`internal/custom/csharp/wcf_test.go` | [ServiceContract] interfaces/classes -> service:<Name>; [OperationContract] methods -> operation:<Name>; emitted as SCOPE.Schema/procedure_extraction (#4968). |
| Schema extraction | 🟢 `partial` | — | 4968 | `internal/custom/csharp/wcf.go`<br>`internal/custom/csharp/wcf_test.go` | [DataContract] classes -> datacontract:<Name>; [DataMember] properties -> datamember entities; SCOPE.Schema/schema_extraction (#4968). |
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL SDL object-type graph concept; WCF data contracts are modelled under schema_extraction, no GraphQL object-type relationship graph. |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | 🔴 `missing` | — | 4968 | — | — |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | 🟢 `partial` | — | 4968 | `internal/custom/csharp/wcf.go`<br>`internal/custom/csharp/wcf_test.go` | new ServiceHost(typeof(X)) self-host + CoreWCF AddServiceModelServices()/AddServiceEndpoint<TSvc,TContract>() -> SCOPE.Pattern/transport_binding (#4968). |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | — `not_applicable` | — | — | — | WCF versions via contract/namespace evolution, not HTTP route/Sunset-header versioning. |
| Endpoint pagination posture | — `not_applicable` | — | — | — | HTTP limit/offset/page/cursor pagination posture is an HTTP-endpoint concept; not applicable to SOAP/WCF RPC. |
| Endpoint response codes | — `not_applicable` | — | — | — | WCF signals outcome via SOAP faults, not HTTP status-code sets. |
| Endpoint synthesis | — `not_applicable` | — | — | — | No HTTP path+verb producer endpoints; WCF endpoints are bindings (ServiceHost/AddServiceEndpoint), captured as transport_binding. |
| Handler attribution | — `not_applicable` | — | — | — | No HTTP handler->route attribution; operation->service binding is modelled by procedure_extraction. |
| Route extraction | — `not_applicable` | — | — | — | WCF addresses operations by SOAP action / contract.operation, not HTTP route paths; surfaced via procedure_extraction (service/operation), not HTTP routes. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | — `not_applicable` | — | — | — | WCF services render no server-side views/templates; responses are serialized data contracts. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 4968 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 4968 | — | — |
| Request validation | 🔴 `missing` | — | 4968 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 4968 | — | — |
| Rate limit stamping | 🔴 `missing` | — | 4968 | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | — | — | `internal/extractor/enum_valueset.go`<br>`internal/extractors/csharp/csharp.go` | enum_declaration -> SCOPE.Schema/enum + value-set; framework-agnostic. |
| Interface extraction | ✅ `full` | — | — | `internal/extractors/csharp/csharp.go` | tree-sitter CST interface_declaration -> SCOPE.Component; framework-agnostic, fires on [ServiceContract] interfaces. |
| Type alias extraction | — `not_applicable` | — | — | — | C# has only file-scoped using-aliases, not first-class type aliases (same as all C# frameworks). |
| Type extraction | ✅ `full` | — | — | `internal/extractors/csharp/csharp.go` | tree-sitter CST class/struct/record_declaration -> SCOPE.Component; framework-agnostic, fires on WCF service/contract classes. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 4968 | — | — |
| DI injection point | 🔴 `missing` | — | 4968 | — | — |
| DI scope resolution | 🔴 `missing` | — | 4968 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | — | — | `internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go` | C# NUnit/xUnit/MSTest test-attr detection is framework-agnostic; links WCF service tests. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 4968 | — | — |
| Metric extraction | 🔴 `missing` | — | 4968 | — | — |
| Trace extraction | 🔴 `missing` | — | 4968 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 4968 | — | — |
| Config consumption | 🔴 `missing` | — | 4968 | — | — |
| Constant propagation | 🔴 `missing` | — | 4968 | — | — |
| DB effect | 🔴 `missing` | — | 4968 | — | — |
| Dead code detection | 🔴 `missing` | — | 4968 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 4968 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 4968 | — | — |
| Error flow | 🔴 `missing` | — | 4968 | — | — |
| Feature flag gating | 🔴 `missing` | — | 4968 | — | — |
| Fs effect | 🔴 `missing` | — | 4968 | — | — |
| HTTP effect | 🔴 `missing` | — | 4968 | — | — |
| Import resolution quality | 🔴 `missing` | — | 4968 | — | — |
| Module cycle detection | 🔴 `missing` | — | 4968 | — | — |
| Mutation effect | 🔴 `missing` | — | 4968 | — | — |
| Pure function tagging | 🔴 `missing` | — | 4968 | — | — |
| Reachability analysis | 🔴 `missing` | — | 4968 | — | — |
| Request shape extraction | 🔴 `missing` | — | 4968 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 4968 | — | — |
| Response shape extraction | 🔴 `missing` | — | 4968 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 4968 | — | — |
| Schema drift detection | 🔴 `missing` | — | 4968 | — | — |
| Taint sink detection | 🔴 `missing` | — | 4968 | — | — |
| Taint source detection | 🔴 `missing` | — | 4968 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 4968 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 4968 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.framework.wcf ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
