// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"errors"
	"testing"
)

// buildListFixture creates a service with a rich graph: nested groups, relation
// implication, single- and multi-level parent inheritance, and a time-boxed
// grant.
func buildListFixture(t *testing.T) *Service {
	t.Helper()
	ctx := context.Background()
	svc := newConfiguredService(t)
	grantAll(t, svc,
		tup("group:eng", "member", "user:alice@example.com"),
		tup("group:eng", "member", "group:backend#member"),
		tup("group:backend", "member", "user:bob@example.com"),
		tup("doc:d1", "owner", "user:alice@example.com"),
		tup("doc:d1", "viewer", "group:eng#member"),
		tup("doc:d2", "viewer", "user:bob@example.com"),
		tup("doc:d2", "parent", "folder:f1"),
		tup("folder:f1", "viewer", "user:carol@example.com"),
		tup("doc:d3", "parent", "folder:f2"),
		tup("folder:f2", "parent", "folder:root"),
		tup("folder:root", "viewer", "user:dave@example.com"),
	)
	// Time-boxed viewer on doc:d4 for alice, active in [1000, 2000).
	if _, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{{
		Op: OpGrant, Tuple: tup("doc:d4", "viewer", "user:alice@example.com"),
		NotBeforeUnixNano: 1000, NotAfterUnixNano: 2000,
	}}}); err != nil {
		t.Fatal(err)
	}
	return svc
}

// --- brute-force references -----------------------------------------------

func objectsInNamespace(t *testing.T, svc *Service, namespace string) []string {
	t.Helper()
	recs := readAll(t, svc, ReadRequest{}, 0)
	set := map[string]bool{}
	for _, r := range recs {
		if namespaceOf(r.Tuple.Object) == namespace {
			set[r.Tuple.Object] = true
		}
	}
	return sortedKeysOf(set)
}

func allUserSubjects(t *testing.T, svc *Service) []string {
	t.Helper()
	recs := readAll(t, svc, ReadRequest{}, 0)
	set := map[string]bool{}
	for _, r := range recs {
		sub, err := parseSubject(r.Tuple.Subject)
		if err == nil && sub.kind == kindUser && !sub.wildcard {
			set[r.Tuple.Subject] = true
		}
	}
	return sortedKeysOf(set)
}

func bruteForceObjects(t *testing.T, svc *Service, namespace, relation, subject string, asOf int64) []string {
	t.Helper()
	var ids []string
	for _, obj := range objectsInNamespace(t, svc, namespace) {
		if checkBool(t, svc, obj, relation, subject, asOf) {
			ids = append(ids, obj[len(namespace)+1:])
		}
	}
	return ids // objectsInNamespace is sorted, so ids are too
}

func bruteForceUsers(t *testing.T, svc *Service, object, relation string, asOf int64) []string {
	t.Helper()
	var users []string
	for _, u := range allUserSubjects(t, svc) {
		if checkBool(t, svc, object, relation, u, asOf) {
			users = append(users, u)
		}
	}
	return users
}

// --- pagination aggregators -----------------------------------------------

func listAllObjects(t *testing.T, svc *Service, req ListObjectsRequest, pageSize int) []string {
	t.Helper()
	ctx := context.Background()
	req.PageSize = pageSize
	var out []string
	seen := map[string]bool{}
	for {
		resp, err := svc.ListObjects(ctx, &req)
		if err != nil {
			t.Fatalf("ListObjects: %v", err)
		}
		for _, id := range resp.ObjectIDs {
			if seen[id] {
				t.Fatalf("duplicate object id across pages: %s", id)
			}
			seen[id] = true
			out = append(out, id)
		}
		if resp.NextPageToken == "" {
			break
		}
		req.PageToken = resp.NextPageToken
	}
	return out
}

