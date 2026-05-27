package engine

// Test fixture only — these synthesizer signatures exercise the
// discover's synthesizerGrep walker. The bodies are intentionally
// empty so the file compiles when grepped by line.

func synthesizeFlask(content string, emit func()) {}

func synthesizeQuart(content string, emit func()) {}

// Unresolved synthesizer (no YAML rule): should land as synth.unknownfw.
func synthesizeUnknownfw(content string, emit func()) {}
