package datastructure

// lru_list.go tracks StringStore key recency in a doubly linked list, kept
// in perfect order: head = most recently used, tail = least recently used
// (the next exact-LRU eviction victim). lruTouch/lruRemove are O(1) because
// lruNodes gives direct access to any node - no scan needed.

type lruNode struct {
	key        string
	prev, next *lruNode
}

func (s *Store) lruUnlink(n *lruNode) {
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		s.lruHead = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		s.lruTail = n.prev
	}
	n.prev, n.next = nil, nil
}

func (s *Store) lruPushFront(n *lruNode) {
	n.next = s.lruHead
	if s.lruHead != nil {
		s.lruHead.prev = n
	}
	s.lruHead = n
	if s.lruTail == nil {
		s.lruTail = n
	}
}

// lruTouch marks key as the most recently used entry, creating it if unseen.
func (s *Store) lruTouch(key string) {
	if n, ok := s.lruNodes[key]; ok {
		if n == s.lruHead {
			return
		}
		s.lruUnlink(n)
		s.lruPushFront(n)
		return
	}
	n := &lruNode{key: key}
	s.lruNodes[key] = n
	s.lruPushFront(n)
}

// lruRemove drops a key from recency tracking (called on any StringStore
// delete: DEL, expiry, or eviction itself).
func (s *Store) lruRemove(key string) {
	n, ok := s.lruNodes[key]
	if !ok {
		return
	}
	s.lruUnlink(n)
	delete(s.lruNodes, key)
}

// lruVictim returns the least recently used key without removing it.
func (s *Store) lruVictim() (string, bool) {
	if s.lruTail == nil {
		return "", false
	}
	return s.lruTail.key, true
}
