package recipe

import (
	"fmt"
	"testing"
)

// TestBucketForUniversalDistribution proves bucketFor distributes functions
// across many buckets for a single-module image of ANY base — not collapsing
// everything into one bucket because the high address bytes are constant
// (the Windrose bug: all 495,540 funcs landed in functions/14/ because the
// /BASE:0x140000000 high byte 0x14 was the bucket key).
//
// Tested for both a UE5 0x140000000 base and a native 0x400000 base.
func TestBucketForUniversalDistribution(t *testing.T) {
	bases := []struct {
		name string
		base uint64
	}{
		{"ue5_0x140000000", 0x140000000},
		{"native_0x400000", 0x400000},
	}
	const (
		n    = 200_000
		step = 0x20 // typical tight function spacing
	)
	for _, b := range bases {
		t.Run(b.name, func(t *testing.T) {
			counts := map[string]int{}
			for i := 0; i < n; i++ {
				addr := fmt.Sprintf("%x", b.base+uint64(i)*step)
				counts[bucketFor(addr)]++
			}
			if len(counts) < 100 {
				t.Fatalf("only %d buckets for %d funcs (base collapsed addresses into too few buckets)",
					len(counts), n)
			}
			// No single bucket may hold a dominant share.
			maxBucket := 0
			for _, c := range counts {
				if c > maxBucket {
					maxBucket = c
				}
			}
			if maxBucket > n/2 {
				t.Fatalf("one bucket holds %d/%d funcs (>50%%) — not distributed", maxBucket, n)
			}
			t.Logf("%s: %d buckets, max bucket=%d (of %d)", b.name, len(counts), maxBucket, n)
		})
	}
}

// TestBucketForClustersNearby proves the scheme keeps nearby addresses grouped
// (RE navigation): addresses within the same cluster window map to the same
// bucket, while addresses far apart map to different buckets.
func TestBucketForClustersNearby(t *testing.T) {
	base := uint64(0x140001000)
	b0 := bucketFor(fmt.Sprintf("%x", base))
	bNear := bucketFor(fmt.Sprintf("%x", base+0x10)) // same 4KB page
	bFar := bucketFor(fmt.Sprintf("%x", base+0x40000))

	if b0 != bNear {
		t.Fatalf("nearby addresses split across buckets: %s vs %s (should cluster)", b0, bNear)
	}
	if b0 == bFar {
		t.Fatalf("far addresses collapsed into one bucket: both %s (should differ)", b0)
	}
}

// TestBucketForDeterministic: same address always yields the same bucket, and
// the 0x prefix is irrelevant.
func TestBucketForDeterministic(t *testing.T) {
	for _, a := range []string{"140001000", "401000", "deadbeef"} {
		first := bucketFor(a)
		for i := 0; i < 3; i++ {
			if got := bucketFor(a); got != first {
				t.Fatalf("bucketFor(%q) not deterministic: %q vs %q", a, got, first)
			}
		}
		if withPrefix := bucketFor("0x" + a); withPrefix != first {
			t.Fatalf("bucketFor 0x-prefix mismatch for %q: %q vs %q", a, withPrefix, first)
		}
	}
}
