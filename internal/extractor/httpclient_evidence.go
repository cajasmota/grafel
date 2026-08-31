package extractor

import (
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Shared JS/TS source-shape helpers (#6433, hardened after review #6446)
// ---------------------------------------------------------------------------
//
// Both consumer-side HTTP client passes — the engine's receiver-style synthesis
// (internal/engine/http_endpoint_jsts_client_1483.go, which emits
// http_endpoint_call) and the cross extractor (internal/extractors/cross/
// httpclient, which emits SCOPE.ExternalEndpoint) — need the same two decisions:
//
//	"is this file test scaffolding?"      → IsTestSourceFile
//	"does this file consume an HTTP client?" → HasInjectedHTTPClientEvidence
//
// They live here, in the package both already import, so the two passes cannot
// drift apart. They had: the engine excluded test sources and the extractor did
// not, so an Angular spec file contributed a SCOPE.ExternalEndpoint and no
// http_endpoint_call — inflating precisely the number #6433 was reported on,
// with test scaffolding.

// ---------------------------------------------------------------------------
// Comment / literal stripping
// ---------------------------------------------------------------------------

// StripJSCommentsAndLiterals blanks the CONTENT of //-comments, /*…*/ comments
// and ' " ` string literals, replacing each blanked byte with a space so that
// byte offsets into the result still index the original source. Quote and
// comment delimiters are blanked too; newlines are preserved so line-anchored
// regexes keep working.
//
// This exists because a gate written as strings.Contains(src, "HttpClient") is
// opened by a MENTION, not a use. All three of these opened the old gate and
// produced a bogus call site from a plain-object member named `http` (#6446):
//
//	// TODO(migration): replace this wrapper with Angular's HttpClient.
//	import type { HttpClient } from '@angular/common/http';
//	export const DOC = 'use HttpClient instead';
//
// The first is not contrived — a legacy wrapper carrying a migration TODO is
// the most common shape in a mid-migration Angular codebase.
//
// This is a lexer approximation, not a parser: it does not track regex literals
// or JSX text. Both failure modes blank MORE than they should, which can only
// close the gate, never open it — the safe direction for a false-positive guard.
func StripJSCommentsAndLiterals(src string) string {
	out := []byte(src)
	blank := func(i int) {
		if out[i] != '\n' && out[i] != '\r' {
			out[i] = ' '
		}
	}

	const (
		code = iota
		lineComment
		blockComment
		inString
	)
	state := code
	var quote byte

	for i := 0; i < len(src); i++ {
		c := src[i]
		switch state {
		case code:
			switch {
			case c == '/' && i+1 < len(src) && src[i+1] == '/':
				state = lineComment
				blank(i)
			case c == '/' && i+1 < len(src) && src[i+1] == '*':
				state = blockComment
				blank(i)
			case c == '\'' || c == '"' || c == '`':
				state = inString
				quote = c
				blank(i)
			}
		case lineComment:
			if c == '\n' {
				state = code
			} else {
				blank(i)
			}
		case blockComment:
			blank(i)
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				blank(i + 1)
				i++
				state = code
			}
		case inString:
			// A backslash escapes the next byte, including the closing quote.
			if c == '\\' && i+1 < len(src) {
				blank(i)
				blank(i + 1)
				i++
				continue
			}
			// An unterminated literal must not swallow the rest of the file.
			if c == '\n' && quote != '`' {
				state = code
				continue
			}
			blank(i)
			if c == quote {
				state = code
			}
		}
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// "does this file consume an HTTP client?"
// ---------------------------------------------------------------------------

// httpClientInjectRe matches `inject(HttpClient)` / `@Inject(HttpService)`.
// The \b on the class name is load-bearing: without it HttpClientTestingModule
// (in every Angular spec file) would match.
var httpClientInjectRe = regexp.MustCompile(
	`\b[Ii]nject\s*\(\s*(?:HttpClient|HttpService)\s*[,)]`,
)

// httpClientAnnotationRe matches a type annotation — `constructor(private http:
// HttpClient)`, `private readonly httpService: HttpService`. Angular DI needs the
// runtime value, so an annotation of this shape is a genuine consumption signal.
var httpClientAnnotationRe = regexp.MustCompile(
	`:\s*(?:HttpClient|HttpService)\b`,
)

// httpClientImportLineRe matches an import statement naming the client class.
// Whether it counts is decided in Go, because a TYPE-ONLY import is erased at
// compile time and cannot be a DI token.
var httpClientImportLineRe = regexp.MustCompile(
	`(?m)^[^\n]*\bimport\b[^\n]*\b(?:HttpClient|HttpService)\b[^\n]*$`,
)

// httpClientTypeOnlyRe matches the two type-only import spellings:
// `import type { HttpClient } …` and `import { type HttpClient } …`.
var httpClientTypeOnlyRe = regexp.MustCompile(
	`\btype\s+(?:\{[^}\n]*\b(?:HttpClient|HttpService)\b|(?:HttpClient|HttpService)\b)`,
)

// legacyNestReceiverRe matches `this.httpService.` — the NestJS @nestjs/axios
// receiver name. Unlike `this.http`, `httpService` is specific enough to be
// evidence on its own; this clause preserves the pre-#6433 behaviour for the
// NestJS idiom that #1483 shipped.
var legacyNestReceiverRe = regexp.MustCompile(
	`\bthis\s*\.\s*httpService\s*\.`,
)

// HasInjectedHTTPClientEvidence reports whether a JS/TS source genuinely
// consumes an Angular / NestJS HTTP client, as opposed to merely mentioning one.
//
// It is the gate for the receiver-style (`this.<field>.<verb>(...)`) passes,
// which cannot key on the receiver field name: `http` is far too common a member
// name to be evidence by itself. Evidence is one of
//
//	inject(HttpClient) / @Inject(HttpService)   — DI call
//	: HttpClient / : HttpService                — type annotation (constructor DI)
//	a VALUE import naming the class             — not `import type`
//	this.httpService.                           — the NestJS receiver (#1483)
//
// all of them evaluated over the source with comments and string literals
// blanked, so a migration TODO or a doc string cannot open the gate.
func HasInjectedHTTPClientEvidence(src string) bool {
	if !strings.Contains(src, "HttpClient") &&
		!strings.Contains(src, "HttpService") &&
		!strings.Contains(src, "httpService") {
		// Cheap reject before the strip; the strip is the expensive part.
		return false
	}
	code := StripJSCommentsAndLiterals(src)

	if httpClientInjectRe.MatchString(code) ||
		httpClientAnnotationRe.MatchString(code) ||
		legacyNestReceiverRe.MatchString(code) {
		return true
	}
	for _, line := range httpClientImportLineRe.FindAllString(code, -1) {
		if !httpClientTypeOnlyRe.MatchString(line) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// "is this file test scaffolding?"
// ---------------------------------------------------------------------------

// testDirSegments are the canonical test directory names, checked as path
// segments.
var testDirSegments = []string{"/__tests__/", "/test/", "/tests/", "/spec/", "/e2e/", "/fixtures/"}

// IsTestSourceFile reports whether a path looks like test scaffolding by
// per-language naming convention. It is the shared core behind the engine's
// endpoint-synthesis exclusion (which delegates here and then adds its own
// Java / Kotlin / C# / Scala / Swift / PHP arms) and the cross HTTP-client
// extractor's exclusion, so the two can no longer disagree about a spec file.
//
// Deliberately conservative: a file that does not match is treated as
// production code even if it imports a test library. Import-based detection
// belongs to the testmap extractor, which emits SCOPE.Pattern/test_coverage
// rather than endpoints.
func IsTestSourceFile(filePath string) bool {
	// Normalise to forward slashes for cross-platform consistency.
	slashed := "/" + filepath.ToSlash(strings.ToLower(filePath))
	for _, seg := range testDirSegments {
		if strings.Contains(slashed, seg) {
			return true
		}
	}

	base := filepath.Base(filePath)
	lower := strings.ToLower(base)
	ext := filepath.Ext(lower)
	stem := strings.TrimSuffix(lower, ext)

	switch ext {
	case ".go":
		return strings.HasSuffix(stem, "_test")
	case ".py":
		return strings.HasPrefix(stem, "test_") || strings.HasSuffix(stem, "_test")
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		// foo.spec.ts, foo.test.ts, foo.e2e-spec.ts, foo.e2e.spec.ts, …
		return strings.Contains(lower, ".test.") ||
			strings.Contains(lower, ".spec.") ||
			strings.HasSuffix(stem, ".test") ||
			strings.HasSuffix(stem, ".spec") ||
			strings.HasSuffix(stem, "-spec") ||
			strings.HasSuffix(stem, "-test")
	case ".rb":
		return strings.HasSuffix(stem, "_spec")
	}
	return false
}
