package slrucache

import (
	"fmt"
	"math/rand"
	"strconv"
	"testing"
)

// The generic SLRUCache uses type parameters for keys and values.
// We assume NewSLRUCache is now generic: NewSLRUCache[K comparable, V any](lruCap, probeCap int) *SLRUCache[K, V]

// Helper functions adapted for generic SLRUCache[string, string]

// insertN inserts `count` string keys and values starting from offset into the cache.
func insertN(c *SLRUCache[string, string], count int, offset int) {
	for n := 0; n < count; n++ {
		s := strconv.Itoa(n + offset)
		c.Insert(s, s)
	}
}

// lookupN looks up `count` string keys starting from offset in the cache.
func lookupN(c *SLRUCache[string, string], count int, offset int) {
	for n := 0; n < count; n++ {
		s := strconv.Itoa(n + offset)
		c.Lookup(s)
	}
}

// checkListCount verifies the counts of freelist, lrulist, and probelist in the cache.
// Returns true if any count mismatches, false otherwise.
func checkListCount(c *SLRUCache[string, string], free, lru, probe int, msg string) bool {
	fail := false
	if c.probelist.count != probe {
		fail = true
	}
	if c.lrulist.count != lru {
		fail = true
	}
	if c.freelist.count != free {
		fail = true
	}
	if fail {
		fmt.Printf("checkListCount: free:%d/%d lru:%d/%d probe:%d/%d (%s)\n",
			free, c.freelist.count, lru, c.lrulist.count, probe, c.probelist.count, msg)
	}
	return fail
}

// TestSLRUCacheInsert tests insertion behavior of the generic SLRUCache.
func TestSLRUCacheInsert(t *testing.T) {
	// insert up to the cache capacity
	c := NewSLRUCache[string, string](10, 10)
	insertN(c, 10, 0)
	// all inserted entries supposed to be in probe
	if checkListCount(c, 10, 0, 10, "insert up to the cache capacity") || checkSLRUCacheSanity(c) {
		t.Fail()
	}

	// insert twice with doubled probe capacity
	c = NewSLRUCache[string, string](10, 20)
	insertN(c, 10, 0)
	insertN(c, 10, 0)
	// all distinct inserted are supposed to be in probe
	if checkListCount(c, 20, 0, 10, "insert twice with doubled probe capacity") || checkSLRUCacheSanity(c) {
		t.Fail()
	}
}

// TestSLRUCacheLookup tests lookup behavior and promotion from probe to lru.
func TestSLRUCacheLookup(t *testing.T) {
	// insert capacity and lookup every inserted once
	c := NewSLRUCache[string, string](10, 10)
	insertN(c, 10, 0)
	lookupN(c, 10, 0)
	// all inserted entries supposed to be in lru
	if checkListCount(c, 10, 10, 0, "insert capacity and lookup every inserted once") || checkSLRUCacheSanity(c) {
		t.Fail()
	}

	// insert additional entries
	insertN(c, 10, 10)
	// additional entries supposed to be in probe
	if checkListCount(c, 0, 10, 10, "insert additional entries") || checkSLRUCacheSanity(c) {
		t.Fail()
	}

	// lookup half of original and half of additional entries
	lookupN(c, 10, 5)
	// originals will remain in lru
	// additional will move from probe to lru displacing some originals into freelist
	if checkListCount(c, 5, 10, 5, "lookup half of original and half of additional entries") || checkSLRUCacheSanity(c) {
		t.Fail()
	}

	// moving window test
	movingWindow(c, 10, 21, 7, 1, false)
	if checkSLRUCacheSanity(c) {
		t.Fail()
	}
}

// movingWindow performs a moving window access pattern over the cache.
// Returns hit and miss counts.
func movingWindow(c *SLRUCache[string, string], windowRange, windowSize, windowStep, windowRepeat int, randomaccess bool) (int, int) {
	baseRange := windowSize * windowRange
	hit := 0
	miss := 0
	var p int
	count := 0
	var s string
	adj := (windowSize % 2)

	rnd := rand.New(rand.NewSource(1)) // deterministic seed

	for w := 0; w < (baseRange - windowSize); w += windowStep {
		for i := 0; i < windowRepeat; i++ {
			count++
			for n := 0; n < windowSize; n++ {
				if randomaccess {
					s = strconv.Itoa(w + rnd.Intn(windowSize))
				} else {
					if count%2 == 0 {
						if n%2 == 0 {
							p = n
						} else {
							p = (windowSize - n) - adj
						}
					} else {
						p = n
					}
					s = strconv.Itoa(w + p)
				}
				if r := c.Lookup(s); r != nil {
					hit++
				} else {
					miss++
					c.Insert(s, s)
				}
			}
		}
	}

	return hit, miss
}

// BenchmarkMovingWindow benchmarks the moving window pattern on the generic cache.
func BenchmarkMovingWindow(b *testing.B) {
	c := NewSLRUCache[string, string](50, 50)
	for n := 0; n < b.N; n++ {
		movingWindow(c, 10, 100, 5, 2, false)
	}
}
