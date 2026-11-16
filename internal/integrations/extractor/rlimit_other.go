//go:build !linux

package extractor

// limitAddressSpace is a no-op outside Linux; the soft Go heap limit and the
// timeout still apply.
func limitAddressSpace(uint64) error { return nil }