func listAllUsers(t *testing.T, svc *Service, req ListUsersRequest, pageSize int) []string {
	t.Helper()
	ctx := context.Background()
	req.PageSize = pageSize
	var out []string
	seen := map[string]bool{}
	for {
		resp, err := svc.ListUsers(ctx, &req)
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		for _, u := range resp.Users {
			if seen[u] {
				t.Fatalf("duplicate user across pages: %s", u)
			}
			seen[u] = true
			out = append(out, u)
		}
		if resp.NextPageToken == "" {
			break
		}
		req.PageToken = resp.NextPageToken
	}
	return out
}

// --- ListObjects tests ----------------------------------------------------

func TestListObjectsMatchesBruteForce(t *testing.T) {
	svc := buildListFixture(t)
	subjects := []string{
		"user:alice@example.com",
		"user:bob@example.com",
		"user:carol@example.com",
		"user:dave@example.com",
		"group:eng#member",
	}
	for _, subj := range subjects {
		for _, asOf := range []int64{0, 1500, 2500} {
			want := bruteForceObjects(t, svc, "doc", "viewer", subj, asOf)
			got := listAllObjects(t, svc, ListObjectsRequest{Namespace: "doc", Relation: "viewer", Subject: subj, AsOfUnixNano: asOf}, 0)
			if !equalStrings(got, want) {
				t.Errorf("ListObjects(doc,viewer,%s,asOf=%d):\n got %v\nwant %v", subj, asOf, got, want)
			}
		}
	}
}

func TestListObjectsExplicit(t *testing.T) {
	svc := buildListFixture(t)
	// alice: d1 via owner-implies-viewer and via group; d4 only inside its window.
	if got := listAllObjects(t, svc, ListObjectsRequest{Namespace: "doc", Relation: "viewer", Subject: "user:alice@example.com", AsOfUnixNano: 0}, 0); !equalStrings(got, []string{"d1"}) {
		t.Errorf("alice@0 = %v, want [d1]", got)
	}
	if got := listAllObjects(t, svc, ListObjectsRequest{Namespace: "doc", Relation: "viewer", Subject: "user:alice@example.com", AsOfUnixNano: 1500}, 0); !equalStrings(got, []string{"d1", "d4"}) {
		t.Errorf("alice@1500 = %v, want [d1 d4]", got)
	}
	// bob: d1 (nested group) and d2 (direct).
	if got := listAllObjects(t, svc, ListObjectsRequest{Namespace: "doc", Relation: "viewer", Subject: "user:bob@example.com"}, 0); !equalStrings(got, []string{"d1", "d2"}) {
		t.Errorf("bob = %v, want [d1 d2]", got)
	}
	// dave inherits d3 through a two-level folder chain.
	if got := listAllObjects(t, svc, ListObjectsRequest{Namespace: "doc", Relation: "viewer", Subject: "user:dave@example.com"}, 0); !equalStrings(got, []string{"d3"}) {
		t.Errorf("dave = %v, want [d3]", got)
	}
}

func TestListObjectsPagination(t *testing.T) {
	svc := buildListFixture(t)
	full := listAllObjects(t, svc, ListObjectsRequest{Namespace: "doc", Relation: "viewer", Subject: "user:bob@example.com"}, 0)
	paged := listAllObjects(t, svc, ListObjectsRequest{Namespace: "doc", Relation: "viewer", Subject: "user:bob@example.com"}, 1)
	if !equalStrings(full, paged) {
		t.Errorf("paged %v != full %v", paged, full)
	}
}

