package cpp

// protobuf.go — Protocol Buffers extractor for C++ projects.
//
// Two surfaces are covered:
//
//  1. .proto IDL files (Language=="proto"): the message / enum / field / service
//     / rpc declarations are the source of truth for the DTO + service shapes.
//
//	    message HelloRequest {
//	      string name = 1;
//	      int32  count = 2;
//	    }
//	    service Greeter {
//	      rpc SayHello (HelloRequest) returns (HelloReply);
//	    }
//
//  2. protoc-generated C++ headers (Language=="cpp"): the generated message
//     classes subclass ::google::protobuf::Message. We recover the DTO type
//     name from those classes:
//
//	    class HelloRequest final : public ::google::protobuf::Message { ... };
//
// .proto parsing yields full message + field shapes (schema_extraction full);
// generated-header parsing yields the message type names only (the per-field
// accessors are present but field types require deeper parsing), so the cpp
// side is honest-partial.

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
	extractor.Register("custom_cpp_protobuf", &cppProtobufExtractor{})
}

type cppProtobufExtractor struct{}

func (e *cppProtobufExtractor) Language() string { return "custom_cpp_protobuf" }

var (
	// .proto: message Foo {
	reProtoMessage = regexp.MustCompile(`(?m)^\s*message\s+([A-Za-z_]\w*)\s*\{`)
	// .proto: enum Foo {
	reProtoEnum = regexp.MustCompile(`(?m)^\s*enum\s+([A-Za-z_]\w*)\s*\{`)
	// .proto field: [repeated|optional|required] <type> name = <num>;
	// Captures: 1=label(optional), 2=type, 3=name, 4=field number.
	reProtoField = regexp.MustCompile(
		`(?m)^\s*(repeated\s+|optional\s+|required\s+)?([A-Za-z_][\w.]*(?:<[^>]*>)?)\s+([A-Za-z_]\w*)\s*=\s*(\d+)\s*;`,
	)
	// .proto: service Foo {
	reProtoService = regexp.MustCompile(`(?m)^\s*service\s+([A-Za-z_]\w*)\s*\{`)
	// .proto rpc: rpc Method (stream? Req) returns (stream? Resp);
	// Captures: 1=method, 2=req-stream, 3=req-type, 4=resp-stream, 5=resp-type.
	reProtoRpc = regexp.MustCompile(
		`(?m)\brpc\s+([A-Za-z_]\w*)\s*\(\s*(stream\s+)?([A-Za-z_][\w.]*)\s*\)\s*returns\s*\(\s*(stream\s+)?([A-Za-z_][\w.]*)\s*\)`,
	)

	// Generated C++ header: class Foo final : public ::google::protobuf::Message
	// Also tolerates MessageLite. Capture group 1 = message class name.
	reProtoGenMessage = regexp.MustCompile(
		`(?m)\bclass\s+(?:PROTOBUF_[A-Z_]+\s+)?([A-Za-z_]\w*)\b[^{;]*?:\s*(?:public\s+)?::?google::protobuf::(?:Message|MessageLite)\b`,
	)
)

