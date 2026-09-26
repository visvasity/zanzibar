// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"testing"
)

func TestGCRemovesDanglingReferences(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t, WithDeletionLog())
	grantAll(t, svc,
		tup("group:eng", "member", "user:alice@example.com"),
		tup("doc:readme", "viewer", "group:eng#member"), // userset ref to group:eng
		tup("doc:notes", "viewer", "group:eng#member"),  // another userset ref
		tup("folder:proj", "viewer", "user:bob@example.com"),
		tup("doc:readme", "parent", "folder:proj"), // bare-object ref to folder:proj
	)

	// Delete both referenced objects (records tombstones).
	if _, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{
		{Op: OpDelete, Tuple: Tuple{Object: "group:eng"}},
		{Op: OpDelete, Tuple: Tuple{Object: "folder:proj"}},
	}}); err != nil {
		t.Fatal(err)
	}

	// Before GC the dangling references are still stored.
	if n := len(readAll(t, svc, ReadRequest{Object: "doc:readme"}, 0)); n != 2 {
		t.Fatalf("doc:readme has %d tuples before GC, want 2", n)
	}
	if n := len(readAll(t, svc, ReadRequest{Object: "doc:notes"}, 0)); n != 1 {
		t.Fatalf("doc:notes has %d tuples before GC, want 1", n)
	}

	// Collect: two tombstoned objects processed.
	n, err := svc.CollectGarbage(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("CollectGarbage processed %d, want 2", n)
	}
	// Idempotent: nothing left to do.
	if again, _ := svc.CollectGarbage(ctx, 0); again != 0 {
		t.Errorf("second CollectGarbage processed %d, want 0", again)
	}

	// The dangling references are gone.
	if n := len(readAll(t, svc, ReadRequest{Object: "doc:readme"}, 0)); n != 0 {
		t.Errorf("doc:readme has %d tuples after GC, want 0", n)
	}
	if n := len(readAll(t, svc, ReadRequest{Object: "doc:notes"}, 0)); n != 0 {
		t.Errorf("doc:notes has %d tuples after GC, want 0", n)
	}
	checkIndexInvariant(t, svc)
}

func TestGCPreservesRevivedObject(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t, WithDeletionLog())
	grantAll(t, svc,
		tup("group:eng", "member", "user:alice@example.com"),
		tup("doc:x", "viewer", "group:eng#member"),
	)
	// Delete the group (tombstone), then re-create it before the collector runs.
	if _, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{{Op: OpDelete, Tuple: Tuple{Object: "group:eng"}}}}); err != nil {
		t.Fatal(err)
	}
	grantAll(t, svc, tup("group:eng", "member", "user:carol@example.com")) // revival clears tombstone

	// The tombstone is gone, so GC has nothing to sweep.
	if n, err := svc.CollectGarbage(ctx, 0); err != nil || n != 0 {
		t.Errorf("CollectGarbage = (%d, %v), want (0, nil) after revival", n, err)
	}
	// The reference is preserved and resolves to the revived membership.
	if _, ok := getForwardMeta(t, svc, "doc:x", "viewer", "group:eng#member"); !ok {
		t.Error("reference to revived group should be preserved")
	}
	if !checkBool(t, svc, "doc:x", "viewer", "user:carol@example.com", 0) {
		t.Error("carol should view doc:x via the revived group")
	}
}

func TestGCDisabledIsNoOp(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t) // deletion log OFF
	grantAll(t, svc,
		tup("group:eng", "member", "user:alice@example.com"),
		tup("doc:x", "viewer", "group:eng#member"),
	)
	if _, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{{Op: OpDelete, Tuple: Tuple{Object: "group:eng"}}}}); err != nil {
		t.Fatal(err)
	}
	// No tombstones were recorded, so the collector does nothing.
	if n, err := svc.CollectGarbage(ctx, 0); err != nil || n != 0 {
		t.Errorf("CollectGarbage = (%d, %v), want (0, nil) when disabled", n, err)
	}
	// The dangling reference remains (inert, but not reclaimed).
	if _, ok := getForwardMeta(t, svc, "doc:x", "viewer", "group:eng#member"); !ok {
		t.Error("dangling reference should remain when GC is disabled")
	}
}

func TestGCBatchLimit(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t, WithDeletionLog())
	// Create and delete several groups, each referenced by a doc.
	var muts []Mutation
	for _, id := range []string{"g0", "g1", "g2", "g3"} {
		muts = append(muts,
			Mutation{Op: OpGrant, Tuple: Tuple{Object: "group:" + id, Relation: "member", Subject: "user:a@x.com"}},
			Mutation{Op: OpGrant, Tuple: Tuple{Object: "doc:" + id, Relation: "viewer", Subject: "group:" + id + "#member"}},
		)
	}
	if _, err := svc.Write(ctx, &WriteRequest{Mutations: muts}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"g0", "g1", "g2", "g3"} {
		if _, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{{Op: OpDelete, Tuple: Tuple{Object: "group:" + id}}}}); err != nil {
			t.Fatal(err)
		}
	}
	// A bounded batch processes at most max, and repeated calls drain the rest.
	total := 0
	for {
		n, err := svc.CollectGarbage(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		if n > 2 {
			t.Fatalf("batch returned %d, want <= 2", n)
		}
		total += n
		if n == 0 {
			break
		}
	}
	if total != 4 {
		t.Errorf("drained %d tombstones, want 4", total)
	}
}
