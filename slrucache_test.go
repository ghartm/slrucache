package slrucache

import (
	"fmt"
	"math/rand"
	"strconv"
	"testing"
)

// go test
func insertN(c *SLRUCache, count int, offset int) {
	for n := 0; n < count; n++ {
		s := strconv.Itoa(n + offset)
		c.Insert(s, s)
	}
}

func lookupN(c *SLRUCache, count int, offset int) {
	for n := 0; n < count; n++ {
		s := strconv.Itoa(n + offset)
		c.Lookup(s)
	}
}

func checkListCount(c *SLRUCache, free, lru, probe int, msg string) bool {
	var fail bool = false
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
		fmt.Printf("checkListCount: free:%d/%d lru:%d/%d probe:%d/%d (%s)", free, c.freelist.count, lru, c.lrulist.count, probe, c.probelist.count, msg)
	}
	return fail
}

func TestSLRUCacheInsert(t *testing.T) {
	// insert up to the cache capacity
	c := NewSLRUCache(10, 10)
	insertN(c, 10, 0)
	// all inserted entries supposed to be in probe
	if checkListCount(c, 10, 0, 10, "insert up to the cache capacity") || checkSLRUCacheSanity(c) {
		t.Fail()
	}

	// insert twice with doubled probe capacity
	c = NewSLRUCache(10, 20)
	insertN(c, 10, 0)
	insertN(c, 10, 0)
	// all distinct inserted are supposed to be in probe
	if checkListCount(c, 20, 0, 10, "insert twice with doubled probe capacity") || checkSLRUCacheSanity(c) {
		t.Fail()
	}
}

func TestSLRUCacheLookup(t *testing.T) {

	// insert capacity and lookup every inserted once
	c := NewSLRUCache(10, 10)
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

func TestSLRUCacheDebug(t *testing.T) {
	c := NewSLRUCache(10, 10)
	//hit, miss := movingWindow(c, 3, 10, 5, 1, false)
	hit, miss := movingWindow(c, 1000, 111, 5, 1, true)
	fmt.Printf("hit:%d miss:%d total:%d ratio: %f%%\n", hit, miss, hit+miss, (float64(hit*100) / float64(hit+miss)))
	checkSLRUCacheSanity(c)
	// fails allway so messages are printed
	//t.Fail()
}

// go test -bench=. -benchmem

func movingWindow(c *SLRUCache, windowRange, windowSize, windowStep, windowRepeat int, randomaccess bool) (int, int) {
	// moving window over range
	baseRange := windowSize * windowRange
	hit := 0
	miss := 0
	var p int
	var count int = 0
	var s string
	adj := (windowSize % 2)

	// use same seed for deterministic results
	rnd := rand.New(rand.NewSource(1))

	for w := 0; w < (baseRange - windowSize); w += windowStep {
		for i := 0; i < windowRepeat; i++ {
			count++
			for n := 0; n < windowSize; n++ {
				// lookup elements of window
				// w = start of window relative to baseRange
				// i = repetition count of window
				// n = index relative to Window
				if randomaccess {
					s = strconv.Itoa(w + rnd.Intn(windowSize))
				} else {
					if count%2 == 0 {
						// fixed and mixed access pattern for deterministic results
						if n%2 == 0 {
							// pick even window positions ascending from left side
							p = n
						} else {
							// pick odd window positions descending from right side
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

func BenchmarkMovingWindow(b *testing.B) {
	c := NewSLRUCache(50, 50)
	for n := 0; n < b.N; n++ {
		movingWindow(c, 10, 100, 5, 2, false)
	}
}
