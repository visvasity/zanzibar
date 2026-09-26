// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// checkBool runs a Check and returns the boolean decision, failing on error.
func checkBool(t *testing.T, svc *Service, object, relation, subject string, asOf int64) bool {
	t.Helper()
	resp, err := svc.Check(context.Background(), &CheckRequest{
		Object: object, Relation: relation, Subject: subject, AsOfUnixNano: asOf,
	})
	if err != nil {
		t.Fatalf("Check(%s#%s@%s): %v", object, relation, subject, err)
	}
	return resp.Allowed
}

// grantAll grants a list of tuples.
func grantAll(t *testing.T, svc *Service, tuples ...Tuple) {
	t.Helper()
	muts := make([]Mutation, len(tuples))
	for i, tup := range tuples {
		muts[i] = Mutation{Op: OpGrant, Tuple: tup}
	}
	if _, err := svc.Write(context.Background(), &WriteRequest{Mutations: muts}); err != nil {
		t.Fatalf("grantAll: %v", err)
	}
}

func tup(object, relation, subject string) Tuple {
	return Tuple{Object: object, Relation: relation, Subject: subject}
}

func TestCheckDirect(t *testing.T) {
	svc := newConfiguredService(t)
	grantAll(t, svc, tup("doc:readme", "viewer", "user:alice@example.com"))
	if !checkBool(t, svc, "doc:readme", "viewer", "user:alice@example.com", 0) {
		t.Error("alice should be a viewer")
	}
	if checkBool(t, svc, "doc:readme", "viewer", "user:bob@example.com", 0) {
		t.Error("bob should not be a viewer")
	}
}

func TestCheckUnregisteredAndUndeclaredDeny(t *testing.T) {
	svc := newConfiguredService(t)
	if checkBool(t, svc, "ghost:x", "viewer", "user:a@b.com", 0) {
		t.Error("unregistered namespace must deny")
	}
	if checkBool(t, svc, "doc:readme", "ghostrel", "user:a@b.com", 0) {
		t.Error("undeclared relation must deny")
	}
}

func TestCheckComputedUserset(t *testing.T) {
	svc := newConfiguredService(t)
	// owner implies editor implies viewer.
	grantAll(t, svc, tup("doc:readme", "owner", "user:alice@example.com"))
	for _, rel := range []string{"owner", "editor", "viewer"} {
		if !checkBool(t, svc, "doc:readme", rel, "user:alice@example.com", 0) {
			t.Errorf("owner alice should satisfy %s", rel)
		}
	}
}

func TestCheckGroupMembership(t *testing.T) {
	svc := newConfiguredService(t)
	grantAll(t, svc,
		tup("group:eng", "member", "user:alice@example.com"),
		tup("doc:readme", "viewer", "group:eng#member"),
	)
	if !checkBool(t, svc, "doc:readme", "viewer", "user:alice@example.com", 0) {
		t.Error("group member alice should be a viewer")
	}
}

func TestCheckNestedGroups(t *testing.T) {
	svc := newConfiguredService(t)
	grantAll(t, svc,
		tup("group:eng", "member", "group:eng-backend#member"),
		tup("group:eng-backend", "member", "user:bob@example.com"),
		tup("doc:readme", "viewer", "group:eng#member"),
	)
	if !checkBool(t, svc, "doc:readme", "viewer", "user:bob@example.com", 0) {
		t.Error("nested group member bob should be a viewer")
	}
}

func TestCheckParentInheritance(t *testing.T) {
	svc := newConfiguredService(t)
	grantAll(t, svc,
		tup("folder:proj", "viewer", "user:alice@example.com"),
		tup("doc:readme", "parent", "folder:proj"),
	)
	if !checkBool(t, svc, "doc:readme", "viewer", "user:alice@example.com", 0) {
		t.Error("alice should inherit viewer via parent folder")
	}
}

