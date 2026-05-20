package links

import (
	"testing"
)

func TestNormalizeLabel_BuiltinStopList(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		reason   string
	}{
		// Newly added 2-3 char common identifiers
		{input: "buf", expected: "", reason: "buf is in builtinLabelStopList"},
		{input: "ctx", expected: "", reason: "ctx is in builtinLabelStopList"},
		{input: "err", expected: "", reason: "err is in builtinLabelStopList"},
		{input: "fd", expected: "", reason: "fd is in builtinLabelStopList"},
		{input: "fs", expected: "", reason: "fs is in builtinLabelStopList"},
		{input: "req", expected: "", reason: "req is in builtinLabelStopList"},
		{input: "res", expected: "", reason: "res is in builtinLabelStopList"},
		{input: "xhr", expected: "", reason: "xhr is in builtinLabelStopList"},
		{input: "ch", expected: "", reason: "ch is in builtinLabelStopList"},
		{input: "ok", expected: "", reason: "ok is in builtinLabelStopList"},
		// Existing single letters
		{input: "a", expected: "", reason: "a is single-letter"},
		{input: "z", expected: "", reason: "z is single-letter"},
		// Existing common methods
		{input: "map", expected: "", reason: "map is JS Array method"},
		{input: "filter", expected: "", reason: "filter is JS Array method"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeLabel(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeLabel(%q) = %q, want %q (%s)", tt.input, result, tt.expected, tt.reason)
			}
		})
	}
}

func TestNormalizeLabel_GenericFieldStopList(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		reason   string
	}{
		// Newly added generic field names
		{input: "socket", expected: "", reason: "socket is in genericFieldStopList"},
		{input: "csv", expected: "", reason: "csv is in genericFieldStopList"},
		{input: "inactive", expected: "", reason: "inactive is in genericFieldStopList"},
		{input: "pending", expected: "", reason: "pending is in genericFieldStopList"},
		{input: "footer", expected: "", reason: "footer is in genericFieldStopList"},
		// Existing generic field names
		{input: "body", expected: "", reason: "body is in genericFieldStopList"},
		{input: "content", expected: "", reason: "content is in genericFieldStopList"},
		{input: "data", expected: "", reason: "data is in genericFieldStopList"},
		{input: "error", expected: "", reason: "error is in genericFieldStopList"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeLabel(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeLabel(%q) = %q, want %q (%s)", tt.input, result, tt.expected, tt.reason)
			}
		})
	}
}

func TestNormalizeLabel_DestructuredPatterns(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		reason   string
	}{
		// React useState destructure patterns
		{input: "[error, setError]", expected: "", reason: "useState tuple pattern filtered"},
		{input: "[value, setValue]", expected: "", reason: "useState tuple pattern filtered"},
		{input: "[count, setCount]", expected: "", reason: "useState tuple pattern filtered"},
		{input: "[user, setUser]", expected: "", reason: "useState tuple pattern filtered"},
		// Inline object destructure patterns
		{input: "{ data }", expected: "", reason: "object destructure pattern filtered"},
		{input: "{ id }", expected: "", reason: "object destructure pattern filtered"},
		{input: "{ url, fields }", expected: "", reason: "object destructure pattern filtered"},
		{input: "{ name, email }", expected: "", reason: "object destructure pattern filtered"},
		// Inline array destructure patterns (non-setState form)
		{input: "[year, month, day]", expected: "", reason: "array destructure pattern filtered"},
		{input: "[first, second, third]", expected: "", reason: "array destructure pattern filtered"},
		// Non-destructure patterns should pass through (but may fail length filter)
		{input: "[error]", expected: "", reason: "single-element array still filtered by length"},
		{input: "{ x }", expected: "", reason: "single-element object still filtered by length"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeLabel(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeLabel(%q) = %q, want %q (%s)", tt.input, result, tt.expected, tt.reason)
			}
		})
	}
}

func TestNormalizeLabel_LengthFilter(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		reason   string
	}{
		// Labels ≤3 alpha chars after stripping leading non-alpha
		{input: "_ab", expected: "", reason: "2 alpha chars after strip"},
		{input: "123abc", expected: "", reason: "3 alpha chars after strip"},
		{input: "!id", expected: "", reason: "2 alpha chars after strip"},
		// Labels >3 alpha chars pass the length filter
		{input: "_abcd", expected: "_abcd", reason: "4 alpha chars after strip passes length"},
		{input: "custom", expected: "custom", reason: "6 alpha chars passes length"},
		{input: "identifier123", expected: "identifier123", reason: "10 alpha chars passes length (goes through isHardenedNoise)"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeLabel(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeLabel(%q) = %q, want %q (%s)", tt.input, result, tt.expected, tt.reason)
			}
		})
	}
}

func TestNormalizeLabel_LineNumberSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		reason   string
	}{
		// Line number suffixes (from #511)
		{input: "error_handling:110", expected: "", reason: "line number suffix filtered"},
		{input: "route:42", expected: "", reason: "line number suffix filtered"},
		{input: "function:1000", expected: "", reason: "line number suffix filtered"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeLabel(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeLabel(%q) = %q, want %q (%s)", tt.input, result, tt.expected, tt.reason)
			}
		})
	}
}

func TestNormalizeLabel_ValidArchitecturalSignal(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		reason   string
	}{
		// Valid architectural identifiers that pass through all filters
		{input: "createUser", expected: "createuser", reason: "valid architectural name"},
		{input: "PaymentProcessor", expected: "paymentprocessor", reason: "valid architectural name"},
		{input: "AuthenticationService", expected: "authentication", reason: "Service suffix stripped, architectural name"},
		{input: "ConnectionPool", expected: "connectionpool", reason: "valid architectural name"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeLabel(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeLabel(%q) = %q, want %q (%s)", tt.input, result, tt.expected, tt.reason)
			}
		})
	}
}
