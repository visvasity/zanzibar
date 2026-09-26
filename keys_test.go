// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"sort"
	"testing"
)

const testPrefix = "zz1/"

func TestKeyBuilders(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"config head", configHeadKey(testPrefix, "doc"), "zz1/c/doc"},
		{"config history", configHistoryKey(testPrefix, "doc", 3), "zz1/h/doc/00000000000000000003"},
		{"forward", forwardKey(testPrefix, "doc:readme", "viewer", "user:a@b.com"), "zz1/t/doc:readme/viewer/user:a@b.com"},
		{"reverse", reverseKey(testPrefix, "user:a@b.com", "doc:readme", "viewer"), "zz1/s/user:a@b.com/doc:readme/viewer"},
		{"forward userset subj", forwardKey(testPrefix, "doc:readme", "viewer", "group:eng#member"), "zz1/t/doc:readme/viewer/group:eng#member"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestEncodeVersionOrderPreserving(t *testing.T) {
	versions := []uint64{0, 1, 2, 9, 10, 99, 100, 1000, 1 << 32, ^uint64(0)}
	// Encode, then sort the encoded strings lexicographically.
	enc := make([]string, len(versions))
	for i, v := range versions {
		enc[i] = encodeVersion(v)
		if len(enc[i]) != versionWidth {
			t.Errorf("encodeVersion(%d) width = %d, want %d", v, len(enc[i]), versionWidth)
		}
	}
	sortedByString := append([]string(nil), enc...)
	sort.Strings(sortedByString)
	for i := range enc {
		if enc[i] != sortedByString[i] {
			t.Fatalf("lexicographic order != numeric order: encoded=%v sorted=%v", enc, sortedByString)
		}
	}
}

// inRange reports whether key falls within the half-open range [beg, end).
func inRange(key, beg, end string) bool {
	if key < beg {
		return false
	}
	if end != "" && key >= end {
		return false
	}
	return true
}

func TestForwardKeyContainment(t *testing.T) {
	k := forwardKey(testPrefix, "doc:readme", "viewer", "user:a@b.com")

	// Within its object+relation range.
	beg, end := forwardRelationRange(testPrefix, "doc:readme", "viewer")
	if !inRange(k, beg, end) {
		t.Errorf("key %q not in forwardRelationRange", k)
	}
	// Within its object range.
	beg, end = forwardObjectRange(testPrefix, "doc:readme")
	if !inRange(k, beg, end) {
		t.Errorf("key %q not in forwardObjectRange", k)
	}
	// NOT within a different relation's range.
	beg, end = forwardRelationRange(testPrefix, "doc:readme", "editor")
	if inRange(k, beg, end) {
		t.Errorf("key %q unexpectedly in editor range", k)
	}
	// NOT within a different object's range.
	beg, end = forwardObjectRange(testPrefix, "doc:other")
	if inRange(k, beg, end) {
		t.Errorf("key %q unexpectedly in other-object range", k)
	}
}

func TestForwardRelationRangeDoesNotBleed(t *testing.T) {
	// A relation whose name is a prefix of another ("view" vs "viewer") must not
	// capture the longer relation's keys, thanks to the trailing "/" delimiter.
	kViewer := forwardKey(testPrefix, "doc:readme", "viewer", "user:a@b.com")
	beg, end := forwardRelationRange(testPrefix, "doc:readme", "view")
	if inRange(kViewer, beg, end) {
		t.Errorf("relation range for %q unexpectedly captured %q", "view", kViewer)
	}
}

func TestReverseKeyContainment(t *testing.T) {
	k := reverseKey(testPrefix, "user:a@b.com", "doc:readme", "viewer")
	beg, end := reverseSubjectRange(testPrefix, "user:a@b.com")
	if !inRange(k, beg, end) {
		t.Errorf("key %q not in reverseSubjectRange", k)
	}
	beg, end = reverseSubjectRange(testPrefix, "user:other@b.com")
	if inRange(k, beg, end) {
		t.Errorf("key %q unexpectedly in other-subject range", k)
	}
}

func TestConfigHistoryContainmentAndOrder(t *testing.T) {
	beg, end := configHistoryRange(testPrefix, "doc")
	for _, v := range []uint64{1, 2, 10, 12345} {
		k := configHistoryKey(testPrefix, "doc", v)
		if !inRange(k, beg, end) {
			t.Errorf("history key for v%d (%q) not in configHistoryRange(doc)", v, k)
		}
	}
	// A different namespace's history is outside doc's range.
	other := configHistoryKey(testPrefix, "folder", 1)
	if inRange(other, beg, end) {
		t.Errorf("folder history key %q unexpectedly in doc range", other)
	}
}

func TestConfigHeadRangeExcludesHistory(t *testing.T) {
	// The head-record scan (c/) must not pick up history records (h/).
	beg, end := configHeadRange(testPrefix)
	head := configHeadKey(testPrefix, "doc")
	if !inRange(head, beg, end) {
		t.Errorf("head key %q not in configHeadRange", head)
	}
	hist := configHistoryKey(testPrefix, "doc", 1)
	if inRange(hist, beg, end) {
		t.Errorf("history key %q unexpectedly in configHeadRange", hist)
	}
}
