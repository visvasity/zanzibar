// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"testing"

	"github.com/visvasity/kv"
)

func TestGetSetDelRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := newTestDB()

	// Set via a transaction.
	tx, err := db.NewTransaction(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := tupleMeta{CreatedAtUnixNano: 5, NotAfterUnixNano: 9}
	if err := setGob(ctx, tx, "k1", &want); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// Read via a snapshot.
	snap, err := db.NewSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got tupleMeta
	found, err := getGob(ctx, snap, "k1", &got)
	snap.Discard(ctx)
	if err != nil || !found {
		t.Fatalf("getGob: found=%v err=%v", found, err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// Delete and confirm absence.
	tx2, _ := db.NewTransaction(ctx)
	if err := del(ctx, tx2, "k1"); err != nil {
		t.Fatal(err)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	snap2, _ := db.NewSnapshot(ctx)
	defer snap2.Discard(ctx)
	found, err = getGob(ctx, snap2, "k1", &got)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("key still present after delete")
	}
}

func TestGetGobNotFound(t *testing.T) {
	ctx := context.Background()
	db := newTestDB()
	snap, _ := db.NewSnapshot(ctx)
	defer snap.Discard(ctx)
	var m tupleMeta
	found, err := getGob(ctx, snap, "missing", &m)
	if err != nil {
		t.Fatalf("getGob(missing) err = %v, want nil", err)
	}
	if found {
		t.Error("found = true for missing key")
	}
}

func TestScanGobOrder(t *testing.T) {
	ctx := context.Background()
	db := newTestDB()
	tx, _ := db.NewTransaction(ctx)
	for _, k := range []string{"p/c", "p/a", "p/b", "q/z"} {
		m := tupleMeta{CreatedAtUnixNano: int64(len(k))}
		if err := setGob(ctx, tx, k, &m); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	snap, _ := db.NewSnapshot(ctx)
	defer snap.Discard(ctx)
	got, err := scanGob[tupleMeta](ctx, snap, "p/", "p0") // PrefixRange("p/") == ["p/","p0")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("scanGob returned %d items, want 3 (must exclude q/z)", len(got))
	}
}

// TestSnapshotIsolation demonstrates the single-shared-view property: reads
// through one snapshot see a stable point-in-time view even as other
// transactions commit changes.
func TestSnapshotIsolation(t *testing.T) {
	ctx := context.Background()
	db := newTestDB()

	// Initial value v1.
	tx, _ := db.NewTransaction(ctx)
	if err := setGob(ctx, tx, "k", &tupleMeta{CreatedAtUnixNano: 1}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// Open the snapshot that must keep seeing v1.
	snap, _ := db.NewSnapshot(ctx)
	defer snap.Discard(ctx)

	// Concurrently commit v2.
	tx2, _ := db.NewTransaction(ctx)
	if err := setGob(ctx, tx2, "k", &tupleMeta{CreatedAtUnixNano: 2}); err != nil {
		t.Fatal(err)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// The original snapshot still observes v1.
	var got tupleMeta
	if _, err := getGob(ctx, snap, "k", &got); err != nil {
		t.Fatal(err)
	}
	if got.CreatedAtUnixNano != 1 {
		t.Errorf("snapshot saw %d, want 1 (isolation broken)", got.CreatedAtUnixNano)
	}

	// A fresh snapshot observes v2.
	snap2, _ := db.NewSnapshot(ctx)
	defer snap2.Discard(ctx)
	if _, err := getGob(ctx, snap2, "k", &got); err != nil {
		t.Fatal(err)
	}
	if got.CreatedAtUnixNano != 2 {
		t.Errorf("fresh snapshot saw %d, want 2", got.CreatedAtUnixNano)
	}
}

func TestInTxAbortRollsBack(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)

	sentinel := &Error{Code: CodeInvalidArgument, Message: "abort"}
	err := svc.inTx(ctx, func(tx kv.Transaction) error {
		if err := setGob(ctx, tx, svc.opts.keyPrefix+"x", &tupleMeta{CreatedAtUnixNano: 7}); err != nil {
			return err
		}
		return sentinel
	})
	if err != sentinel {
		t.Fatalf("inTx returned %v, want sentinel", err)
	}

	// The write must not have been committed.
	snap, _ := svc.db.NewSnapshot(ctx)
	defer snap.Discard(ctx)
	var m tupleMeta
	found, err := getGob(ctx, snap, svc.opts.keyPrefix+"x", &m)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("aborted transaction left data behind")
	}
}

func TestInTxCommits(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if err := svc.inTx(ctx, func(tx kv.Transaction) error {
		return setGob(ctx, tx, svc.opts.keyPrefix+"y", &tupleMeta{CreatedAtUnixNano: 42})
	}); err != nil {
		t.Fatal(err)
	}
	snap, _ := svc.db.NewSnapshot(ctx)
	defer snap.Discard(ctx)
	var m tupleMeta
	found, err := getGob(ctx, snap, svc.opts.keyPrefix+"y", &m)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if m.CreatedAtUnixNano != 42 {
		t.Errorf("got %d, want 42", m.CreatedAtUnixNano)
	}
}
