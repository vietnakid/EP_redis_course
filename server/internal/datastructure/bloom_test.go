package datastructure

import (
	"strconv"
	"testing"
)

func TestBloomAddExist(t *testing.T) {
	b := CreateBloomFilter(1000, 0.01)

	if b.Exist("a") {
		t.Fatalf("Exist on empty filter = true, want false")
	}
	if !b.AddIfNotExist("a") {
		t.Fatalf("first AddIfNotExist(a) = false, want true (bits were unset)")
	}
	if b.AddIfNotExist("a") {
		t.Fatalf("second AddIfNotExist(a) = true, want false (already added)")
	}
	if !b.Exist("a") {
		t.Fatalf("Exist(a) after add = false - a Bloom filter must never false-negative")
	}
}

// A Bloom filter may say yes to something it never saw, but it must never
// say no to something it did: every added item stays findable.
func TestBloomNoFalseNegative(t *testing.T) {
	const n = 1000
	b := CreateBloomFilter(n, 0.01)

	items := make([]string, n)
	for i := range items {
		items[i] = "item:" + strconv.Itoa(i)
		b.AddIfNotExist(items[i])
	}
	for _, item := range items {
		if !b.Exist(item) {
			t.Fatalf("Exist(%q) = false after add, want true", item)
		}
	}
}
