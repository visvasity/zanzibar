// Copyright (c) 2026 Visvasity LLC

package zanzibar_test

import (
	"context"
	"fmt"
	"log"

	"github.com/visvasity/kv"
	"github.com/visvasity/kvmemdb"
	"github.com/visvasity/zanzibar"
)

// Example_payments shows the recommended model for a payments keyspace where a
// user may access only their own data, while staff (admins or support) may
// access everyone's. Ownership is intrinsic ("your keyspace is named after you")
// and is checked by the application with a string compare; zanzibar handles only
// the staff override, which is the part that needs a policy.
func Example_payments() {
	ctx := context.Background()
	svc, err := zanzibar.New(kv.DatabaseFrom(kvmemdb.New()))
	if err != nil {
		log.Fatal(err)
	}

	// Schema: staff on a single sentinel object is granted to the admin and
	// support group usersets; groups hold their members.
	for _, cfg := range []*zanzibar.NamespaceConfig{
		{Namespace: "payments", Relations: map[string]zanzibar.Rewrite{"staff": {This: &zanzibar.This{}}}},
		{Namespace: "group", Relations: map[string]zanzibar.Rewrite{"member": {This: &zanzibar.This{}}}},
	} {
		if _, err := svc.WriteConfig(ctx, cfg); err != nil {
			log.Fatal(err)
		}
	}

	// A handful of grants — two staff-grants plus one membership per staff member.
	if _, err := svc.Write(ctx, &zanzibar.WriteRequest{Mutations: []zanzibar.Mutation{
		{Op: zanzibar.OpGrant, Tuple: zanzibar.Tuple{Object: "payments:all", Relation: "staff", Subject: "group:admin#member"}},
		{Op: zanzibar.OpGrant, Tuple: zanzibar.Tuple{Object: "payments:all", Relation: "staff", Subject: "group:support#member"}},
		{Op: zanzibar.OpGrant, Tuple: zanzibar.Tuple{Object: "group:support", Relation: "member", Subject: "user:carol@example.com"}},
	}}); err != nil {
		log.Fatal(err)
	}

	// canAccess: self-access is intrinsic; otherwise defer to the staff override.
	canAccess := func(caller, keyspaceOwner string) bool {
		if caller == keyspaceOwner {
			return true
		}
		resp, err := svc.Check(ctx, &zanzibar.CheckRequest{
			Object:   "payments:all",
			Relation: "staff",
			Subject:  "user:" + caller,
		})
		if err != nil {
			return false // fail closed
		}
		return resp.Allowed
	}

	fmt.Println("alice -> alice:", canAccess("alice@example.com", "alice@example.com"))
	fmt.Println("alice -> bob:  ", canAccess("alice@example.com", "bob@example.com"))
	fmt.Println("carol -> bob:  ", canAccess("carol@example.com", "bob@example.com"))
	// Output:
	// alice -> alice: true
	// alice -> bob:   false
	// carol -> bob:   true
}
