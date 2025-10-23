// author: (c) Gunter Hartmann

package slrucache

import (
	"fmt"
)

// SLRU_EOF is a special marker for the end of the list.
const SLRU_EOF = -3

// SLRUCacheEntry represents an element in the cache linked list.
// It stores the key, value, and pointers to previous and next entries by index.
// Key and Value are generic types.
type SLRUCacheEntry[K comparable, V any] struct {
	key   K
	value V
	prev  int             // index of previous entry (>=0 if set)
	next  int             // index of next entry (>=0 if set)
	list  *SLRUList[K, V] // pointer to the list this entry belongs to
}

// SLRUList is a doubly linked list of SLRUCacheEntries backed by an array.
// It maintains head and tail indices and the count of entries.
type SLRUList[K comparable, V any] struct {
	entries *[]SLRUCacheEntry[K, V]
	head    int // index of the head entry
	tail    int // index of the tail entry
	count   int // number of entries in the list
}

// NewSLRUList initializes a new empty SLRUList backed by the given entries slice.
func NewSLRUList[K comparable, V any](entries *[]SLRUCacheEntry[K, V]) *SLRUList[K, V] {
	return &SLRUList[K, V]{
		entries: entries,
		head:    SLRU_EOF,
		tail:    SLRU_EOF,
		count:   0,
	}
}

// removeTail removes the tail entry from the list and returns its index.
func (l *SLRUList[K, V]) removeTail() int {
	t := l.tail
	if t < 0 {
		return SLRU_EOF
	}

	e := *l.entries
	l.tail = e[t].prev
	e[t].next = SLRU_EOF
	e[t].prev = SLRU_EOF

	if l.tail == SLRU_EOF {
		// List is now empty
		l.head = SLRU_EOF
	} else {
		e[l.tail].next = SLRU_EOF
	}
	e[t].list = nil
	l.count--

	return t
}

// removeHead removes the head entry from the list and returns its index.
func (l *SLRUList[K, V]) removeHead() int {
	h := l.head
	if h < 0 {
		return SLRU_EOF
	}

	e := *l.entries
	l.head = e[h].next
	e[h].next = SLRU_EOF
	e[h].prev = SLRU_EOF

	if l.head == SLRU_EOF {
		// List is now empty
		l.tail = SLRU_EOF
	} else {
		e[l.head].prev = SLRU_EOF
	}
	e[h].list = nil
	l.count--

	return h
}

// remove removes the entry at index n from the list.
// Returns false if the entry is not part of this list.
func (l *SLRUList[K, V]) remove(n int) bool {
	e := *l.entries

	// Check if entry belongs to this list
	if e[n].list != l {
		return false
	}

	if l.head == n {
		l.removeHead()
	} else if l.tail == n {
		l.removeTail()
	} else {
		// Link previous and next entries
		e[e[n].next].prev = e[n].prev
		e[e[n].prev].next = e[n].next

		e[n].next = SLRU_EOF
		e[n].prev = SLRU_EOF
		e[n].list = nil
		l.count--
	}

	return true
}

// insertHead inserts the entry at index n at the head of the list.
// Does not check if entry already exists in the list.
func (l *SLRUList[K, V]) insertHead(n int) {
	e := *l.entries
	h := l.head

	if h >= 0 {
		// List has entries, link new head
		e[h].prev = n
		e[n].next = h
	} else {
		// List was empty
		e[n].next = SLRU_EOF
		l.tail = n
	}

	e[n].prev = SLRU_EOF
	e[n].list = l
	l.head = n
	l.count++
}

// SLRUCache implements a segmented LRU cache with two segments:
// - lrulist: protected entries with at least one hit (survivor entries)
// - probelist: probationary entries with no hits yet
// Entries are backed by an array and indexed by a map for O(1) lookup.
// Key type must be comparable for map keys.
type SLRUCache[K comparable, V any] struct {
	entries []SLRUCacheEntry[K, V]
	mapping map[K]int // key to entry index

	cnum int // total number of entries (snum + pnum)
	snum int // number of survivor entries (lrulist size)
	pnum int // number of probationary entries (probelist size)

	insertCb func(K) // optional callback after insert into lrulist
	removeCb func(K) // optional callback after removal from lrulist

	freelist  *SLRUList[K, V] // list of free entries
	lrulist   *SLRUList[K, V] // protected segment
	probelist *SLRUList[K, V] // probationary segment
}

// NewSLRUCache creates a new SLRUCache with given sizes for survivor and probe segments.
func NewSLRUCache[K comparable, V any](lruEntries int, probeEntries int) *SLRUCache[K, V] {
	cache := &SLRUCache[K, V]{
		snum:    lruEntries,
		pnum:    probeEntries,
		cnum:    lruEntries + probeEntries,
		mapping: make(map[K]int),
	}

	cache.entries = make([]SLRUCacheEntry[K, V], cache.cnum)

	cache.freelist = NewSLRUList(&cache.entries)
	cache.lrulist = NewSLRUList(&cache.entries)
	cache.probelist = NewSLRUList(&cache.entries)

	cache.insertCb = nil
	cache.removeCb = nil

	// Initialize freelist with all entries
	for i := 0; i < cache.cnum; i++ {
		cache.freelist.insertHead(i)
	}

	return cache
}

