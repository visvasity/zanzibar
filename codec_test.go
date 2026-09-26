// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"errors"
	"reflect"
	"testing"
)

func TestTupleMetaGobRoundTrip(t *testing.T) {
	in := tupleMeta{CreatedAtUnixNano: 111, NotBeforeUnixNano: 222, NotAfterUnixNano: 333}
	data, err := gobEncode(in)
	if err != nil {
		t.Fatalf("gobEncode: %v", err)
	}
	var out tupleMeta
	if err := gobDecode(data, &out); err != nil {
		t.Fatalf("gobDecode: %v", err)
	}
	if in != out {
		t.Errorf("round-trip: got %+v, want %+v", out, in)
	}
}

func TestNamespaceConfigGobRoundTrip(t *testing.T) {
	in := NamespaceConfig{
		Namespace: "doc",
		Version:   7,
		Relations: map[string]Rewrite{
			"owner": {This: &This{}},
			"editor": {Union: &SetOperation{Children: []Rewrite{
				{This: &This{}},
				{ComputedUserset: &ComputedUserset{Relation: "owner"}},
			}}},
			"viewer": {Union: &SetOperation{Children: []Rewrite{
				{This: &This{}},
				{ComputedUserset: &ComputedUserset{Relation: "editor"}},
				{TupleToUserset: &TupleToUserset{Tupleset: "parent", ComputedUserset: "viewer"}},
			}}},
		},
	}
	data, err := gobEncode(in)
	if err != nil {
		t.Fatalf("gobEncode: %v", err)
	}
	var out NamespaceConfig
	if err := gobDecode(data, &out); err != nil {
		t.Fatalf("gobDecode: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round-trip mismatch:\n got %#v\nwant %#v", out, in)
	}
}

func TestTupleMetaActiveAt(t *testing.T) {
	const (
		lo = 1000
		hi = 2000
	)
	tests := []struct {
		name string
		m    tupleMeta
		asOf int64
		want bool
	}{
		{"no interval, no asOf", tupleMeta{}, 0, true},
		{"no interval, with asOf", tupleMeta{}, 1500, true},
		{"interval but asOf unset -> inactive", tupleMeta{NotAfterUnixNano: hi}, 0, false},
		{"interval but asOf negative -> inactive", tupleMeta{NotBeforeUnixNano: lo}, -5, false},
		{"before window", tupleMeta{NotBeforeUnixNano: lo, NotAfterUnixNano: hi}, 500, false},
		{"at lower bound (inclusive)", tupleMeta{NotBeforeUnixNano: lo, NotAfterUnixNano: hi}, lo, true},
		{"in window", tupleMeta{NotBeforeUnixNano: lo, NotAfterUnixNano: hi}, 1500, true},
		{"at upper bound (exclusive)", tupleMeta{NotBeforeUnixNano: lo, NotAfterUnixNano: hi}, hi, false},
		{"after window", tupleMeta{NotBeforeUnixNano: lo, NotAfterUnixNano: hi}, 2500, false},
		{"unbounded start, before end", tupleMeta{NotAfterUnixNano: hi}, 1500, true},
		{"unbounded start, after end", tupleMeta{NotAfterUnixNano: hi}, 2500, false},
		{"unbounded end, after start", tupleMeta{NotBeforeUnixNano: lo}, 1500, true},
		{"unbounded end, before start", tupleMeta{NotBeforeUnixNano: lo}, 500, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.activeAt(tt.asOf); got != tt.want {
				t.Errorf("activeAt(%d) = %v, want %v", tt.asOf, got, tt.want)
			}
		})
	}
}

func TestValidInterval(t *testing.T) {
	valid := [][2]int64{{0, 0}, {0, 100}, {100, 0}, {100, 200}}
	for _, iv := range valid {
		if err := validInterval(iv[0], iv[1]); err != nil {
			t.Errorf("validInterval(%d,%d) = %v, want nil", iv[0], iv[1], err)
		}
	}
	invalid := [][2]int64{{200, 100}, {100, 100}, {-1, 0}, {0, -1}, {-5, -1}}
	for _, iv := range invalid {
		err := validInterval(iv[0], iv[1])
		if err == nil {
			t.Errorf("validInterval(%d,%d) = nil, want error", iv[0], iv[1])
			continue
		}
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("validInterval(%d,%d) err = %v, want ErrInvalidArgument", iv[0], iv[1], err)
		}
	}
}
