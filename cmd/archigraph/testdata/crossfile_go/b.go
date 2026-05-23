package crossfile

// FileB hosts the entry points and chains them through every helper in
// a.go so Pass-4 community detection sees one tightly-connected
// component. Louvain emits communities at or above
// CommunityOptions.MinSize (default 5); a thin call graph (3-4 funcs)
// gets denoised into the "ungrouped" bucket and the test that checks
// Communities is non-empty would fail. Keep the graph dense.
func Greet() string {
	return Punctuate(Hello() + " " + World())
}

// GreetQuietly is a second entry point that reuses Whisper + Punctuate
// so every helper is reachable from at least two callers.
func GreetQuietly() string {
	return Punctuate(Whisper())
}

// GreetLoudly chains all four helpers to maximise edge density.
func GreetLoudly() string {
	return Punctuate(Hello() + " " + World() + " " + Whisper())
}

// MainEntry calls every other entry point so the call graph is one
// connected component, not three islands.
func MainEntry() string {
	return Greet() + "/" + GreetQuietly() + "/" + GreetLoudly()
}
