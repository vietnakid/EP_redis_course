package datastructure

import "testing"

func TestKeyspaceCrossType(t *testing.T) {
	StringStore = make(map[string]StringEntry)
	SetStore = make(map[string]*SimpleSet)
	ZSetStore = make(map[string]SortedSet)

	StringStore["s"] = StringEntry{Value: "v"}
	SetStore["set"] = NewSimpleSet()
	ZSetStore["z"] = newSkiplistZSet()

	cases := map[string]Kind{"s": KindString, "set": KindSet, "z": KindZSet, "missing": KindNone}
	for key, want := range cases {
		if got := KindOf(key); got != want {
			t.Errorf("KindOf(%q) = %v, want %v", key, got, want)
		}
		if got := Exists(key); got != (want != KindNone) {
			t.Errorf("Exists(%q) = %v, want %v", key, got, want != KindNone)
		}
	}

	if !Delete("set") {
		t.Fatal("Delete(set) = false, want true")
	}
	if Exists("set") {
		t.Fatal("set still exists after Delete")
	}
	if Delete("missing") {
		t.Fatal("Delete(missing) = true, want false")
	}

	ReplaceKind("s", KindString)
	if !Exists("s") {
		t.Fatal("ReplaceKind(s, KindString) deleted s itself")
	}
	StringStore["z"] = StringEntry{Value: "overwritten"}
	ReplaceKind("z", KindString)
	if _, ok := ZSetStore["z"]; ok {
		t.Fatal("ReplaceKind(z, KindString) left a stale ZSetStore entry")
	}
}
