package slrucache

import (
	"fmt"
)

const SLRU_EOF = -3 // End of list

// list of SLRUCacheEntries. Private functions care for consistency of structures so they can be atomic.
type SLRUList struct {
	entries *[]SLRUCacheEntry
	head    int // head of list
	tail    int // tail of list
	count   int // number of entries
}

func NewSLRUList(entries *[]SLRUCacheEntry) *SLRUList {
	i := new(SLRUList)
	i.count = 0
	i.entries = entries
	i.head = SLRU_EOF
	i.tail = SLRU_EOF
	return i
}

// remove entry from tail:
func (i *SLRUList) removeTail() int {
	t := i.tail
	e := *i.entries
	if t >= 0 {
		i.tail = e[t].prev
		e[t].next = SLRU_EOF
		e[t].prev = SLRU_EOF
		if i.tail == SLRU_EOF {
			// last one has been removed - adjust head
			i.head = SLRU_EOF
		} else {
			e[i.tail].next = SLRU_EOF
		}
		e[t].list = nil
		i.count--
	}
	return t
}

// remove entry from list
func (i *SLRUList) remove(n int) bool {
	e := *i.entries
	// check if entry is member of list
	if e[n].list != i {
		return false
	}
	if i.head == n {
		// check head of list
		i.removeHead()
	} else if i.tail == n {
		// check tail of list
		i.removeTail()
	} else {
		e[e[n].next].prev = e[n].prev
		e[e[n].prev].next = e[n].next
		e[n].next = SLRU_EOF
		e[n].prev = SLRU_EOF
		e[n].list = nil
		i.count--
	}
	return true
}

// remove head from list
func (i *SLRUList) removeHead() int {
	h := i.head
	e := *i.entries
	if h >= 0 {
		i.head = e[h].next
		e[h].next = SLRU_EOF
		e[h].prev = SLRU_EOF
		if i.head == SLRU_EOF {
			// last one has been removed - adjust tail
			i.tail = SLRU_EOF
		} else {
			e[i.head].prev = SLRU_EOF
		}
		e[h].list = nil
		i.count--
	}
	return h
}

// insert at head - does not check entry n before insert
func (i *SLRUList) insertHead(n int) {
	e := *i.entries
	h := i.head
	if h >= 0 {
		// list has entries
		e[h].prev = n
		e[n].next = h
	} else {
		// list was empty
		e[n].next = SLRU_EOF
		i.tail = n
	}
	e[n].prev = SLRU_EOF
	e[n].list = i
	i.head = n
	i.count++
}

//--------------------------------------------------------------------------

// SLRUCacheEntry is a element of a linked list backed by an array of elements for addressing
type SLRUCacheEntry struct {
	key   string
	value string
	prev  int       // index of previous entry ( >=0 if set )
	next  int       // index of next entry ( >=0 if set )
	list  *SLRUList // reference to the list, the entry is in.
}

//--------------------------------------------------------------------------

// SLRUCache is a segmented least recently used cache.
// It is divided into two segments. One segment holds the protected/surviver lru entries that at least have one hit.
// The other holds the probationary entries that have no hit jet in order to protect the lru cache from beeing spoiled by data that is used only once.
// There is a common list of entries for both segments backed by a map for entry lookup. The entries are connected in a linked list
type SLRUCache struct {
	entries []SLRUCacheEntry // array of cache entries
	mapping map[string]int   // maps a key to cache entry
	cnum    int              // total number of cache entries as per snum and pnum
	snum    int              // configured number of survivor entries
	pnum    int              // configured number of probe entries

	insertCb func() // optional insert callback that is called after a key was inserted into lru segment.
	removeCb func() // optional remove callback that is called after a key was removed from lru segment.

	freelist  *SLRUList
	lrulist   *SLRUList
	probelist *SLRUList
}

func NewSLRUCache(lruEntries int, probeEntries int) *SLRUCache {
	i := new(SLRUCache)
	i.snum = lruEntries
	i.pnum = probeEntries
	i.cnum = i.pnum + i.snum

	i.insertCb = i.insertCbStub
	i.removeCb = i.removeCbStub

	i.entries = make([]SLRUCacheEntry, i.cnum)
	i.mapping = make(map[string]int)

	i.freelist = NewSLRUList(&i.entries)
	i.lrulist = NewSLRUList(&i.entries)
	i.probelist = NewSLRUList(&i.entries)

	// initialize freelist
	for x := 0; x < i.cnum; x++ {
		i.freelist.insertHead(x)
	}

	return i
}

func (i *SLRUCache) doPanic(msg string) {
	//allow checking consistency and collecting information before paniking...
	checkSLRUCacheSanity(i)
	panic(msg)
}

func (i *SLRUCache) insertCbStub() {}
func (i *SLRUCache) removeCbStub() {}

// var debug bool = true

// func (i *SLRUCache) log(msg string) {
// 	if debug {
// 		fmt.Printf("f:%0.2d l:%0.2d p:%0.2d - %s\n", i.freelist.count, i.lrulist.count, i.probelist.count, msg)
// 	}
// }

