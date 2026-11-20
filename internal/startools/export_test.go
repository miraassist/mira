package startools

// SetMemLimit sets the memory brake for a test and gives back the old one.
func SetMemLimit(n uint64) (old uint64) {
	old, memLimit = memLimit, n
	return old
}
