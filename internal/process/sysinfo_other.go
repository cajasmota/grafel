//go:build !linux && !darwin

package process

// TotalMemoryMB returns 0 on unsupported platforms.
// Callers that use this to compute a budget default should fall back to
// their hard-coded safe value when 0 is returned.
func TotalMemoryMB() int64 {
	return 0
}

// AvailableMemoryMB returns 0 on unsupported platforms — "unknown", not zero
// available memory. Callers must fall back to a safe default (#5954).
func AvailableMemoryMB() int64 {
	return 0
}
