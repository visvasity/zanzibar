// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
)

// TestPropertyCheckMatchesFixpoint cross-validates the on-demand recursive Check
// against an independent, fully-materialized fixpoint evaluator over random
// tuple graphs on the (monotonic) doc/folder/group schema. It also proves
// termination: the test only completes if Check never loops on the random
// cycles that arise.
func TestPropertyCheckMatchesFixpoint(t *testing.T) {
	for seed := int64(1); seed <= 8; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			svc := newConfiguredService(t)
			seedRandomTuples(t, svc, rng)

			ref := fixpointMembers(t, svc)
			objects, users := universe(t, svc)

			for _, o := range objects {
				cfg := mustConfig(t, svc, namespaceOf(o))
				for rel := range cfg.Relations {
					for _, u := range users {
						want := ref[o+"#"+rel][u]
						got := checkBool(t, svc, o, rel, u, 0)
						if got != want {
							t.Fatalf("Check(%s#%s@%s) = %v, want %v (seed %d)", o, rel, u, got, want, seed)
						}
					}
				}
			}
		})
	}
}

// seedRandomTuples grants a random, valid set of tuples across the doc/folder/
// group schema, including nesting and parent chains that may form cycles.
func seedRandomTuples(t *testing.T, svc *Service, rng *rand.Rand) {
	t.Helper()
	users := []string{"user:u0@x.com", "user:u1@x.com", "user:u2@x.com", "user:u3@x.com"}
	groups := []string{"group:g0", "group:g1", "group:g2"}
	docs := []string{"doc:c0", "doc:c1", "doc:c2", "doc:c3"}
	folders := []string{"folder:f0", "folder:f1", "folder:f2", "folder:f3"}

	pick := func(ss []string) string { return ss[rng.Intn(len(ss))] }

	var muts []Mutation
	grant := func(o, r, s string) {
		muts = append(muts, Mutation{Op: OpGrant, Tuple: Tuple{Object: o, Relation: r, Subject: s}})
	}

	n := 30 + rng.Intn(20)
	for i := 0; i < n; i++ {
		switch rng.Intn(8) {
		case 0:
			grant(pick(groups), "member", pick(users))
		case 1:
			grant(pick(groups), "member", pick(groups)+"#member")
		case 2:
			grant(pick(docs), "owner", pick(users))
		case 3:
			grant(pick(docs), "viewer", pick(users))
		case 4:
			grant(pick(docs), "viewer", pick(groups)+"#member")
		case 5:
			grant(pick(docs), "parent", pick(folders))
		case 6:
			grant(pick(folders), "viewer", pick(users))
			grant(pick(folders), "viewer", pick(groups)+"#member")
		case 7:
			grant(pick(folders), "parent", pick(folders))
		}
	}
	if _, err := svc.Write(context.Background(), &WriteRequest{Mutations: muts}); err != nil {
		t.Fatalf("seed write: %v", err)
	}
}

// universe returns all objects appearing in tuples (as object or object-valued
// subject) and all user subjects appearing.
func universe(t *testing.T, svc *Service) (objects, users []string) {
	recs := readAll(t, svc, ReadRequest{}, 0)
	objSet := map[string]bool{}
	userSet := map[string]bool{}
	for _, r := range recs {
		objSet[r.Tuple.Object] = true
		sub, err := parseSubject(r.Tuple.Subject)
		if err != nil {
			continue
		}
		switch sub.kind {
		case kindUser:
			userSet[r.Tuple.Subject] = true
		case kindObject:
			objSet[sub.namespace+":"+sub.id] = true
		}
	}
	return sortedKeysOf(objSet), sortedKeysOf(userSet)
}

func mustConfig(t *testing.T, svc *Service, namespace string) *NamespaceConfig {
	t.Helper()
	cfg, err := svc.ReadConfig(context.Background(), namespace)
	if err != nil {
		t.Fatalf("ReadConfig(%s): %v", namespace, err)
	}
	return cfg
}

// fixpointMembers materializes, for every (object, relation) node, the set of
// user subjects that are members, by iterating to a fixpoint. It is an
// intentionally different algorithm from Check (whole-graph forward fixpoint vs.
// on-demand recursion) and only supports the monotonic rewrite kinds present in
// the doc/folder/group schema (this, computed_userset, tuple_to_userset, union).
func fixpointMembers(t *testing.T, svc *Service) map[string]map[string]bool {
	t.Helper()
	recs := readAll(t, svc, ReadRequest{}, 0)
	orSubjects := map[string][]string{} // "object|relation" -> subject strings
	for _, r := range recs {
		key := r.Tuple.Object + "|" + r.Tuple.Relation
		orSubjects[key] = append(orSubjects[key], r.Tuple.Subject)
	}
	objects, _ := universe(t, svc)
	cfgOf := map[string]*NamespaceConfig{}
	getCfg := func(ns string) *NamespaceConfig {
		if c, ok := cfgOf[ns]; ok {
			return c
		}
		c := mustConfig(t, svc, ns)
		cfgOf[ns] = c
		return c
	}

	members := map[string]map[string]bool{}
	get := func(node string) map[string]bool {
		if members[node] == nil {
			members[node] = map[string]bool{}
		}
		return members[node]
	}

	// Iterate to a fixpoint: recompute every node from current estimates until
	// nothing changes. Monotonic rewrites guarantee convergence.
	for {
		changed := false
		for _, o := range objects {
			cfg := getCfg(namespaceOf(o))
			for rel := range cfg.Relations {
				node := o + "#" + rel
				next := evalRewriteRef(o, rel, cfg.Relations[rel], orSubjects, get)
				if !sameSet(members[node], next) {
					members[node] = next
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return members
}

// evalRewriteRef evaluates a monotonic rewrite for object#relation using the
// current membership estimates via get().
func evalRewriteRef(object, relation string, rw Rewrite, orSubjects map[string][]string, get func(string) map[string]bool) map[string]bool {
	out := map[string]bool{}
	switch {
	case isEmptyRewrite(rw) || rw.This != nil:
		for _, subj := range orSubjects[object+"|"+relation] {
			sub, err := parseSubject(subj)
			if err != nil {
				continue
			}
			switch sub.kind {
			case kindUser:
				out[subj] = true
			case kindUserset:
				for u := range get(sub.namespace + ":" + sub.id + "#" + sub.relation) {
					out[u] = true
				}
			}
		}
	case rw.ComputedUserset != nil:
		for u := range get(object + "#" + rw.ComputedUserset.Relation) {
			out[u] = true
		}
	case rw.TupleToUserset != nil:
		for _, subj := range orSubjects[object+"|"+rw.TupleToUserset.Tupleset] {
			sub, err := parseSubject(subj)
			if err != nil || sub.kind != kindObject {
				continue
			}
			for u := range get(sub.namespace + ":" + sub.id + "#" + rw.TupleToUserset.ComputedUserset) {
				out[u] = true
			}
		}
	case rw.Union != nil:
		for _, c := range rw.Union.Children {
			for u := range evalRewriteRef(object, relation, c, orSubjects, get) {
				out[u] = true
			}
		}
	}
	return out
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
