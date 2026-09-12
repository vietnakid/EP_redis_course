// Package datastructure holds the data structures a Redis key's value can
// be: a plain string, a SimpleSet, a SortedSet, a Bloom filter or a CMS.
// Each lives in its own package-level map so the command package's
// routing layer doesn't need to
// touch storage internals - it looks a key up by kind, gets back the
// structure, and calls methods on it.
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
func KindOf(key string) Kind {
	if _, ok := GetLiveString(key); ok {
		return KindString
	}
	if _, ok := SetStore[key]; ok {
		return KindSet
	}
	if _, ok := ZSetStore[key]; ok {
		return KindZSet
	}
	if _, ok := BloomStore[key]; ok {
		return KindBloom
	}
	if _, ok := CMSStore[key]; ok {
		return KindCMS
	}
	return KindNone
}

// Exists reports whether key has a live value in any keyspace.
func Exists(key string) bool {
	return KindOf(key) != KindNone
}

// Delete removes key from whichever keyspace holds it. It reports whether
// the key existed (and was therefore removed).
func Delete(key string) bool {
	switch KindOf(key) {
	case KindString:
		delete(StringStore, key)
		lruRemove(key)
	case KindSet:
		delete(SetStore, key)
	case KindZSet:
		delete(ZSetStore, key)
	case KindBloom:
		delete(BloomStore, key)
	case KindCMS:
		delete(CMSStore, key)
	default:
		return false
	}
	return true
}

// ReplaceKind clears key out of every keyspace except keep. Real Redis
// commands like SET always overwrite whatever type a key held before -
// this is what makes that safe: a key can't simultaneously be a string and
// a stale set left behind by an earlier SADD.
func ReplaceKind(key string, keep Kind) {
	if keep != KindString {
		delete(StringStore, key)
		lruRemove(key)
	}
	if keep != KindSet {
		delete(SetStore, key)
	}
	if keep != KindZSet {
		delete(ZSetStore, key)
	}
	if keep != KindBloom {
		delete(BloomStore, key)
	}
	if keep != KindCMS {
		delete(CMSStore, key)
	}
}
