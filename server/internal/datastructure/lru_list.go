package datastructure

// lru_list.go tracks StringStore key recency in a doubly linked list, kept
// in perfect order: head = most recently used, tail = least recently used
// (the next exact-LRU eviction victim). lruTouch/lruRemove are O(1) because
// lruNodes gives direct access to any node - no scan needed.

type lruNode struct {
	key        string
	prev, next *lruNode
}

var (
	lruHead, lruTail *lruNode
	lruNodes         = make(map[string]*lruNode)
)

func lruUnlink(n *lruNode) {
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		lruHead = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		lruTail = n.prev
	}
	n.prev, n.next = nil, nil
}

func lruPushFront(n *lruNode) {
	n.next = lruHead
	if lruHead != nil {
		lruHead.prev = n
	}
	lruHead = n
	if lruTail == nil {
		lruTail = n
	}
}

// lruTouch marks key as the most recently used entry, creating it if unseen.
func lruTouch(key string) {
	if n, ok := lruNodes[key]; ok {
		if n == lruHead {
			return
		}
		lruUnlink(n)
		lruPushFront(n)
		return
	}
	n := &lruNode{key: key}
	lruNodes[key] = n
	lruPushFront(n)
}

// lruRemove drops a key from recency tracking (called on any StringStore
// delete: DEL, expiry, or eviction itself).
func lruRemove(key string) {
	n, ok := lruNodes[key]
	if !ok {
		return
	}
	lruUnlink(n)
	delete(lruNodes, key)
}

// lruVictim returns the least recently used key without removing it.
func lruVictim() (string, bool) {
	if lruTail == nil {
		return "", false
	}
	return lruTail.key, true
}