func (e *cppProtobufExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.cpp_protobuf.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "protobuf"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)

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

	switch {
	case file.Language == "proto" || file.Language == "protobuf" || strings.HasSuffix(file.Path, ".proto"):
		e.extractProto(src, file, add)
	case file.Language == "cpp":
		// Only generated protobuf headers; gate on the protobuf base class.
		if !strings.Contains(src, "google::protobuf") {
			return nil, nil
		}
		e.extractGenerated(src, file, add)
	default:
		return nil, nil
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// extractProto parses a .proto IDL file: messages (+fields), enums, services
// (+rpcs). Messages become SCOPE.Schema DTOs; fields become SCOPE.Schema/field
// children; each rpc becomes a SCOPE.Operation/endpoint with the canonical
// gRPC path.
func (e *cppProtobufExtractor) extractProto(src string, file extractor.FileInput, add func(types.EntityRecord)) {
	// Messages + their fields (field list is the brace-balanced body).
	for _, m := range reProtoMessage.FindAllStringSubmatchIndex(src, -1) {
		msgName := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		ent := makeEntity("proto_message:"+msgName, "SCOPE.Schema", "dto", file.Path, "proto", line)
		setProps(&ent, "framework", "protobuf",
			"provenance", "INFERRED_FROM_PROTO_MESSAGE",
			"dto_name", msgName, "message_kind", "proto_message")
		add(ent)

		bodyStart, bodyEnd := cppBraceBody(src, m[1]-1)
		if bodyStart < 0 {
			continue
		}
		body := src[bodyStart:bodyEnd]
		for _, fm := range reProtoField.FindAllStringSubmatchIndex(body, -1) {
			label := ""
			if fm[2] >= 0 {
				label = strings.TrimSpace(body[fm[2]:fm[3]])
			}
			ftype := body[fm[4]:fm[5]]
			fname := body[fm[6]:fm[7]]
			fnum := body[fm[8]:fm[9]]
			// Skip 'returns'/'rpc' false-positives — proto field types are not keywords.
			fldEnt := makeEntity(msgName+"."+fname, "SCOPE.Schema", "field", file.Path, "proto", line+lineOf(body, fm[0])-1)
			setProps(&fldEnt, "framework", "protobuf",
				"provenance", "INFERRED_FROM_PROTO_FIELD",
				"parent_message", msgName,
				"field_name", fname, "field_type", ftype,
				"field_number", fnum)
			if label != "" {
				setProps(&fldEnt, "field_label", label)
			}
			add(fldEnt)
		}
	}

	// Enums.
	for _, m := range reProtoEnum.FindAllStringSubmatchIndex(src, -1) {
		enumName := src[m[2]:m[3]]
		ent := makeEntity("proto_enum:"+enumName, "SCOPE.Schema", "enum", file.Path, "proto", lineOf(src, m[0]))
		setProps(&ent, "framework", "protobuf",
			"provenance", "INFERRED_FROM_PROTO_ENUM",
			"enum_name", enumName)
		add(ent)
	}

	// Services + rpcs.
	for _, m := range reProtoService.FindAllStringSubmatchIndex(src, -1) {
		svcName := src[m[2]:m[3]]
		svcLine := lineOf(src, m[0])
		svcEnt := makeEntity("grpc_service:"+svcName, "SCOPE.Service", "grpc_service", file.Path, "proto", svcLine)
		setProps(&svcEnt, "framework", "protobuf",
			"provenance", "INFERRED_FROM_PROTO_SERVICE",
			"grpc_service", svcName, "rpc_protocol", "grpc")
		add(svcEnt)

		bodyStart, bodyEnd := cppBraceBody(src, m[1]-1)
		if bodyStart < 0 {
			continue
		}
		body := src[bodyStart:bodyEnd]
		for _, rm := range reProtoRpc.FindAllStringSubmatchIndex(body, -1) {
			method := body[rm[2]:rm[3]]
			reqStream := rm[4] >= 0 && body[rm[4]:rm[5]] != ""
			reqType := cppGrpcLastSegment(body[rm[6]:rm[7]])
			respStream := rm[8] >= 0 && body[rm[8]:rm[9]] != ""
			respType := cppGrpcLastSegment(body[rm[10]:rm[11]])
			line := svcLine + lineOf(body, rm[0]) - 1

			path := "/" + svcName + "/" + method
			ent := makeEntity("RPC "+path, "SCOPE.Operation", "endpoint", file.Path, "proto", line)
			setProps(&ent, "framework", "protobuf",
				"provenance", "INFERRED_FROM_PROTO_RPC",
				"http_method", "RPC", "verb", "RPC",
				"route_path", path, "rpc_protocol", "grpc",
				"grpc_service", svcName, "grpc_method", method,
				"request_message", reqType, "response_message", respType)
			streamKind := protoStreamKind(reqStream, respStream)
			if streamKind != "" {
				setProps(&ent, "streaming", streamKind)
			}
			add(ent)
		}
	}
}

// protoStreamKind maps the (req,resp) stream flags to a canonical label.
func protoStreamKind(reqStream, respStream bool) string {
	switch {
	case reqStream && respStream:
		return "bidi_streaming"
	case respStream:
		return "server_streaming"
	case reqStream:
		return "client_streaming"
	}
	return ""
}

// extractGenerated parses a protoc-generated C++ header for the message classes
// that subclass google::protobuf::Message. Only the DTO type name is recovered
// (honest-partial; field shapes live in the accessor bodies).
func (e *cppProtobufExtractor) extractGenerated(src string, file extractor.FileInput, add func(types.EntityRecord)) {
	for _, m := range reProtoGenMessage.FindAllStringSubmatchIndex(src, -1) {
		msgName := src[m[2]:m[3]]
		ent := makeEntity("proto_message:"+msgName, "SCOPE.Schema", "dto", file.Path, "cpp", lineOf(src, m[0]))
		setProps(&ent, "framework", "protobuf",
			"provenance", "INFERRED_FROM_PROTOBUF_GENERATED",
			"dto_name", msgName, "message_kind", "generated_message")
		add(ent)
	}
}
