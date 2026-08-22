package datastructure

import "testing"

func TestSimpleSet(t *testing.T) {
	s := NewSimpleSet()

	if added := s.Add("a", "b", "a"); added != 2 {
		t.Fatalf("Add(a,b,a) = %d, want 2 (a counted once)", added)
	}
	if !s.IsMember("a") || !s.IsMember("b") || s.IsMember("c") {
		t.Fatal("IsMember disagrees with what was added")
	}
	if got := s.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}

	if removed := s.Rem("a", "z"); removed != 1 {
		t.Fatalf("Rem(a,z) = %d, want 1 (z was never a member)", removed)
	}
	if s.IsMember("a") {
		t.Fatal("a still a member after Rem")
	}
	if members := s.Members(); len(members) != 1 || members[0] != "b" {
		t.Fatalf("Members() = %v, want [b]", members)
	}
}
