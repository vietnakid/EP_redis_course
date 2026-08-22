package datastructure

import (
	"math/rand/v2"
	"strings"
)

// skiplistMaxLevel bounds how many forward pointers one node can carry.
// 32 levels comfortably covers millions of members at the standard p=0.5
// promotion probability used by randomLevel.
const skiplistMaxLevel = 32

// skiplistLevel is one node's forward pointer at one level, plus span: how
// many nodes (at level 0) lie between this node and levels[i].forward -
// what makes rank computable while walking down through levels instead of
// needing a full linear scan.
type skiplistLevel struct {
	forward *skiplistNode
	span    uint32
}

type skiplistNode struct {
	member   string
	score    float64
	backward *skiplistNode
	levels   []skiplistLevel
}

type skiplist struct {
	head   *skiplistNode
	tail   *skiplistNode
	length uint32
	level  int
}

func newSkiplistNode(level int, score float64, member string) *skiplistNode {
	return &skiplistNode{member: member, score: score, levels: make([]skiplistLevel, level)}
}

func newSkiplist() *skiplist {
	sl := &skiplist{length: 0, level: 1}
	sl.head = newSkiplistNode(skiplistMaxLevel, 0, "")
	return sl
}

func (sl *skiplist) randomLevel() int {
	level := 1
	for rand.IntN(2) == 1 && level < skiplistMaxLevel {
		level++
	}
	return level
}

// insert adds a new node for (score, member), sorted by score then member.
// Caller must ensure member is not already present anywhere in the list -
// use updateScore to relocate an existing member instead.
func (sl *skiplist) insert(score float64, member string) *skiplistNode {
	update := [skiplistMaxLevel]*skiplistNode{}
	rank := [skiplistMaxLevel]uint32{}
	x := sl.head

	for i := sl.level - 1; i >= 0; i-- {
		if i == sl.level-1 {
			rank[i] = 0
		} else {
			rank[i] = rank[i+1]
		}
		for x.levels[i].forward != nil && (x.levels[i].forward.score < score ||
			(x.levels[i].forward.score == score && strings.Compare(x.levels[i].forward.member, member) < 0)) {
			rank[i] += x.levels[i].span
			x = x.levels[i].forward
		}
		update[i] = x
	}

	level := sl.randomLevel()
	if level > sl.level {
		for i := sl.level; i < level; i++ {
			rank[i] = 0
			update[i] = sl.head
			update[i].levels[i].span = sl.length
		}
		sl.level = level
	}

	x = newSkiplistNode(level, score, member)
	for i := range level {
		x.levels[i].forward = update[i].levels[i].forward
		update[i].levels[i].forward = x
		x.levels[i].span = update[i].levels[i].span - (rank[0] - rank[i])
		update[i].levels[i].span = rank[0] - rank[i] + 1
	}
	for i := level; i < sl.level; i++ {
		update[i].levels[i].span++
	}

	if update[0] == sl.head {
		x.backward = nil
	} else {
		x.backward = update[0]
	}
	if x.levels[0].forward != nil {
		x.levels[0].forward.backward = x
	} else {
		sl.tail = x
	}
	sl.length++
	return x
}

// rank returns the 1-based position of (score, member) in ascending
// order, or 0 if no such node exists.
func (sl *skiplist) rank(score float64, member string) uint32 {
	x := sl.head
	var r uint32 = 0
	for i := sl.level - 1; i >= 0; i-- {
		for x.levels[i].forward != nil && (x.levels[i].forward.score < score ||
			(x.levels[i].forward.score == score && strings.Compare(x.levels[i].forward.member, member) <= 0)) {
			r += x.levels[i].span
			x = x.levels[i].forward
		}
	}
	if x != sl.head && x.score == score && x.member == member {
		return r
	}
	return 0
}

// updateScore relocates member from curScore to newScore, keeping the
// list sorted, and returns the (possibly new) node. Assumes member is
// currently present with curScore.
func (sl *skiplist) updateScore(curScore float64, member string, newScore float64) *skiplistNode {
	update := [skiplistMaxLevel]*skiplistNode{}
	x := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for x.levels[i].forward != nil && (x.levels[i].forward.score < curScore ||
			(x.levels[i].forward.score == curScore && strings.Compare(x.levels[i].forward.member, member) < 0)) {
			x = x.levels[i].forward
		}
		update[i] = x
	}
	x = x.levels[0].forward

	// Fast path: the node stays between the same neighbors at the new
	// score, so just mutate the score in place - no relink needed.
	if (x.backward == nil || x.backward.score < newScore) &&
		(x.levels[0].forward == nil || x.levels[0].forward.score > newScore) {
		x.score = newScore
		return x
	}

	sl.delete(x, update)
	return sl.insert(newScore, member)
}

func (sl *skiplist) delete(x *skiplistNode, update [skiplistMaxLevel]*skiplistNode) {
	for i := range sl.level {
		if update[i].levels[i].forward == x {
			update[i].levels[i].span += x.levels[i].span - 1
			update[i].levels[i].forward = x.levels[i].forward
		} else {
			update[i].levels[i].span--
		}
	}
	if x.levels[0].forward != nil {
		x.levels[0].forward.backward = x.backward
	} else {
		sl.tail = x.backward
	}
	for sl.level > 1 && sl.head.levels[sl.level-1].forward == nil {
		sl.level--
	}
	sl.length--
}

// skiplistZSet is the skip-list-backed SortedSet - the approach real
// Redis uses. Average O(log n) search/insert; a member->score map makes
// Score O(1) and gives Add/updateScore the current score without a
// tree/list walk.
type skiplistZSet struct {
	list   *skiplist
	scores map[string]float64
}

func newSkiplistZSet() *skiplistZSet {
	return &skiplistZSet{list: newSkiplist(), scores: make(map[string]float64)}
}

func (z *skiplistZSet) Add(score float64, member string) int {
	if cur, exists := z.scores[member]; exists {
		if cur != score {
			z.list.updateScore(cur, member, score)
			z.scores[member] = score
		}
		return 0
	}
	z.list.insert(score, member)
	z.scores[member] = score
	return 1
}

func (z *skiplistZSet) Score(member string) (float64, bool) {
	score, ok := z.scores[member]
	return score, ok
}

func (z *skiplistZSet) Rank(member string) (int64, bool) {
	score, ok := z.scores[member]
	if !ok {
		return 0, false
	}
	r := z.list.rank(score, member)
	if r == 0 {
		return 0, false
	}
	return int64(r) - 1, true
}
