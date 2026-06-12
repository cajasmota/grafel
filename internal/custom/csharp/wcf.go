// Package csharp — WCF (Windows Communication Foundation) extractor for C#.
//
// Covers the service-contract surface for lang.csharp.framework.wcf (issue
// #4968). WCF models RPC the way gRPC-net does: a [ServiceContract] interface
// declares the service, each [OperationContract] method is an invokable
// procedure, [DataContract]/[DataMember] types describe the payload schema, and
// ServiceHost / AddServiceModelServices registrations bind the service to a
// transport. We mirror the grpc_net.go shape so the two RPC frameworks read the
// same in the graph.
//
//	Schema/procedure_extraction:
//	  [OperationContract] methods inside a [ServiceContract] interface emitted
//	  as SCOPE.Schema/procedure_extraction (one per operation). [ServiceContract]
//	  interfaces/classes emitted as the owning service procedure surface.
//
//	Schema/schema_extraction:
//	  [DataContract] C# classes and their [DataMember] properties emitted as
//	  SCOPE.Schema/schema_extraction.
//
//	Transport/transport_binding:
//	  new ServiceHost(typeof(X)) host registration and
//	  builder.Services.AddServiceModelServices()/AddServiceModelWebServices()
//	  (CoreWCF) emitted as SCOPE.Pattern/transport_binding.
//
// Registration key: "custom_csharp_wcf"
// Issue #4968.
package csharp

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_csharp_wcf", &wcfExtractor{})
}

type wcfExtractor struct{}

func (e *wcfExtractor) Language() string { return "custom_csharp_wcf" }

// ---------------------------------------------------------------------------
// Regex catalog
// ---------------------------------------------------------------------------

