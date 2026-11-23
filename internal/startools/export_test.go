package startools

// SetMemLimit sets the memory brake for a test and gives back the old one.
func SetMemLimit(n uint64) (old uint64) {
	old, memLimit = memLimit, n
	return old
}

// MemSample is how often a call looks at the memory.
const MemSample = memSample
