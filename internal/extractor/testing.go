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

// SnapshotForTesting captures the current registry state and returns a restore
// function that reinstates it. Pair with t.Cleanup to guarantee restoration even
// on test failure:
//
//	t.Cleanup(extractor.SnapshotForTesting())
//
// Only for use in unit tests — do NOT call in production code.
func SnapshotForTesting() func() {
	mu.RLock()
	snap := make(map[string]Extractor, len(registry))
	for k, v := range registry {
		snap[k] = v
	}
	mu.RUnlock()

	return func() {
		mu.Lock()
		defer mu.Unlock()
		for k := range registry {
			delete(registry, k)
		}
		for k, v := range snap {
			registry[k] = v
		}
	}
}