// doPanic is called on fatal errors to check cache sanity before panicking.
func (c *SLRUCache[K, V]) doPanic(msg string) {
	checkSLRUCacheSanity(c)
	panic(msg)
}

// Lookup returns a pointer to the value for the given key, or nil if not found.
// It also promotes entries from probelist to lrulist on hit.
func (c *SLRUCache[K, V]) Lookup(key K) *V {
	n, ok := c.mapping[key]
	if !ok {
		return nil
	}

	e := &c.entries[n]
	// If entry is in lrulist (protected segment)
	if e.list == c.lrulist {
		if n != c.lrulist.head {
			// Move to head of lrulist (most recently used)
			if !c.lrulist.remove(n) {
				c.doPanic(fmt.Sprintf("Lookup: cannot remove from lrulist index %d", n))
			}
			c.lrulist.insertHead(n)
		}
		return &e.value
	}

	// Entry is in probelist or freelist (should not be freelist)
	// Try to promote to lrulist
	if c.lrulist.count >= c.snum {
		// lrulist full, remove tail entry
		lt := c.lrulist.removeTail()
		if lt != SLRU_EOF {
			// Remove old key from mapping and clear entry
			delete(c.mapping, c.entries[lt].key)
			if c.removeCb != nil {
				c.removeCb(c.entries[lt].key)
			}
			var zeroK K
			var zeroV V
			c.entries[lt].key = zeroK
			c.entries[lt].value = zeroV
			// Put removed entry into freelist
			c.freelist.insertHead(lt)
		}
	}

	// Remove from current list (probelist)
	if !e.list.remove(n) {
		c.doPanic(fmt.Sprintf("Lookup: cannot remove from probelist index %d", n))
	}
	// Insert at head of lrulist
	c.lrulist.insertHead(n)
	if c.insertCb != nil {
		c.insertCb(key)
	}

	return &e.value
}

// Insert adds or updates a key-value pair in the cache.
// New entries go into the probelist first.
func (c *SLRUCache[K, V]) Insert(key K, value V) {
	if n, ok := c.mapping[key]; ok {
		// Key exists, update value if changed
		e := &c.entries[n]
		e.value = value
		return
	}

	var n int
	if c.probelist.count >= c.pnum {
		// Probelist full, evict tail entry
		n = c.probelist.removeTail()
		if n == SLRU_EOF {
			c.doPanic(fmt.Sprintf("Insert: no entry to evict in probelist for key %v", key))
		}
		// Remove old key from mapping and clear entry
		delete(c.mapping, c.entries[n].key)
		var zeroK K
		var zeroV V
		c.entries[n].key = zeroK
		c.entries[n].value = zeroV

	} else {
		// Take from freelist
		n = c.freelist.removeTail()
		if n == SLRU_EOF {
			c.doPanic(fmt.Sprintf("Insert: no free entry available for key %v", key))
		}
	}

	// Set new key and value
	c.entries[n].key = key
	c.entries[n].value = value

	// Add to mapping
	c.mapping[key] = n

	// Insert at head of probelist
	c.probelist.insertHead(n)
}

// Remove deletes an entry by key from the cache.
// Returns true if the entry was found and removed.
func (c *SLRUCache[K, V]) Remove(key K) bool {
	n, ok := c.mapping[key]
	if !ok {
		return false
	}

	e := &c.entries[n]
	if e.list != nil {
		e.list.remove(n)
	}

	delete(c.mapping, key)

	// Clear entry and return to freelist
	var zeroK K
	var zeroV V
	e.key = zeroK
	e.value = zeroV
	c.freelist.insertHead(n)

	if c.removeCb != nil {
		c.removeCb(key)
	}

	return true
}

// checkSLRUCacheSanity verifies internal consistency of the cache lists.
// Returns true if any inconsistency is found.
func checkSLRUCacheSanity[K comparable, V any](c *SLRUCache[K, V]) bool {
	fail := false
	topic := ""

	failure := func(msg string) {
		fail = true
		fmt.Printf("%s: %s\n", topic, msg)
	}

	walkList := func(l *SLRUList[K, V]) {
		n := l.head
		ln := n
		entries := *l.entries

		var lastList *SLRUList[K, V]

		for n >= 0 {
			e := entries[n]

			if e.prev >= 0 && entries[e.prev].next != n {
				failure("prev link failure")
			}
			if e.next >= 0 && entries[e.next].prev != n {
				failure("next link failure")
			}
			if e.list == nil {
				failure("nil list reference")
			}
			if lastList != nil && lastList != e.list {
				failure("multiple list references")
			}

			ln = n
			n = e.next
			lastList = e.list
		}

		if l.tail != ln {
			failure("tail reference mismatch")
		}
	}

	topic = "freelist"
	walkList(c.freelist)
	topic = "probelist"
	walkList(c.probelist)
	topic = "lrulist"
	walkList(c.lrulist)

	if c.freelist.count > c.cnum {
		failure("freelist size overflow")
	}
	if c.probelist.count > c.pnum {
		failure("probelist size overflow")
	}
	if c.lrulist.count > c.snum {
		failure("lrulist size overflow")
	}

	return fail
}