// Lookup a value for a key.
// Returns a string pointer or nil if not found
func (i *SLRUCache) Lookup(key string) (s *string) {
	// i.log(fmt.Sprintf("lookup: [%s] --", key))
	// find entry via mapping
	if n, ok := i.mapping[key]; ok {
		e := &i.entries[n]
		s = &e.value
		if e.list == i.lrulist {
			// if found in lrulist and its not the head allready
			// i.log(fmt.Sprintf("lookup: [%s] found: in lrulist", key))

			if n != i.lrulist.head {
				// remove found from lrulist
				// i.log(fmt.Sprintf("lookup: [%s] found: moving entry to top of lru", key))
				if !i.lrulist.remove(n) {
					i.doPanic(fmt.Sprintf("Inside lookup(%s). can not remove from lrulist[%d]", key, n))
				}
				// insert at head of lrulist
				i.lrulist.insertHead(n)
				// call the inster cb
				i.insertCb()
			}
			// else {
			// i.log(fmt.Sprintf("lookup: [%s] found: lru entry is on top allready", key))
			// }
		} else {
			// i.log(fmt.Sprintf("lookup: [%s] found other: in other list", key))
			// e.list can be freelist or probelist
			// if found in  other list, try to move the entry to head of lru
			if i.lrulist.count >= i.snum {
				// if there is no space in lrulist
				// remove tail from lrulist
				lt := i.lrulist.removeTail()

				// delete old key from mapping and clear data
				delete(i.mapping, i.entries[lt].key)
				i.entries[lt].key = ""
				i.entries[lt].value = ""

				// and put into freelist
				i.freelist.insertHead(lt)

				// call the remove cb
				i.removeCb()
			}
			// i.log(fmt.Sprintf("lookup: [%s] found other: remove from other list -> insert top of lru", key))
			// remove found from list
			if !e.list.remove(n) {
				i.doPanic(fmt.Sprintf("Inside lookup(%s). can not remove from free- or probe-list[%d]", key, n))
			}
			// insert at head of lrulist
			i.lrulist.insertHead(n)
		}
	} else {
		s = nil
		// i.log(fmt.Sprintf("lookup: [%s] not found", key))
	}
	return s
}

// Insert or replace a value for a key in the cache
func (i *SLRUCache) Insert(key string, value string) {
	// i.log(fmt.Sprintf("insert: [%s] --", key))
	// lookup
	if n, ok := i.mapping[key]; ok {
		// i.log(fmt.Sprintf("insert: [%s] found in mapping", key))
		e := &i.entries[n]
		// replace a key
		if e.value != value {
			// reset only if value differs
			e.value = value
		}
	} else {
		// insert new entry
		if i.probelist.count >= i.pnum {
			// i.log(fmt.Sprintf("insert: [%s] insert: probelist is full- move tail to freelist", key))
			// if probelist is full
			// remove tail from probelist
			n = i.probelist.removeTail()
			// remove the key from mapping
			delete(i.mapping, i.entries[n].key)
			i.entries[n].key = ""
			i.entries[n].value = ""
			// call remove callback
			i.removeCb()
		} else {
			// i.log(fmt.Sprintf("insert: [%s] insert: probelist is free", key))
			// pick entry from freelist
			n = i.freelist.removeTail()
			if n == SLRU_EOF {
				i.doPanic(fmt.Sprintf("Inside insert(key:%s, value:%s) no free entry available.", key, value))
			}
		}
		// use recycled n for new entry and set key/value
		// e := &i.entries[n]
		// e.key = key
		// e.value = value

		i.entries[n].key = key
		i.entries[n].value = value

		// insert new key into mapping
		i.mapping[key] = n

		// insert entry at head of probelist
		// i.log(fmt.Sprintf("insert: [%s] insert: insert head of probelist", key))
		i.probelist.insertHead(n)
	}
}

// Remove an entry by its key
func (i *SLRUCache) Remove(key string) bool {

	n, ok := i.mapping[key]
	if !ok {
		return false
	}

	// remove entry from its list
	e := &i.entries[n]
	e.list.remove(n)

	// remove entry from its mapping
	delete(i.mapping, key)
	i.entries[n].key = ""
	i.entries[n].value = ""

	// put it back to freelist
	i.freelist.insertHead(n)

	// call the remove cb
	i.removeCb()

	return true
}

func checkSLRUCacheSanity(c *SLRUCache) bool {
	// walk throu all lists, test entries and check count
	var fail bool
	var topic string

	failure := func(msg string) {
		fail = true
		fmt.Printf("%s: %s\n", topic, msg)
	}

	walkList := func(l *SLRUList) {
		n := l.head
		ln := n
		entries := *l.entries

		var ll *SLRUList
		ll = nil

		for n >= 0 {
			e := entries[n]
			if e.prev >= 0 {
				if entries[e.prev].next != n {
					failure("prev link failure")

				}
			}
			if e.next >= 0 {
				if entries[e.next].prev != n {
					failure("next link failure")
				}
			}

			if e.list == nil {
				failure("list reference failure: nil list reference")
			}

			if ll != nil && ll != e.list {
				failure("list reference failure: multiple liste references")
			}

			if e.key == "" && e.value != "" {
				failure("value error")
			}
			ln = n
			n = e.next
			ll = e.list
		}
		if l.tail != ln {
			failure("tail reference failure")
		}

	}

	// walk throu all lists, test entries and check count
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