func TestListObjectsInvalid(t *testing.T) {
	ctx := context.Background()
	svc := buildListFixture(t)
	if _, err := svc.ListObjects(ctx, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("nil = %v", err)
	}
	if _, err := svc.ListObjects(ctx, &ListObjectsRequest{Namespace: "doc", Relation: "viewer", Subject: "user:*"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("wildcard = %v, want ErrInvalidArgument", err)
	}
	if _, err := svc.ListObjects(ctx, &ListObjectsRequest{Namespace: "doc", Relation: "viewer", Subject: "user:bad"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad subject = %v", err)
	}
}

// --- ListUsers tests ------------------------------------------------------

func TestListUsersMatchesBruteForce(t *testing.T) {
	svc := buildListFixture(t)
	objects := []struct {
		object, relation string
	}{
		{"doc:d1", "viewer"},
		{"doc:d1", "owner"},
		{"doc:d2", "viewer"},
		{"doc:d3", "viewer"},
		{"group:eng", "member"},
	}
	for _, o := range objects {
		for _, asOf := range []int64{0, 1500} {
			want := bruteForceUsers(t, svc, o.object, o.relation, asOf)
			got := listAllUsers(t, svc, ListUsersRequest{Object: o.object, Relation: o.relation, AsOfUnixNano: asOf}, 0)
			if !equalStrings(got, want) {
				t.Errorf("ListUsers(%s,%s,asOf=%d):\n got %v\nwant %v", o.object, o.relation, asOf, got, want)
			}
		}
	}
}

func TestListUsersExplicit(t *testing.T) {
	svc := buildListFixture(t)
	// d1 viewer: alice (owner) + alice,bob (group eng, nested backend).
	if got := listAllUsers(t, svc, ListUsersRequest{Object: "doc:d1", Relation: "viewer"}, 0); !equalStrings(got, []string{"user:alice@example.com", "user:bob@example.com"}) {
		t.Errorf("d1 viewer = %v, want [alice bob]", got)
	}
	// d2 viewer: bob (direct) + carol (via parent folder).
	if got := listAllUsers(t, svc, ListUsersRequest{Object: "doc:d2", Relation: "viewer"}, 0); !equalStrings(got, []string{"user:bob@example.com", "user:carol@example.com"}) {
		t.Errorf("d2 viewer = %v, want [bob carol]", got)
	}
}

func TestListUsersTimeBoxed(t *testing.T) {
	svc := buildListFixture(t)
	if got := listAllUsers(t, svc, ListUsersRequest{Object: "doc:d4", Relation: "viewer", AsOfUnixNano: 1500}, 0); !equalStrings(got, []string{"user:alice@example.com"}) {
		t.Errorf("d4 viewer @1500 = %v, want [alice]", got)
	}
	if got := listAllUsers(t, svc, ListUsersRequest{Object: "doc:d4", Relation: "viewer", AsOfUnixNano: 2500}, 0); len(got) != 0 {
		t.Errorf("d4 viewer @2500 = %v, want empty", got)
	}
}

func TestListUsersIntersectionExclusion(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	cfg := &NamespaceConfig{Namespace: "res", Relations: map[string]Rewrite{
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
	if _, err := svc.WriteConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	grantAll(t, svc,
		tup("res:x", "a", "user:alice@example.com"),
		tup("res:x", "b", "user:alice@example.com"),
		tup("res:x", "a", "user:bob@example.com"),
	)
	if got := listAllUsers(t, svc, ListUsersRequest{Object: "res:x", Relation: "both"}, 0); !equalStrings(got, []string{"user:alice@example.com"}) {
		t.Errorf("both = %v, want [alice]", got)
	}
	if got := listAllUsers(t, svc, ListUsersRequest{Object: "res:x", Relation: "only_a"}, 0); !equalStrings(got, []string{"user:bob@example.com"}) {
		t.Errorf("only_a = %v, want [bob]", got)
	}
}

func TestListUsersCycleTerminates(t *testing.T) {
	svc := newConfiguredService(t)
	grantAll(t, svc,
		tup("folder:a", "parent", "folder:b"),
		tup("folder:b", "parent", "folder:a"),
	)
	// Must terminate and return no users.
	if got := listAllUsers(t, svc, ListUsersRequest{Object: "folder:a", Relation: "viewer"}, 0); len(got) != 0 {
		t.Errorf("cyclic ListUsers = %v, want empty", got)
	}
}

func TestListUsersInvalid(t *testing.T) {
	ctx := context.Background()
	svc := buildListFixture(t)
	if _, err := svc.ListUsers(ctx, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("nil = %v", err)
	}
	if _, err := svc.ListUsers(ctx, &ListUsersRequest{Object: "nocolon", Relation: "viewer"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad object = %v", err)
	}
}
