//go:build !linux || race

package extractor

// limitAddressSpace is a no-op outside Linux, and in race-detector builds,
// whose runtime reserves far more address space than any sane limit. The
// soft Go heap limit and the timeout still apply.
func limitAddressSpace(uint64) error { return nil }
