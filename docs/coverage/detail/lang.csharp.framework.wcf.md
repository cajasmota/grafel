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
| Schema extraction | 🟢 `partial` | — | 4968 | `internal/custom/csharp/wcf.go`<br>`internal/custom/csharp/wcf_test.go` | [DataContract] classes -> datacontract:<Name>; [DataMember] properties -> datamember entities; SCOPE.Schema/schema_extraction (#4968). #5091: [FaultContract(typeof(X))] declared SOAP faults -> fault_contract:<Fault>:<line> schema_extraction carrying fault_type; when the attribute decorates an operation, a USES edge -> operation:<Op> binds the fault to it (incremental fault-contract metadata on top of the framework-agnostic exception_flow throw/catch surface). |
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL SDL object-type graph concept; WCF data contracts are modelled under schema_extraction, no GraphQL object-type relationship graph. |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | 🟢 `partial` | — | 5091 | `internal/custom/csharp/wcf.go`<br>`internal/custom/csharp/wcf_test.go` | WCF client proxies -> SCOPE.Component/client_codegen (mirrors grpc_net.go): new ChannelFactory<IContract>(...) -> channel_factory:<Contract> with a USES edge -> contract:<Contract>; class XxxClient : ClientBase<IContract> -> client_base:<Name> + USES -> contract:<Contract>; new XxxClient(...) -> client:<Name> (common non-proxy *Client types like HttpClient excluded). #5091: channelFactory.CreateChannel() call-site attribution — a `var f = new ChannelFactory<IContract>(...)` assignment maps the factory var, and each `f.CreateChannel()` -> create_channel:<var>:<Contract> SCOPE.Component/client_codegen + USES -> contract:<Contract> (a CreateChannel on a receiver with no in-file factory assignment is skipped, honest-partial). Honest-partial: cross-file factory-var tracking and App.config client-endpoint binding sections deferred. |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | 🟢 `partial` | — | 5091 | `internal/custom/csharp/wcf.go`<br>`internal/custom/csharp/wcf_test.go` | new ServiceHost(typeof(X)) self-host + CoreWCF AddServiceModelServices()/AddServiceEndpoint<TSvc,TContract>() -> SCOPE.Pattern/transport_binding (#4968). #5091: code binding config props — new BasicHttpBinding/NetTcpBinding/WSHttpBinding/NetNamedPipeBinding/etc -> binding:<type>:<line> transport_binding carrying binding_type, endpoint_address (from a co-located new EndpointAddress("...")) and security_mode (Security.Mode = SecurityMode.X or a *SecurityMode.X ctor arg); [ServiceBehavior(InstanceContextMode=..., ConcurrencyMode=...)] -> service_behavior transport_binding with instance_context_mode/concurrency_mode; [OperationBehavior] -> operation_behavior:<Op> transport_binding. Honest-partial: address+mode are file-scoped hints attached to each binding, not per-endpoint resolved; App.config <system.serviceModel> XML binding sections deferred. |

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
| Auth coverage | 🟢 `partial` | — | 5091 | `internal/custom/csharp/wcf.go`<br>`internal/custom/csharp/wcf_test.go` | #5091: [PrincipalPermission(SecurityAction.Demand, Role=...)] declarative WCF authorization demands -> SCOPE.Pattern/auth_coverage carrying the demand argument list. Honest-partial: message-security mode on bindings and per-operation [PrincipalPermission]-to-operation binding deferred. |

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
| Tests linkage | ✅ `full` | — | 5071 | `internal/custom/csharp/test_doubles.go`<br>`internal/custom/csharp/test_doubles_test.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go` | C# NUnit/xUnit/MSTest test-attr detection is framework-agnostic; links WCF service tests. #5005 (spun out of #4968) adds the .NET test-double surface via custom_csharp_test_doubles (internal/custom/csharp/test_doubles.go), framework-agnostic across all C# frameworks: (a) MOCK-BINDING — Moq `new Mock<T>()` / `Mock.Of<T>()` and NSubstitute `Substitute.For<T>()` emit a SCOPE.Pattern/test_double node carrying library=moq|nsubstitute + target=<T> and a USES edge -> type:<T> (dotted types leaf-normalised, primitives excluded); (b) CONTAINER-TOPOLOGY — Testcontainers typed builders `new XxxContainer()` and `.WithImage("img")` chains emit SCOPE.Pattern/container_topology with a DEPENDS_ON_SERVICE edge -> service:<container-or-image> (the generic ContainerBuilder is excluded); (c) BDD STEP-DEFINITIONS — SpecFlow/Reqnroll [Binding] classes with [Given]/[When]/[Then]/[StepDefinition] emit SCOPE.Pattern/step_definition carrying keyword+step_text (gated on [Binding] so stray attrs don't fire). Reuses existing entity Kind (SCOPE.Pattern) + edge kinds (USES, DEPENDS_ON_SERVICE) — no new Kind. #5071 (follow-up) extends the same extractor with: (d) TEST-DATA BUILDERS — Bogus `new Faker<T>()` (with the faked field list harvested from the `.RuleFor(x => x.Field, …)` chain → fields=Name,Email,…) and AutoFixture `fixture.Create<T>()` / `fixture.Build<T>()` emit a SCOPE.Pattern/test_data_builder node carrying library=bogus|autofixture + target=<T> and a USES edge -> type:<T> (AutoFixture gated on a Fixture reference so stray generic Create<T>/Build<T> don't fire; primitives excluded); (e) MOCK-TARGET → DI-IMPL RESOLUTION — when a Moq mock's `.Object` is wired into production code (DI registration `services.AddSingleton(mock.Object)` or SUT ctor `new Sut(mock.Object)`), the mocked interface is resolved to its concrete impl by the dotnet_di naming convention (strip leading `I`, IOrderRepository → OrderRepository) and a RESOLVES_TO edge is emitted -> impl:<Impl> (the same node dotnet_di lands as its BINDS target), stitching the test-double surface to the production DI graph; the mock node is stamped resolved_impl=<Impl>. Resolution is gated on actual wiring (unwired mocks get no RESOLVES_TO). Value-asserting tests in test_doubles_test.go (Moq+dotted-leaf+Mock.Of, NSubstitute, Testcontainers typed+image+ContainerBuilder-exclusion, SpecFlow steps, Bogus Faker+field-harvest, AutoFixture Create+Build+Fixture-gate, mock→DI resolution via registration+SUT-ctor, unwired-no-resolution, plain-source + binding-gate + wrong-language negatives). Honest residuals: Bogus RuleFor fields are file-scoped (not per-Faker binding); mock→impl resolution is by-name only and skips custom factory registrations; NSubstitute substitutes are not yet DI-resolved (#5071). |

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
