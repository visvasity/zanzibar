// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"errors"
	"testing"
)

func expandTree(t *testing.T, svc *Service, object, relation string, asOf int64) UsersetNode {
	t.Helper()
	resp, err := svc.Expand(context.Background(), &ExpandRequest{Object: object, Relation: relation, AsOfUnixNano: asOf})
	if err != nil {
		t.Fatalf("Expand(%s#%s): %v", object, relation, err)
	}
	return resp.Tree
}

// childByKind returns the first direct child of the given kind, or a zero node.
func childByKind(n UsersetNode, kind UsersetNodeKind) (UsersetNode, bool) {
	for _, c := range n.Children {
		if c.Kind == kind {
			return c, true
		}
	}
	return UsersetNode{}, false
}

// anyTruncated reports whether any node in the tree is marked truncated.
func anyTruncated(n UsersetNode) bool {
	if n.Truncated {
		return true
	}
	for _, c := range n.Children {
		if anyTruncated(c) {
			return true
		}
	}
	return false
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestExpandThisLeaf(t *testing.T) {
	svc := newConfiguredService(t)
	grantAll(t, svc,
		tup("doc:readme", "owner", "user:bob@example.com"),
		tup("doc:readme", "owner", "user:alice@example.com"),
	)
	tree := expandTree(t, svc, "doc:readme", "owner", 0)
	if tree.Kind != NodeThis {
		t.Fatalf("owner tree kind = %q, want %q", tree.Kind, NodeThis)
	}
	if tree.Object != "doc:readme" {
		t.Errorf("Object = %q, want doc:readme", tree.Object)
	}
	want := []string{"user:alice@example.com", "user:bob@example.com"} // sorted
	if len(tree.Subjects) != 2 || tree.Subjects[0] != want[0] || tree.Subjects[1] != want[1] {
		t.Errorf("Subjects = %v, want %v", tree.Subjects, want)
	}
}

func TestExpandUsersetSubjectNotRecursed(t *testing.T) {
	svc := newConfiguredService(t)
	grantAll(t, svc,
		tup("group:eng", "member", "user:alice@example.com"),
		tup("doc:readme", "owner", "group:eng#member"),
	)
	tree := expandTree(t, svc, "doc:readme", "owner", 0)
	// The userset subject appears verbatim in the leaf, not expanded.
	if !contains(tree.Subjects, "group:eng#member") {
		t.Errorf("owner Subjects = %v, want to contain the userset subject verbatim", tree.Subjects)
	}
}

func TestExpandUnionStructure(t *testing.T) {
	svc := newConfiguredService(t)
	tree := expandTree(t, svc, "doc:readme", "viewer", 0)
	if tree.Kind != NodeUnion {
		t.Fatalf("viewer tree kind = %q, want %q", tree.Kind, NodeUnion)
	}
	if _, ok := childByKind(tree, NodeThis); !ok {
		t.Error("viewer union missing a _this child")
	}
	cu, ok := childByKind(tree, NodeComputedUserset)
	if !ok || cu.Relation != "editor" {
		t.Errorf("viewer union missing computed_userset(editor): %+v", cu)
	}
	// The computed_userset child expands the same object's editor relation.
	if len(cu.Children) != 1 || cu.Children[0].Object != "doc:readme" {
		t.Errorf("computed_userset child = %+v, want one child with Object doc:readme", cu.Children)
	}
	ttu, ok := childByKind(tree, NodeTupleToUserset)
	if !ok || ttu.Tupleset != "parent" || ttu.Relation != "viewer" {
		t.Errorf("viewer union missing tuple_to_userset(parent,viewer): %+v", ttu)
	}
}

func TestExpandTupleToUsersetPerParent(t *testing.T) {
	svc := newConfiguredService(t)
	grantAll(t, svc,
		tup("doc:readme", "parent", "folder:proj"),
		tup("doc:readme", "parent", "folder:archive"),
	)
	tree := expandTree(t, svc, "doc:readme", "viewer", 0)
	ttu, ok := childByKind(tree, NodeTupleToUserset)
	if !ok {
		t.Fatal("no tuple_to_userset node")
	}
	if len(ttu.Children) != 2 {
		t.Fatalf("tuple_to_userset children = %d, want 2", len(ttu.Children))
	}
	// Each parent subtree carries its own object, sorted (archive before proj).
	if ttu.Children[0].Object != "folder:archive" || ttu.Children[1].Object != "folder:proj" {
		t.Errorf("parent subtree objects = [%q,%q], want [folder:archive, folder:proj]",
			ttu.Children[0].Object, ttu.Children[1].Object)
	}
}

func TestExpandAsOfFiltersLeaf(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	_, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{{
		Op:                OpGrant,
		Tuple:             tup("doc:readme", "owner", "user:temp@example.com"),
		NotBeforeUnixNano: 1000, NotAfterUnixNano: 2000,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if in := expandTree(t, svc, "doc:readme", "owner", 1500); !contains(in.Subjects, "user:temp@example.com") {
		t.Errorf("in-window Subjects = %v, want temp present", in.Subjects)
	}
	if out := expandTree(t, svc, "doc:readme", "owner", 2500); contains(out.Subjects, "user:temp@example.com") {
		t.Errorf("out-of-window Subjects = %v, want temp absent", out.Subjects)
	}
}

func TestExpandCycleTruncated(t *testing.T) {
	svc := newConfiguredService(t)
	grantAll(t, svc,
		tup("folder:a", "parent", "folder:b"),
		tup("folder:b", "parent", "folder:a"),
	)
	tree := expandTree(t, svc, "folder:a", "viewer", 0)
	if !anyTruncated(tree) {
		t.Error("expected a Truncated node from the parent cycle, found none")
	}
}

func TestExpandUnregisteredEmpty(t *testing.T) {
	svc := newConfiguredService(t)
	tree := expandTree(t, svc, "ghost:x", "viewer", 0)
	if tree.Kind != NodeThis || len(tree.Subjects) != 0 || tree.Truncated {
		t.Errorf("unregistered expand = %+v, want empty NodeThis", tree)
	}
}

func TestExpandInvalidInputs(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	if _, err := svc.Expand(ctx, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("nil = %v, want ErrInvalidArgument", err)
	}
	if _, err := svc.Expand(ctx, &ExpandRequest{Object: "nocolon", Relation: "viewer"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad object = %v, want ErrInvalidArgument", err)
	}
}