func TestCheckTransitiveParentInheritance(t *testing.T) {
	svc := newConfiguredService(t)
	// folder:root -> folder:mid -> doc:readme (parent chain), viewer at root.
	grantAll(t, svc,
		tup("folder:root", "viewer", "user:alice@example.com"),
		tup("folder:mid", "parent", "folder:root"),
		tup("doc:readme", "parent", "folder:mid"),
	)
	if !checkBool(t, svc, "doc:readme", "viewer", "user:alice@example.com", 0) {
		t.Error("alice should inherit viewer transitively up the folder chain")
	}
}

func TestCheckIntersectionExclusion(t *testing.T) {
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
		tup("res:x", "a", "user:bob@example.com"), // bob has a but not b
	)
	// alice has both a and b.
	if !checkBool(t, svc, "res:x", "both", "user:alice@example.com", 0) {
		t.Error("alice should be in both")
	}
	if checkBool(t, svc, "res:x", "only_a", "user:alice@example.com", 0) {
		t.Error("alice has b, so should NOT be in only_a")
	}
	// bob has a only.
	if checkBool(t, svc, "res:x", "both", "user:bob@example.com", 0) {
		t.Error("bob lacks b, should not be in both")
	}
	if !checkBool(t, svc, "res:x", "only_a", "user:bob@example.com", 0) {
		t.Error("bob has a and not b, should be in only_a")
	}
}

func TestCheckCycleTerminates(t *testing.T) {
	svc := newConfiguredService(t)
	// folder:a <-> folder:b parent loop; nobody granted viewer.
	grantAll(t, svc,
		tup("folder:a", "parent", "folder:b"),
		tup("folder:b", "parent", "folder:a"),
	)
	// Must terminate and deny, not hang.
	if checkBool(t, svc, "folder:a", "viewer", "user:alice@example.com", 0) {
		t.Error("cycle should not grant membership")
	}
}

func TestCheckDepthExceeded(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t, WithMaxDepth(3))
	// Build a parent chain longer than the depth limit: f0 -> f1 -> ... -> f5.
	var muts []Mutation
	for i := 0; i < 5; i++ {
		muts = append(muts, Mutation{Op: OpGrant, Tuple: tup(
			fmt.Sprintf("folder:f%d", i), "parent", fmt.Sprintf("folder:f%d", i+1))})
	}
	muts = append(muts, Mutation{Op: OpGrant, Tuple: tup("folder:f5", "viewer", "user:alice@example.com")})
	if _, err := svc.Write(ctx, &WriteRequest{Mutations: muts}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Check(ctx, &CheckRequest{Object: "folder:f0", Relation: "viewer", Subject: "user:alice@example.com"})
	if !errors.Is(err, ErrDepthExceeded) {
		t.Errorf("deep chain Check err = %v, want ErrDepthExceeded", err)
	}
}

