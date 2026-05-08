package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// mockLibraryExtractor detects mock library usage in tests.
// Matches Python mock_library_extractor.py.
type mockLibraryExtractor struct{}

var mockImportTokens = []string{
	"mockito", "unittest.mock", "@patch", "jest.mock", "sinon",
	"testdouble", "rspec-mocks", "gomock", "moq",
}

var (
	mockMockitoMockRE    = regexp.MustCompile(`\bMockito\.mock\s*\(\s*(\w+)\.class\s*\)`)
	mockMockitoAnnotRE   = regexp.MustCompile(`@Mock\s+(\w+)\b`)
	mockPatchStrRE       = regexp.MustCompile(`@patch\s*\(\s*["']([^"']+)["']`)
	mockPatchObjectRE    = regexp.MustCompile(`@patch\.object\s*\(\s*(\w+)\s*,`)
	mockMockSpecRE       = regexp.MustCompile(`\b(?:Mock|MagicMock)\s*\(\s*spec\s*=\s*(\w+)`)
	mockRSpecDoubleRE    = regexp.MustCompile(`\bdouble\s*\(\s*["'](\w+)["']`)
mockJestSpyOnRE      = regexp.MustCompile(`\bjest\.spyOn\s*\(\s*(\w+)\s*,\s*["'](\w+)["']`)
	mockJestMockRE       = regexp.MustCompile(`\bjest\.mock\s*\(\s*["']([^"']+)["']`)
	mockGoMockControlRE  = regexp.MustCompile(`gomock\.NewController\s*\(`)
)

func (m *mockLibraryExtractor) Category() string { return "mock_library" }

func (m *mockLibraryExtractor) AppliesTo(src string) bool {
	srcLower := strings.ToLower(src)
	for _, tok := range mockImportTokens {
		if strings.Contains(srcLower, strings.ToLower(tok)) {
			return true
		}
	}
	return false
}

func (m *mockLibraryExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}
	idx := 0

	emit := func(target, library string, line int) {
		key := fmt.Sprintf("%s:%s", library, target)
		if seen[key] {
			return
		}
		seen[key] = true
		idx++
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("mock_%s_%d", library, idx),
			"SCOPE.Pattern", "mock", language, line,
			map[string]string{"kind": "mock_library", "library": library, "target": target}))
	}

	for _, match := range mockMockitoMockRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[match[2]:match[3]], "mockito", lineOf(src, match[0]))
	}
	for _, match := range mockMockitoAnnotRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[match[2]:match[3]], "mockito", lineOf(src, match[0]))
	}
	for _, match := range mockPatchStrRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[match[2]:match[3]], "unittest.mock", lineOf(src, match[0]))
	}
	for _, match := range mockPatchObjectRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[match[2]:match[3]], "unittest.mock", lineOf(src, match[0]))
	}
	for _, match := range mockMockSpecRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[match[2]:match[3]], "unittest.mock", lineOf(src, match[0]))
	}
	for _, match := range mockRSpecDoubleRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[match[2]:match[3]], "rspec-mocks", lineOf(src, match[0]))
	}
	for _, match := range mockJestSpyOnRE.FindAllStringSubmatchIndex(src, -1) {
		obj := src[match[2]:match[3]]
		method := src[match[4]:match[5]]
		emit(obj+"."+method, "jest", lineOf(src, match[0]))
	}
	for _, match := range mockJestMockRE.FindAllStringSubmatchIndex(src, -1) {
		emit(src[match[2]:match[3]], "jest", lineOf(src, match[0]))
	}
	if mockGoMockControlRE.MatchString(src) {
		emit("controller", "gomock", 1)
	}

	return results
}

func init() {
	Register(&mockLibraryExtractor{})
}
