package datastructure

// bItem is one (score, member) pair stored in a B+ tree node.
type bItem struct {
	score  float64
	member string
}

// less orders items by score, ties broken by member, so every (score,
// member) pair has one unambiguous position in the tree.
func less(score float64, member string, item *bItem) bool {
	if score != item.score {
		return score < item.score
	}
	return member < item.member
}

// bNode is one B+ tree node. Leaves hold every item and link to the next
// leaf via next, which is what lets Score/Rank walk every item in sorted
// order without descending the tree per lookup. Internal nodes hold
// routing keys and child pointers only (no members of their own).
type bNode struct {
	items    []*bItem
	children []*bNode
	isLeaf   bool
	parent   *bNode
	next     *bNode // leaf-only
}

// bPlusTree is a B+ tree keyed by (score, member). See splitLeaf and
// splitInternal for the "M-1 keys, M children" overflow handling from the
// lecture slides.
type bPlusTree struct {
	root   *bNode
	degree int // max children per node; a node splits above degree-1 items
}

func newBPlusTree(degree int) *bPlusTree {
	return &bPlusTree{root: &bNode{isLeaf: true}, degree: degree}
}

// add finds the leaf where (score, member) belongs and inserts it in
// sorted position, splitting nodes that overflow. Caller must ensure
// member is not already present anywhere in the tree - use removeMember
// first to relocate an existing member to a new score.
func (t *bPlusTree) add(score float64, member string) {
	node := t.root
	for !node.isLeaf {
		i := 0
		for i < len(node.items) && !less(score, member, node.items[i]) {
			i++
		}
		node = node.children[i]
	}

	i := 0
	for i < len(node.items) && !less(score, member, node.items[i]) {
		i++
	}
	node.items = append(node.items, nil)
	copy(node.items[i+1:], node.items[i:])
	node.items[i] = &bItem{score: score, member: member}

	if len(node.items) > t.degree-1 {
		t.splitNode(node)
	}
}

// removeMember splices member's item out of whichever leaf holds it,
// walking the leaf chain from the leftmost leaf. No rebalancing needed:
// this tree only enforces a max fill (splits on overflow) and never a
// min fill, so a leaf shrinking - even to empty - breaks no invariant.
func (t *bPlusTree) removeMember(member string) {
	node := t.root
	for !node.isLeaf {
		node = node.children[0]
	}
	for node != nil {
		for i, it := range node.items {
			if it.member == member {
				node.items = append(node.items[:i], node.items[i+1:]...)
				return
			}
		}
		node = node.next
	}
}

func (t *bPlusTree) splitNode(node *bNode) {
	if node.parent == nil {
		t.splitRoot()
		return
	}
	if node.isLeaf {
		t.splitLeaf(node)
	} else {
		t.splitInternal(node)
	}
}

func (t *bPlusTree) splitLeaf(node *bNode) {
	mid := len(node.items) / 2
	sibling := &bNode{isLeaf: true, parent: node.parent, next: node.next}
	sibling.items = append(sibling.items, node.items[mid:]...)
	node.items = node.items[:mid]
	node.next = sibling

	// The new leaf's first key is promoted as-is (not removed from the
	// leaf): leaves hold every item, internal nodes just route.
	t.insertIntoParent(node, sibling, sibling.items[0])
}

func (t *bPlusTree) splitInternal(node *bNode) {
	mid := len(node.items) / 2
	promoted := node.items[mid]

	sibling := &bNode{parent: node.parent}
	sibling.items = append(sibling.items, node.items[mid+1:]...)
	sibling.children = append(sibling.children, node.children[mid+1:]...)
	node.items = node.items[:mid]
	node.children = node.children[:mid+1]
	for _, child := range sibling.children {
		child.parent = sibling
	}

	t.insertIntoParent(node, sibling, promoted)
}

// insertIntoParent inserts sibling as node's right neighbor in their
// shared parent, promoting key as the routing item between them, and
// recursively splits the parent if that overflows it.
func (t *bPlusTree) insertIntoParent(node, sibling *bNode, key *bItem) {
	parent := node.parent
	idx := 0
	for idx < len(parent.children) && parent.children[idx] != node {
		idx++
	}

	parent.items = append(parent.items, nil)
	copy(parent.items[idx+1:], parent.items[idx:])
	parent.items[idx] = key

	parent.children = append(parent.children, nil)
	copy(parent.children[idx+2:], parent.children[idx+1:])
	parent.children[idx+1] = sibling

	if len(parent.items) > t.degree-1 {
		t.splitNode(parent)
	}
}

func (t *bPlusTree) splitRoot() {
	oldRoot := t.root
	newRoot := &bNode{}
	t.root = newRoot
	oldRoot.parent = newRoot
	newRoot.children = append(newRoot.children, oldRoot)

	if oldRoot.isLeaf {
		t.splitLeaf(oldRoot)
	} else {
		t.splitInternal(oldRoot)
	}
}

// bPlusZSet is the B+-tree-backed SortedSet, as used by Dragonfly: lower
// per-node memory overhead than a skip list.
type bPlusZSet struct {
	tree   *bPlusTree
	scores map[string]float64
}

func newBPlusZSet(degree int) *bPlusZSet {
	return &bPlusZSet{tree: newBPlusTree(degree), scores: make(map[string]float64)}
}

func (z *bPlusZSet) Add(score float64, member string) int {
	if cur, exists := z.scores[member]; exists {
		if cur != score {
			z.tree.removeMember(member)
			z.tree.add(score, member)
			z.scores[member] = score
		}
		return 0
	}
	z.tree.add(score, member)
	z.scores[member] = score
	return 1
}

// Score is O(1): the hashmap half of the lecture's "hybrid approach", so
// member lookup never needs a tree walk.
func (z *bPlusZSet) Score(member string) (float64, bool) {
	score, ok := z.scores[member]
	return score, ok
}

// ponytail: Rank walks the leaf chain from the start counting items,
// since this tree keeps no per-subtree item counts - O(n) rather than
// O(log n). Fine for the lecture's scope (ZRANK isn't the hot path); add
// subtree counts to internal nodes if ZRANK throughput ever matters.
func (z *bPlusZSet) Rank(member string) (int64, bool) {
	if _, ok := z.scores[member]; !ok {
		return 0, false
	}
	node := z.tree.root
	for !node.isLeaf {
		node = node.children[0]
	}
	var rank int64
	for node != nil {
		for _, it := range node.items {
			if it.member == member {
				return rank, true
			}
			rank++
		}
		node = node.next
	}
	return 0, false
}
