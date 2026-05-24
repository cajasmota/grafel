package worktree

// ParseWorktreeListForTest exposes the package-internal parseWorktreeList
// function for white-box unit tests in the _test package.
func ParseWorktreeListForTest(s string) []RawWorktree {
	return parseWorktreeList(s)
}
