package extractor

import "syscall"

// limitAddressSpace caps the process's virtual memory, so the kernel fails
// allocations past it instead of the pod running out of memory.
func limitAddressSpace(bytes uint64) error {
	return syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{Cur: bytes, Max: bytes})
}