var (
	// [ServiceContract] on an interface or class — declares a WCF service.
	// Captures the type name (the leading I on interfaces is part of the name).
	reWCFServiceContract = regexp.MustCompile(
		`\[ServiceContract\b[^\]]*\]\s*(?:public\s+|internal\s+)?(?:partial\s+)?(?:interface|class)\s+(\w+)`,
	)

	// [OperationContract] on a method — one invokable RPC operation. Captures
	// the method name. The return type may be a generic (Task<T>) so we skip it.
	reWCFOperationContract = regexp.MustCompile(
		`\[OperationContract\b[^\]]*\]\s*(?:public\s+|internal\s+)?(?:async\s+)?[\w.<>\[\]]+(?:\s*<[^>]+>)?\s+(\w+)\s*\(`,
	)

	// [DataContract] C# class — WCF payload schema type.
	reWCFDataContract = regexp.MustCompile(
		`\[DataContract\b[^\]]*\]\s*(?:public\s+|internal\s+)?(?:partial\s+)?class\s+(\w+)`,
	)

	// [DataMember] property — a serialized member of a data contract.
	reWCFDataMember = regexp.MustCompile(
		`\[DataMember\b[^\]]*\]\s*(?:public\s+)?[\w.<>\[\]]+(?:\s*<[^>]+>)?\s+(\w+)\s*(?:\{|;|=)`,
	)

	// new ServiceHost(typeof(MyService)) — self-hosted WCF endpoint binding.
	reWCFServiceHost = regexp.MustCompile(
		`new\s+ServiceHost\s*\(\s*typeof\s*\(\s*(\w+)\s*\)`,
	)

	// CoreWCF registration: AddServiceModelServices / AddServiceModelWebServices.
	reWCFAddServiceModel = regexp.MustCompile(
		`\.AddServiceModel(?:Web)?Services\s*\(`,
	)

	// CoreWCF endpoint wiring: builder.AddServiceEndpoint<TService, TContract>()
	// or serviceBuilder.AddServiceEndpoint(...). Captures the contract type when
	// expressed generically.
	reWCFAddServiceEndpoint = regexp.MustCompile(
		`\.AddServiceEndpoint\s*<\s*(\w+)\s*,\s*(\w+)\s*>`,
	)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *wcfExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.wcf_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "wcf"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "csharp" {
		return nil, nil
	}

	src := string(file.Content)

	// Cheap gate: only WCF files carry these attributes / host types.
	if !regexpAny(src, "[ServiceContract", "[OperationContract", "[DataContract",
		"ServiceHost", "AddServiceModel") {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// -------------------------------------------------------------------------
	// Schema/procedure_extraction — the service + its operations
	// -------------------------------------------------------------------------

	// [ServiceContract] interfaces/classes
	for _, m := range reWCFServiceContract.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("service:"+name, "SCOPE.Schema", "procedure_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "wcf", "provenance", "INFERRED_FROM_SERVICE_CONTRACT",
			"service_name", name)
		add(ent)
	}

	// [OperationContract] methods
	for _, m := range reWCFOperationContract.FindAllStringSubmatchIndex(src, -1) {
		opName := src[m[2]:m[3]]
		ent := makeEntity("operation:"+opName, "SCOPE.Schema", "procedure_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "wcf", "provenance", "INFERRED_FROM_OPERATION_CONTRACT",
			"operation_name", opName)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Schema/schema_extraction — data contracts + members
	// -------------------------------------------------------------------------

	// [DataContract] classes
	for _, m := range reWCFDataContract.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity("datacontract:"+name, "SCOPE.Schema", "schema_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "wcf", "provenance", "INFERRED_FROM_DATA_CONTRACT",
			"class_name", name)
		add(ent)
	}

	// [DataMember] properties
	for _, m := range reWCFDataMember.FindAllStringSubmatchIndex(src, -1) {
		field := src[m[2]:m[3]]
		ent := makeEntity("datamember:"+field+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Schema", "schema_extraction", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "wcf", "provenance", "INFERRED_FROM_DATA_MEMBER",
			"field_name", field)
		add(ent)
	}

	// -------------------------------------------------------------------------
	// Transport/transport_binding — host + CoreWCF registration
	// -------------------------------------------------------------------------

	// new ServiceHost(typeof(X)) — self-hosted endpoint
	for _, m := range reWCFServiceHost.FindAllStringSubmatchIndex(src, -1) {
		svc := src[m[2]:m[3]]
		ent := makeEntity("service_host:"+svc, "SCOPE.Pattern", "transport_binding", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "wcf", "provenance", "INFERRED_FROM_SERVICE_HOST",
			"service_type", svc)
		add(ent)
	}

	// builder.AddServiceEndpoint<TService, TContract>() — CoreWCF endpoint
	for _, m := range reWCFAddServiceEndpoint.FindAllStringSubmatchIndex(src, -1) {
		svc := src[m[2]:m[3]]
		contract := src[m[4]:m[5]]
		ent := makeEntity("service_endpoint:"+svc+":"+contract, "SCOPE.Pattern", "transport_binding", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "wcf", "provenance", "INFERRED_FROM_ADD_SERVICE_ENDPOINT",
			"service_type", svc, "contract_type", contract)
		add(ent)
	}

	// .AddServiceModelServices() / .AddServiceModelWebServices() — CoreWCF wiring
	for _, m := range reWCFAddServiceModel.FindAllStringIndex(src, -1) {
		ent := makeEntity("add_service_model:"+file.Path+":"+itoa(lineOf(src, m[0])),
			"SCOPE.Pattern", "transport_binding", file.Path, "csharp", lineOf(src, m[0]))
		setProps(&ent, "framework", "wcf", "provenance", "INFERRED_FROM_ADD_SERVICE_MODEL")
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// regexpAny reports whether src contains any of the literal substrings. Used as
// a cheap pre-filter before running the WCF regex catalog.
func regexpAny(src string, subs ...string) bool {
	for _, s := range subs {
		if strings.Contains(src, s) {
			return true
		}
	}
	return false
}