func TestCheckTimeBoxedDirect(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	_, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{{
		Op:                OpGrant,
		Tuple:             tup("doc:readme", "viewer", "user:temp@example.com"),
		NotBeforeUnixNano: 1000, NotAfterUnixNano: 2000,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		asOf int64
		want bool
	}{
		{1500, true},  // in window
		{500, false},  // before
		{2500, false}, // after
		{1000, true},  // lower bound inclusive
		{2000, false}, // upper bound exclusive
		{0, false},    // no AsOf supplied -> interval-bearing tuple inactive
	}
	for _, c := range cases {
		if got := checkBool(t, svc, "doc:readme", "viewer", "user:temp@example.com", c.asOf); got != c.want {
			t.Errorf("AsOf=%d: got %v, want %v", c.asOf, got, c.want)
		}
	}
}

func TestCheckTimeBoxedGroupMembership(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	// Membership is time-boxed; the doc grant to the group is permanent.
	_, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{
		{Op: OpGrant, Tuple: tup("doc:readme", "viewer", "group:eng#member")},
		{Op: OpGrant, Tuple: tup("group:eng", "member", "user:alice@example.com"),
			NotBeforeUnixNano: 1000, NotAfterUnixNano: 2000},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !checkBool(t, svc, "doc:readme", "viewer", "user:alice@example.com", 1500) {
		t.Error("alice should be a viewer during her membership window")
	}
	if checkBool(t, svc, "doc:readme", "viewer", "user:alice@example.com", 3000) {
		t.Error("alice should not be a viewer after her membership expires")
	}
}

func TestCheckTimeBoxedParentEdge(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	// A time-boxed parent edge stops inheritance outside its window.
	_, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{
		{Op: OpGrant, Tuple: tup("folder:proj", "viewer", "user:alice@example.com")},
		{Op: OpGrant, Tuple: tup("doc:readme", "parent", "folder:proj"),
			NotBeforeUnixNano: 1000, NotAfterUnixNano: 2000},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !checkBool(t, svc, "doc:readme", "viewer", "user:alice@example.com", 1500) {
		t.Error("inheritance should hold while the parent edge is active")
	}
	if checkBool(t, svc, "doc:readme", "viewer", "user:alice@example.com", 2500) {
		t.Error("inheritance should stop once the parent edge expires")
	}
}

func TestCheckNonIntervalIgnoresAsOf(t *testing.T) {
	svc := newConfiguredService(t)
	grantAll(t, svc, tup("doc:readme", "viewer", "user:alice@example.com"))
	// A permanent grant is active regardless of AsOf (including unset).
	if !checkBool(t, svc, "doc:readme", "viewer", "user:alice@example.com", 0) {
		t.Error("permanent grant should be active with AsOf=0")
	}
	if !checkBool(t, svc, "doc:readme", "viewer", "user:alice@example.com", 99999) {
		t.Error("permanent grant should be active at any AsOf")
	}
}

// TestCheckPaymentsScenario models the staff-override use case: staff (admins or
// support) can access all payments; self-access is handled by the application.
func TestCheckPaymentsScenario(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	for _, cfg := range []*NamespaceConfig{
		{Namespace: "payments", Relations: map[string]Rewrite{"staff": {This: &This{}}}},
		groupConfig(),
	} {
		if _, err := svc.WriteConfig(ctx, cfg); err != nil {
			t.Fatal(err)
		}
	}
	grantAll(t, svc,
		tup("payments:all", "staff", "group:admin#member"),
		tup("payments:all", "staff", "group:support#member"),
		tup("group:admin", "member", "user:root@example.com"),
		tup("group:support", "member", "user:carol@example.com"),
	)
	if !checkBool(t, svc, "payments:all", "staff", "user:root@example.com", 0) {
		t.Error("admin root should be staff")
	}
	if !checkBool(t, svc, "payments:all", "staff", "user:carol@example.com", 0) {
		t.Error("support carol should be staff")
	}
	if checkBool(t, svc, "payments:all", "staff", "user:mallory@example.com", 0) {
		t.Error("outsider mallory should not be staff")
	}
}

func TestCheckExactUsersetTarget(t *testing.T) {
	svc := newConfiguredService(t)
	grantAll(t, svc, tup("doc:readme", "viewer", "group:eng#member"))
	// Checking the userset itself as the subject is an exact match.
	if !checkBool(t, svc, "doc:readme", "viewer", "group:eng#member", 0) {
		t.Error("exact userset subject should match")
	}
}

func TestCheckCaseFold(t *testing.T) {
	svc := newConfiguredService(t, WithEmailCaseFold(true))
	grantAll(t, svc, tup("doc:readme", "viewer", "user:Alice@Example.COM"))
	if !checkBool(t, svc, "doc:readme", "viewer", "user:ALICE@example.com", 0) {
		t.Error("case-folded email should match regardless of request casing")
	}
}

func TestCheckInvalidInputs(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	if _, err := svc.Check(ctx, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("nil = %v, want ErrInvalidArgument", err)
	}
	if _, err := svc.Check(ctx, &CheckRequest{Object: "nocolon", Relation: "viewer", Subject: "user:a@b.com"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad object = %v, want ErrInvalidArgument", err)
	}
	if _, err := svc.Check(ctx, &CheckRequest{Object: "doc:x", Relation: "viewer", Subject: "user:*"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("wildcard subject = %v, want ErrInvalidArgument", err)
	}
}
