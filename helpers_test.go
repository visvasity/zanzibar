// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"testing"

	"github.com/visvasity/kv"
	"github.com/visvasity/kvmemdb"
)

// newTestDB returns a fresh in-memory kv.Database for tests.
func newTestDB() kv.Database {
	return kv.DatabaseFrom(kvmemdb.New())
}

// newTestService constructs a Service over a fresh in-memory database, failing
// the test on construction error.
func newTestService(t *testing.T, opts ...Option) *Service {
	t.Helper()
	svc, err := New(newTestDB(), opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc
}

// groupConfig is a minimal group namespace with a member relation.
func groupConfig() *NamespaceConfig {
	return &NamespaceConfig{
		Namespace: "group",
		Relations: map[string]Rewrite{"member": {This: &This{}}},
	}
}

// folderConfig is a folder namespace with viewer inheriting from its parent.
func folderConfig() *NamespaceConfig {
	return &NamespaceConfig{
		Namespace: "folder",
		Relations: map[string]Rewrite{
			"parent": {This: &This{}},
			"viewer": {Union: &SetOperation{Children: []Rewrite{
				{This: &This{}},
				{TupleToUserset: &TupleToUserset{Tupleset: "parent", ComputedUserset: "viewer"}},
			}}},
		},
	}
}

// newConfiguredService returns a Service with the canonical doc, folder, and
// group namespaces already written.
func newConfiguredService(t *testing.T, opts ...Option) *Service {
	t.Helper()
	svc := newTestService(t, opts...)
	ctx := context.Background()
	for _, cfg := range []*NamespaceConfig{sampleDocConfig(), folderConfig(), groupConfig()} {
		if _, err := svc.WriteConfig(ctx, cfg); err != nil {
			t.Fatalf("WriteConfig(%s): %v", cfg.Namespace, err)
		}
	}
	return svc
}
