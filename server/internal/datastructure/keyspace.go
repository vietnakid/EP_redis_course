// Package datastructure holds the data structures a Redis key's value can
// be: a plain string, a SimpleSet, a SortedSet, a Bloom filter or a CMS.
// Each lives in its own map on a *Store so the command package's routing
// layer doesn't need to touch storage internals - it looks a key up by
// kind, gets back the structure, and calls methods on it.
package datastructure

// Kind identifies which keyspace a key currently lives in. A
// key belongs to at most one keyspace at a time: overwriting it with a
// different type (e.g. SET on a key that held a set) clears the others.
type Kind int

const (
	KindNone Kind = iota
	KindString
	KindSet
	KindZSet
	KindBloom
	KindCMS
)

// KindOf reports which keyspace key currently lives in, transparently
// treating an expired string as absent. KindNone means the key exists in
// none of the stores.
func (s *Store) KindOf(key string) Kind {
	if _, ok := s.GetLiveString(key); ok {
		return KindString
	}
	if _, ok := s.SetStore[key]; ok {
		return KindSet
	}
	if _, ok := s.ZSetStore[key]; ok {
		return KindZSet
	}
	if _, ok := s.BloomStore[key]; ok {
		return KindBloom
	}
	if _, ok := s.CMSStore[key]; ok {
		return KindCMS
	}
	return KindNone
}

// Exists reports whether key has a live value in any keyspace.
func (s *Store) Exists(key string) bool {
	return s.KindOf(key) != KindNone
}

// Delete removes key from whichever keyspace holds it. It reports whether
// the key existed (and was therefore removed).
func (s *Store) Delete(key string) bool {
	switch s.KindOf(key) {
	case KindString:
		delete(s.StringStore, key)
		s.lruRemove(key)
	case KindSet:
		delete(s.SetStore, key)
	case KindZSet:
		delete(s.ZSetStore, key)
	case KindBloom:
		delete(s.BloomStore, key)
	case KindCMS:
		delete(s.CMSStore, key)
	default:
		return false
	}
	return true
}

// ReplaceKind clears key out of every keyspace except keep. Real Redis
// commands like SET always overwrite whatever type a key held before -
// this is what makes that safe: a key can't simultaneously be a string and
// a stale set left behind by an earlier SADD.
func (s *Store) ReplaceKind(key string, keep Kind) {
	if keep != KindString {
		delete(s.StringStore, key)
		s.lruRemove(key)
	}
	if keep != KindSet {
		delete(s.SetStore, key)
	}
	if keep != KindZSet {
		delete(s.ZSetStore, key)
	}
	if keep != KindBloom {
		delete(s.BloomStore, key)
	}
	if keep != KindCMS {
		delete(s.CMSStore, key)
	}
}
