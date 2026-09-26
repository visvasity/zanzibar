// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"testing"
)

// buildConformanceFixture installs the canonical namespaces and a fixed tuple
// set exercising every rewrite kind, groups, nesting, single- and multi-level
// inheritance, intersection/exclusion, and a time-boxed grant.
func buildConformanceFixture(t *testing.T) *Service {
	t.Helper()
	ctx := context.Background()
	svc := newTestService(t)

	res := &NamespaceConfig{Namespace: "res", Relations: map[string]Rewrite{
		"a": {This: &This{}},
		"b": {This: &This{}},
		"both": {Intersection: &SetOperation{Children: []Rewrite{
			{ComputedUserset: &ComputedUserset{Relation: "a"}},
			{ComputedUserset: &ComputedUserset{Relation: "b"}},
		}}},
		"only_a": {Exclusion: &Exclusion{
			Base:     &Rewrite{ComputedUserset: &ComputedUserset{Relation: "a"}},
			Subtract: &Rewrite{ComputedUserset: &ComputedUserset{Relation: "b"}},
		}},
	}}
	payments := &NamespaceConfig{Namespace: "payments", Relations: map[string]Rewrite{"staff": {This: &This{}}}}

	for _, cfg := range []*NamespaceConfig{sampleDocConfig(), folderConfig(), groupConfig(), res, payments} {
		if _, err := svc.WriteConfig(ctx, cfg); err != nil {
			t.Fatalf("WriteConfig(%s): %v", cfg.Namespace, err)
		}
	}

	grantAll(t, svc,
		// relation implication + group
		tup("doc:d1", "owner", "user:alice@example.com"),
		tup("doc:d1", "viewer", "group:eng#member"),
		tup("group:eng", "member", "user:bob@example.com"),
		tup("group:eng", "member", "group:back#member"),
		tup("group:back", "member", "user:carol@example.com"),
		// single- and multi-level parent inheritance
		tup("doc:d2", "parent", "folder:f1"),
		tup("folder:f1", "viewer", "user:dave@example.com"),
		tup("folder:f1", "parent", "folder:root"),
		tup("folder:root", "viewer", "user:erin@example.com"),
		// intersection / exclusion
		tup("res:x", "a", "user:alice@example.com"),
		tup("res:x", "b", "user:alice@example.com"),
		tup("res:x", "a", "user:bob@example.com"),
		// staff override
		tup("payments:all", "staff", "group:admin#member"),
		tup("group:admin", "member", "user:root@example.com"),
	)
	// time-boxed grant
	if _, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{{
		Op: OpGrant, Tuple: tup("doc:d3", "viewer", "user:frank@example.com"),
		NotBeforeUnixNano: 1000, NotAfterUnixNano: 2000,
	}}}); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestConformanceGolden(t *testing.T) {
	svc := buildConformanceFixture(t)
	cases := []struct {
		name                      string
		object, relation, subject string
		asOf                      int64
		want                      bool
	}{
		{"direct owner", "doc:d1", "owner", "user:alice@example.com", 0, true},
		{"owner implies editor", "doc:d1", "editor", "user:alice@example.com", 0, true},
		{"owner implies viewer", "doc:d1", "viewer", "user:alice@example.com", 0, true},
		{"group member viewer", "doc:d1", "viewer", "user:bob@example.com", 0, true},
		{"nested group viewer", "doc:d1", "viewer", "user:carol@example.com", 0, true},
		{"non-member deny", "doc:d1", "viewer", "user:dave@example.com", 0, false},
		{"parent inheritance", "doc:d2", "viewer", "user:dave@example.com", 0, true},
		{"transitive inheritance", "doc:d2", "viewer", "user:erin@example.com", 0, true},
		{"inheritance non-member", "doc:d2", "viewer", "user:bob@example.com", 0, false},
		{"intersection member", "res:x", "both", "user:alice@example.com", 0, true},
		{"intersection non-member", "res:x", "both", "user:bob@example.com", 0, false},
		{"exclusion member", "res:x", "only_a", "user:bob@example.com", 0, true},
		{"exclusion excluded", "res:x", "only_a", "user:alice@example.com", 0, false},
		{"time in window", "doc:d3", "viewer", "user:frank@example.com", 1500, true},
		{"time no asof", "doc:d3", "viewer", "user:frank@example.com", 0, false},
		{"time after window", "doc:d3", "viewer", "user:frank@example.com", 2500, false},
		{"staff via group", "payments:all", "staff", "user:root@example.com", 0, true},
		{"staff outsider", "payments:all", "staff", "user:mallory@example.com", 0, false},
		{"unregistered namespace", "ghost:x", "viewer", "user:alice@example.com", 0, false},
		{"undeclared relation", "doc:d1", "ghostrel", "user:alice@example.com", 0, false},
		{"exact userset subject", "doc:d1", "viewer", "group:eng#member", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkBool(t, svc, tc.object, tc.relation, tc.subject, tc.asOf); got != tc.want {
				t.Errorf("Check(%s#%s@%s, asOf=%d) = %v, want %v", tc.object, tc.relation, tc.subject, tc.asOf, got, tc.want)
			}
		})
	}
}
