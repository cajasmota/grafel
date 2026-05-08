package extractor

// ClearForTesting removes all registered extractors.
// Only for use in unit tests — do NOT call in production code.
func ClearForTesting() {
	mu.Lock()
	defer mu.Unlock()
	for k := range registry {
		delete(registry, k)
	}
}
