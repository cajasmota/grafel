package crossfile

// FileA defines functions used by file b.go in the same package.
func Hello() string {
	return "hi"
}

// World is called by Greet alongside Hello to ensure Pass-4 sees a
// non-trivial call graph. Louvain community detection only emits
// communities at or above CommunityOptions.MinSize (default 5), so
// the fixture must contribute enough nodes to clear that floor.
func World() string {
	return "world"
}

// Punctuate caps the greeting; called by Greet.
func Punctuate(s string) string {
	return s + "!"
}

// Whisper is a low-volume variant called by GreetQuietly.
func Whisper() string {
	return "shh"
}
