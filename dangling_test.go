// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"testing"
)

// TestDanglingReferencesResolveEmpty verifies that after an object is deleted
// (all tuples on it removed), references to it left elsewhere as a subject — a
// userset subject and a bare-object (parent) subject — resolve to the empty set
// with no error across every read operation (§8.1).
func TestDanglingReferencesResolveEmpty(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	grantAll(t, svc,
		tup("group:eng", "member", "user:alice@example.com"),
		tup("doc:readme", "viewer", "group:eng#member"), // userset reference
		tup("folder:proj", "viewer", "user:bob@example.com"),
		tup("doc:readme", "parent", "folder:proj"), // bare-object reference
	)

	// Sanity: both indirection paths resolve before deletion.
	if !checkBool(t, svc, "doc:readme", "viewer", "user:alice@example.com", 0) {
		t.Fatal("precondition: alice should view doc:readme via group")
	}
	if !checkBool(t, svc, "doc:readme", "viewer", "user:bob@example.com", 0) {
		t.Fatal("precondition: bob should view doc:readme via parent folder")
	}

	// Delete both referenced objects (removes their own tuples only).
	if _, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{
		{Op: OpDelete, Tuple: Tuple{Object: "group:eng"}},
		{Op: OpDelete, Tuple: Tuple{Object: "folder:proj"}},
	}}); err != nil {
		t.Fatal(err)
	}

	// The references on doc:readme are now dangling but still stored.
	if recs := readAll(t, svc, ReadRequest{Object: "doc:readme"}, 0); len(recs) != 2 {
		t.Fatalf("doc:readme should still have its 2 dangling tuples, got %d", len(recs))
	}

	// Check: false, and crucially NO error, for every subject.
	for _, u := range []string{"user:alice@example.com", "user:bob@example.com", "user:carol@example.com"} {
		resp, err := svc.Check(ctx, &CheckRequest{Object: "doc:readme", Relation: "viewer", Subject: u})
		if err != nil {
			t.Errorf("Check(%s) returned error on dangling refs: %v", u, err)
			continue
		}
		if resp.Allowed {
			t.Errorf("Check(%s) = allowed, want denied (references are dangling)", u)
		}
	}

	// Expand must not error, and the dangling subtrees must be empty.
	exp, err := svc.Expand(ctx, &ExpandRequest{Object: "doc:readme", Relation: "viewer"})
	if err != nil {
		t.Fatalf("Expand returned error on dangling refs: %v", err)
	}
	if flattenExpandUsers(exp.Tree) != 0 {
		t.Errorf("Expand tree should contain no resolved users, tree=%+v", exp.Tree)
	}

	// ListUsers / ListObjects must not error and must be empty.
	lu, err := svc.ListUsers(ctx, &ListUsersRequest{Object: "doc:readme", Relation: "viewer"})
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}
	if len(lu.Users) != 0 {
		t.Errorf("ListUsers = %v, want empty", lu.Users)
	}
	for _, u := range []string{"user:alice@example.com", "user:bob@example.com"} {
		lo, err := svc.ListObjects(ctx, &ListObjectsRequest{Namespace: "doc", Relation: "viewer", Subject: u})
		if err != nil {
			t.Fatalf("ListObjects(%s) returned error: %v", u, err)
		}
		if len(lo.ObjectIDs) != 0 {
			t.Errorf("ListObjects(%s) = %v, want empty", u, lo.ObjectIDs)
		}
	}
}

// flattenExpandUsers counts the "user:" leaves that survived in an expand tree.
func flattenExpandUsers(n UsersetNode) int {
	count := 0
	for _, s := range n.Subjects {
		if len(s) >= 5 && s[:5] == "user:" {
			count++
		}
	}
	for _, c := range n.Children {
		count += flattenExpandUsers(c)
	}
	return count
}

// TestDanglingUndeclaredRelationResolvesEmpty verifies the other way a reference
// can dangle: the referenced relation is removed from the config while tuples
// still point at it. Evaluation must deny (fail-closed) without error.
func TestDanglingUndeclaredRelationResolvesEmpty(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	grantAll(t, svc,
		tup("group:eng", "member", "user:alice@example.com"),
		tup("doc:readme", "viewer", "group:eng#member"),
	)
	if !checkBool(t, svc, "doc:readme", "viewer", "user:alice@example.com", 0) {
		t.Fatal("precondition: alice should view via group")
	}

	// Rewrite the group config so that "member" no longer exists.
	cur, err := svc.ReadConfig(ctx, "group")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteConfig(ctx, &NamespaceConfig{
		Namespace: "group",
		Version:   cur.Version,
		Relations: map[string]Rewrite{"other": {This: &This{}}},
	}); err != nil {
		t.Fatal(err)
	}

	// The userset subject now names an undeclared relation. Check denies, no error.
	resp, err := svc.Check(ctx, &CheckRequest{Object: "doc:readme", Relation: "viewer", Subject: "user:alice@example.com"})
	if err != nil {
		t.Fatalf("Check errored on undeclared relation: %v", err)
	}
	if resp.Allowed {
		t.Error("Check = allowed, want denied (referenced relation undeclared)")
	}
	// And directly checking the now-undeclared relation is also a clean deny.
	if r, err := svc.Check(ctx, &CheckRequest{Object: "group:eng", Relation: "member", Subject: "user:alice@example.com"}); err != nil || r.Allowed {
		t.Errorf("Check(group:eng#member) = (%v, %v), want (denied, nil)", r, err)
	}
}
