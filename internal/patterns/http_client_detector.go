package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// httpClientDetector detects HTTP client call patterns.
// Matches Python http_client_detector.py.
type httpClientDetector struct{}

var httpClientImportTokens = []string{
	"requests", "httpx", "aiohttp", "urllib",
	"axios", "fetch", "got", "node-fetch",
	"net/http", "httpclient",
	"OkHttpClient", "RestTemplate", "WebClient", "Retrofit",
	"HttpClient", "Faraday", "HTTParty",
}

var (
	hcURLRE        = regexp.MustCompile(`["'](https?://([^/"'\s]+)[^"']*)["']`)
	hcPyRequestsRE = regexp.MustCompile(`\brequests\s*\.\s*(get|post|put|patch|delete|request)\s*\(`)
	hcTSFetchRE    = regexp.MustCompile(`\bfetch\s*\(`)
	hcTSAxiosRE    = regexp.MustCompile(`\baxios\s*\.\s*(?:get|post|put|patch|delete|request)\s*\(`)
	hcGoHTTPRE     = regexp.MustCompile(`\bhttp\s*\.\s*(?:Get|Post|PostForm|NewRequest|Do)\s*\(`)
	hcJavaHTTPRE   = regexp.MustCompile(`\b(?:OkHttpClient|HttpClient|RestTemplate|WebClient|Retrofit)\b`)
hcBoto3ClientRE = regexp.MustCompile(`boto3\s*\.\s*(?:client|resource)\s*\(\s*["']([^"']+)["']`)
	hcGRPCStubRE   = regexp.MustCompile(`\b\w+Stub\s*\(\s*\w+Channel\s*\)`)
)

func (h *httpClientDetector) Category() string { return "http_client" }

func (h *httpClientDetector) AppliesTo(src string) bool {
	srcLower := strings.ToLower(src)
	for _, tok := range httpClientImportTokens {
		if strings.Contains(srcLower, strings.ToLower(tok)) {
			return true
		}
	}
	return false
}

func (h *httpClientDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, client, url string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Operation", "http_client", language, line,
			map[string]string{
				"kind":          "http_client",
				"client_library": client,
				"url":           url,
			}))
	}

	// Collect URLs
	for idx, m := range hcURLRE.FindAllStringSubmatchIndex(src, -1) {
		url := src[m[2]:m[3]]
		host := src[m[4]:m[5]]
		key := fmt.Sprintf("url:%s:%d", host, idx)
		emit(key, fmt.Sprintf("http_call_%s_%d", host, idx), "http", url, lineOf(src, m[0]))
		if idx >= 19 {
			break
		}
	}

	// Python requests
	if m := hcPyRequestsRE.FindStringIndex(src); m != nil {
		emit("py:requests", "http_client_requests", "requests", "", lineOf(src, m[0]))
	}

	// JS fetch
	if m := hcTSFetchRE.FindStringIndex(src); m != nil {
		emit("js:fetch", "http_client_fetch", "fetch", "", lineOf(src, m[0]))
	}

	// JS axios
	if m := hcTSAxiosRE.FindStringIndex(src); m != nil {
		emit("js:axios", "http_client_axios", "axios", "", lineOf(src, m[0]))
	}

	// Go net/http
	if m := hcGoHTTPRE.FindStringIndex(src); m != nil {
		emit("go:net_http", "http_client_go", "net/http", "", lineOf(src, m[0]))
	}

	// Java
	if m := hcJavaHTTPRE.FindStringIndex(src); m != nil {
		emit("java:http_client", "http_client_java", "java_http", "", lineOf(src, m[0]))
	}

	// boto3 client
	for _, m := range hcBoto3ClientRE.FindAllStringSubmatchIndex(src, -1) {
		svc := src[m[2]:m[3]]
		emit("boto3:"+svc, "boto3_client_"+svc, "boto3", "aws://"+svc, lineOf(src, m[0]))
	}

	// gRPC stub
	if m := hcGRPCStubRE.FindStringIndex(src); m != nil {
		emit("grpc:stub", "grpc_stub_call", "grpc", "", lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&httpClientDetector{})
}
