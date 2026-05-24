//go:build !linux && !darwin

package process

// TotalMemoryMB returns 0 on unsupported platforms.
// Callers that use this to compute a budget default should fall back to
// their hard-coded safe value when 0 is returned.
func TotalMemoryMB() int64 {
	return 0
}
