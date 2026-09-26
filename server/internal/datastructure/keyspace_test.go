package datastructure

import "testing"

func TestKeyspaceCrossType(t *testing.T) {
	s := NewStore()
	s.StringStore["s"] = StringEntry{Value: "v"}
	s.SetStore["set"] = NewSimpleSet()
	s.ZSetStore["z"] = newSkiplistZSet()

	cases := map[string]Kind{"s": KindString, "set": KindSet, "z": KindZSet, "missing": KindNone}
	for key, want := range cases {
		if got := s.KindOf(key); got != want {
			t.Errorf("KindOf(%q) = %v, want %v", key, got, want)
		}
		if got := s.Exists(key); got != (want != KindNone) {
			t.Errorf("Exists(%q) = %v, want %v", key, got, want != KindNone)
		}
	}

	if !s.Delete("set") {
		t.Fatal("Delete(set) = false, want true")
	}
	if s.Exists("set") {
		t.Fatal("set still exists after Delete")
	}
	if s.Delete("missing") {
		t.Fatal("Delete(missing) = true, want false")
	}

	s.ReplaceKind("s", KindString)
	if !s.Exists("s") {
		t.Fatal("ReplaceKind(s, KindString) deleted s itself")
	}
	s.StringStore["z"] = StringEntry{Value: "overwritten"}
	s.ReplaceKind("z", KindString)
	if _, ok := s.ZSetStore["z"]; ok {
		t.Fatal("ReplaceKind(z, KindString) left a stale ZSetStore entry")
	}
}
