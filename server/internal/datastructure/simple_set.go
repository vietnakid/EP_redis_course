package datastructure

// SimpleSet is an unordered collection of unique string members, backed by
// a native Go map - Add/Rem/IsMember are all O(1).
type SimpleSet struct {
	members map[string]struct{}
}

func NewSimpleSet() *SimpleSet {
	return &SimpleSet{members: make(map[string]struct{})}
}

// Add inserts members not already present and returns how many were new.
func (s *SimpleSet) Add(members ...string) int {
	added := 0
	for _, m := range members {
		if _, exists := s.members[m]; !exists {
			s.members[m] = struct{}{}
			added++
		}
	}
	return added
}

// Rem removes members that are present and returns how many were removed.
func (s *SimpleSet) Rem(members ...string) int {
	removed := 0
	for _, m := range members {
		if _, exists := s.members[m]; exists {
			delete(s.members, m)
			removed++
		}
	}
	return removed
}

func (s *SimpleSet) IsMember(member string) bool {
	_, exists := s.members[member]
	return exists
}

func (s *SimpleSet) Len() int {
	return len(s.members)
}

// Members returns every member in unspecified order, matching real Redis:
// SMEMBERS makes no ordering guarantee for a plain set.
func (s *SimpleSet) Members() []string {
	out := make([]string, 0, len(s.members))
	for m := range s.members {
		out = append(out, m)
	}
	return out
}

// SetStore holds every key whose value is a SimpleSet.
var SetStore = make(map[string]*SimpleSet)
