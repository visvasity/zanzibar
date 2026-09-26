// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestConcurrentDistinctWrites grants many distinct tuples concurrently; all
// must succeed (no false conflicts) and all must be present afterward. Run under
// -race to catch data races in the shared Service.
func TestConcurrentDistinctWrites(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)

	const n = 32
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			obj := fmt.Sprintf("doc:d%d", i)
			subj := fmt.Sprintf("user:u%d@example.com", i)
			_, errs[i] = svc.Write(ctx, grantReq(obj, "viewer", subj))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("write %d failed: %v", i, err)
		}
	}
	for i := 0; i < n; i++ {
		obj := fmt.Sprintf("doc:d%d", i)
		subj := fmt.Sprintf("user:u%d@example.com", i)
		if !checkBool(t, svc, obj, "viewer", subj, 0) {
			t.Errorf("tuple %s#viewer@%s missing after concurrent writes", obj, subj)
		}
	}
	checkIndexInvariant(t, svc)
}

// TestConcurrentSameTupleGrant grants the SAME tuple from many goroutines. Each
// must succeed (idempotent), commit-retry absorbing the write-write conflicts,
// and the tuple must exist exactly once.
func TestConcurrentSameTupleGrant(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)

	const n = 16
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.Write(ctx, grantReq("doc:shared", "viewer", "user:alice@example.com"))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("grant %d failed: %v", i, err)
		}
	}
	if _, ok := getForwardMeta(t, svc, "doc:shared", "viewer", "user:alice@example.com"); !ok {
		t.Error("shared tuple missing after concurrent idempotent grants")
	}
	checkIndexInvariant(t, svc)
}

// TestConcurrentWritesAndChecks runs writes and checks concurrently to exercise
// snapshot isolation under -race: checks must never observe a torn state and must
// not race with writers.
func TestConcurrentWritesAndChecks(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)

	var wg sync.WaitGroup
	// Writers.
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			obj := fmt.Sprintf("doc:x%d", i)
			if _, err := svc.Write(ctx, grantReq(obj, "viewer", "user:alice@example.com")); err != nil {
				t.Errorf("write: %v", err)
			}
		}(i)
	}
	// Readers running against a moving target; any boolean is acceptable, we only
	// require no race and no error.
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			obj := fmt.Sprintf("doc:x%d", i)
			if _, err := svc.Check(ctx, &CheckRequest{Object: obj, Relation: "viewer", Subject: "user:alice@example.com"}); err != nil {
				t.Errorf("check: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// After the dust settles every written tuple resolves true.
	for i := 0; i < 16; i++ {
		obj := fmt.Sprintf("doc:x%d", i)
		if !checkBool(t, svc, obj, "viewer", "user:alice@example.com", 0) {
			t.Errorf("%s#viewer@alice not visible after writes", obj)
		}
	}
}
