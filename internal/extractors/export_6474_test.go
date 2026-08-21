package extractors

// SwapPassStartCommitHook replaces the pass-start commit seam and returns a
// restore func. It exists only for the external extractors_test package, which
// holds this package's TryIncremental fixtures but cannot reach an unexported
// var (#6474; same shape as SwapWriteGraphGen, #6212).
func SwapPassStartCommitHook(fn func(short, full string)) (restore func()) {
	prev := passStartCommitHook
	passStartCommitHook = fn
	return func() { passStartCommitHook = prev }
}
