package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// grpcImplDetector detects proto service implementations and emits IMPLEMENTS relationships.
// Matches Python grpc_impl_detector.py.
type grpcImplDetector struct{}

var (
	grpcGoUnimplementedRE  = regexp.MustCompile(`\bUnimplemented([A-Z][A-Za-z0-9_]*)Server\b`)
	grpcJavaAnnotationRE   = regexp.MustCompile(`@GrpcService\b`)
	grpcJavaImplBaseRE     = regexp.MustCompile(`\b([A-Z][A-Za-z0-9_]*)Grpc\.[A-Z][A-Za-z0-9_]*ImplBase\b`)
	grpcPythonServicerRE   = regexp.MustCompile(`\b([A-Z][A-Za-z0-9_]*)Servicer\b`)
)

var grpcProtoImportTokens = []string{
	"_pb2_grpc", "grpc", "google.golang.org/grpc",
	`pb"`, "io.grpc", "net.devh.boot.grpc",
}

func (g *grpcImplDetector) Category() string { return "grpc_impl" }

func (g *grpcImplDetector) AppliesTo(src string) bool {
	for _, tok := range grpcProtoImportTokens {
		if strings.Contains(src, tok) {
			return true
		}
	}
	return false
}

func (g *grpcImplDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Go: Unimplemented<ServiceName>Server embedding
	for _, m := range grpcGoUnimplementedRE.FindAllStringSubmatchIndex(src, -1) {
		svcName := src[m[2]:m[3]]
		key := "go:" + svcName
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"grpc_impl_"+svcName, "SCOPE.Service", "grpc_impl", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "grpc_impl", "service_name": svcName, "language": "go"}))
	}

	// Java: @GrpcService annotation
	for _, m := range grpcJavaAnnotationRE.FindAllStringIndex(src, -1) {
		key := "java:grpc_service"
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"grpc_impl_java_service", "SCOPE.Service", "grpc_impl", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "grpc_impl", "annotation": "@GrpcService", "language": "java"}))
	}

	// Java: extends <ServiceName>Grpc.<ServiceName>ImplBase
	for _, m := range grpcJavaImplBaseRE.FindAllStringSubmatchIndex(src, -1) {
		svcName := src[m[2]:m[3]]
		key := "java:implbase:" + svcName
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"grpc_impl_"+svcName, "SCOPE.Service", "grpc_impl", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "grpc_impl", "service_name": svcName, "language": "java"}))
	}

	// Python: class extending <ServiceName>Servicer
	for _, m := range grpcPythonServicerRE.FindAllStringSubmatchIndex(src, -1) {
		svcName := src[m[2]:m[3]]
		key := "python:servicer:" + svcName
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"grpc_impl_"+svcName, "SCOPE.Service", "grpc_impl", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "grpc_impl", "service_name": svcName, "language": "python"}))
	}

	return results
}

func init() {
	Register(&grpcImplDetector{})
}
