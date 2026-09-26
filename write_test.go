// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/visvasity/kv/kvutil"
)

// --- test helpers ---------------------------------------------------------

func grantReq(object, relation, subject string) *WriteRequest {
	return &WriteRequest{Mutations: []Mutation{{Op: OpGrant, Tuple: Tuple{Object: object, Relation: relation, Subject: subject}}}}
}

func revokeReq(object, relation, subject string) *WriteRequest {
	return &WriteRequest{Mutations: []Mutation{{Op: OpRevoke, Tuple: Tuple{Object: object, Relation: relation, Subject: subject}}}}
}

func getForwardMeta(t *testing.T, svc *Service, object, relation, subj string) (tupleMeta, bool) {
	t.Helper()
	ctx := context.Background()
	snap, _ := svc.db.NewSnapshot(ctx)
	defer snap.Discard(ctx)
	var m tupleMeta
	found, err := getGob(ctx, snap, forwardKey(svc.opts.keyPrefix, object, relation, subj), &m)
	if err != nil {
		t.Fatalf("getForwardMeta: %v", err)
	}
	return m, found
}

func getReverseMeta(t *testing.T, svc *Service, object, relation, subj string) (tupleMeta, bool) {
	t.Helper()
	ctx := context.Background()
	snap, _ := svc.db.NewSnapshot(ctx)
	defer snap.Discard(ctx)
	var m tupleMeta
	found, err := getGob(ctx, snap, reverseKey(svc.opts.keyPrefix, subj, object, relation), &m)
	if err != nil {
		t.Fatalf("getReverseMeta: %v", err)
	}
	return m, found
}

// checkIndexInvariant asserts the forward and reverse indexes describe exactly
// the same set of tuples with identical metadata (§11.3).
func checkIndexInvariant(t *testing.T, svc *Service) {
	t.Helper()
	ctx := context.Background()
	snap, _ := svc.db.NewSnapshot(ctx)
	defer snap.Discard(ctx)
	prefix := svc.opts.keyPrefix

	collect := func(class string, order [3]int) map[string]tupleMeta {
		out := map[string]tupleMeta{}
		beg, end := kvutil.PrefixRange(prefix + class)
		var err error
		for k, r := range snap.Ascend(ctx, beg, end, &err) {
			rest := strings.TrimPrefix(k, prefix+class)
			parts := strings.SplitN(rest, "/", 3)
			if len(parts) != 3 {
				t.Fatalf("malformed key %q", k)
			}
			// Normalize to obj|rel|subj regardless of index order.
			key := parts[order[0]] + "|" + parts[order[1]] + "|" + parts[order[2]]
			data, _ := io.ReadAll(r)
			var m tupleMeta
			if err := gobDecode(data, &m); err != nil {
				t.Fatalf("decode %q: %v", k, err)
			}
			out[key] = m
		}
		if err != nil {
			t.Fatalf("scan %s: %v", class, err)
		}
		return out
	}

	// forward key: t/obj/rel/subj  -> order (obj=0, rel=1, subj=2)
	fwd := collect(classForward, [3]int{0, 1, 2})
	// reverse key: s/subj/obj/rel  -> map to obj|rel|subj = (obj=1, rel=2, subj=0)
	rev := collect(classReverse, [3]int{1, 2, 0})

	if !reflect.DeepEqual(fwd, rev) {
		t.Errorf("forward/reverse index mismatch:\n forward=%v\n reverse=%v", fwd, rev)
	}
}

// --- tests ----------------------------------------------------------------

func TestGrantWritesBothIndexes(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)

	resp, err := svc.Write(ctx, grantReq("doc:readme", "viewer", "user:alice@example.com"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if resp.Applied != 1 {
		t.Errorf("Applied = %d, want 1", resp.Applied)
	}
	if _, ok := getForwardMeta(t, svc, "doc:readme", "viewer", "user:alice@example.com"); !ok {
		t.Error("forward record missing")
	}
	if _, ok := getReverseMeta(t, svc, "doc:readme", "viewer", "user:alice@example.com"); !ok {
		t.Error("reverse record missing")
	}
	checkIndexInvariant(t, svc)
}

func TestGrantIdempotentPreservesCreatedAt(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)

	first := &WriteRequest{Mutations: []Mutation{{
		Op: OpGrant, Tuple: Tuple{Object: "doc:readme", Relation: "viewer", Subject: "user:a@b.com"},
		CreatedAtUnixNano: 100,
	}}}
	if _, err := svc.Write(ctx, first); err != nil {
		t.Fatal(err)
	}

	// Redundant grant with a different CreatedAt and same (empty) interval.
	second := &WriteRequest{Mutations: []Mutation{{
		Op: OpGrant, Tuple: Tuple{Object: "doc:readme", Relation: "viewer", Subject: "user:a@b.com"},
		CreatedAtUnixNano: 999,
	}}}
	resp, err := svc.Write(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Applied != 0 {
		t.Errorf("redundant grant Applied = %d, want 0", resp.Applied)
	}
	m, _ := getForwardMeta(t, svc, "doc:readme", "viewer", "user:a@b.com")
	if m.CreatedAtUnixNano != 100 {
		t.Errorf("CreatedAt = %d, want 100 (must not be reset)", m.CreatedAtUnixNano)
	}
}

func TestGrantIntervalReplace(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)

	g := func(nb, na int64) (*WriteResponse, error) {
		return svc.Write(ctx, &WriteRequest{Mutations: []Mutation{{
			Op: OpGrant, Tuple: Tuple{Object: "doc:readme", Relation: "viewer", Subject: "user:a@b.com"},
			CreatedAtUnixNano: 10, NotBeforeUnixNano: nb, NotAfterUnixNano: na,
		}}})
	}
	if _, err := g(10, 20); err != nil {
		t.Fatal(err)
	}
	resp, err := g(10, 30) // change upper bound
	if err != nil {
		t.Fatal(err)
	}
	if resp.Applied != 1 {
		t.Errorf("interval change Applied = %d, want 1", resp.Applied)
	}
	m, _ := getForwardMeta(t, svc, "doc:readme", "viewer", "user:a@b.com")
	if m.NotAfterUnixNano != 30 || m.CreatedAtUnixNano != 10 {
		t.Errorf("meta = %+v, want NotAfter=30 CreatedAt=10", m)
	}
	checkIndexInvariant(t, svc)
}

func TestRevoke(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	svc.Write(ctx, grantReq("doc:readme", "viewer", "user:a@b.com"))

	resp, err := svc.Write(ctx, revokeReq("doc:readme", "viewer", "user:a@b.com"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Applied != 1 {
		t.Errorf("revoke Applied = %d, want 1", resp.Applied)
	}
	if _, ok := getForwardMeta(t, svc, "doc:readme", "viewer", "user:a@b.com"); ok {
		t.Error("forward record still present after revoke")
	}
	if _, ok := getReverseMeta(t, svc, "doc:readme", "viewer", "user:a@b.com"); ok {
		t.Error("reverse record still present after revoke")
	}
	// Revoke again is a no-op.
	resp, _ = svc.Write(ctx, revokeReq("doc:readme", "viewer", "user:a@b.com"))
	if resp.Applied != 0 {
		t.Errorf("second revoke Applied = %d, want 0", resp.Applied)
	}
	checkIndexInvariant(t, svc)
}

func TestWriteUsersetSubject(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)

	// Userset subject over a registered namespace: ok.
	if _, err := svc.Write(ctx, grantReq("doc:readme", "viewer", "group:eng#member")); err != nil {
		t.Fatalf("grant userset subject: %v", err)
	}
	// Object subject over a registered namespace (parent edge): ok.
	if _, err := svc.Write(ctx, grantReq("doc:readme", "parent", "folder:proj")); err != nil {
		t.Fatalf("grant object subject: %v", err)
	}
	checkIndexInvariant(t, svc)

	// Userset subject over an unregistered namespace: rejected.
	_, err := svc.Write(ctx, grantReq("doc:readme", "viewer", "team:x#member"))
	if !errors.Is(err, ErrNamespaceUnregistered) {
		t.Errorf("unregistered subject namespace = %v, want ErrNamespaceUnregistered", err)
	}
}

func TestWriteValidationErrors(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)

	tests := []struct {
		name string
		req  *WriteRequest
		code ErrorCode
	}{
		{"unregistered namespace", grantReq("ghost:x", "viewer", "user:a@b.com"), CodeNamespaceUnregistered},
		{"undeclared relation", grantReq("doc:readme", "ghostrel", "user:a@b.com"), CodeRelationUndeclared},
		{"bad object", grantReq("nocolon", "viewer", "user:a@b.com"), CodeInvalidArgument},
		{"bad subject", grantReq("doc:readme", "viewer", "user:notanemail"), CodeInvalidArgument},
		{"wildcard subject", grantReq("doc:readme", "viewer", "user:*"), CodeInvalidArgument},
		{"invalid op", &WriteRequest{Mutations: []Mutation{{Op: "nope", Tuple: Tuple{Object: "doc:readme", Relation: "viewer", Subject: "user:a@b.com"}}}}, CodeInvalidArgument},
		{"invalid interval", &WriteRequest{Mutations: []Mutation{{Op: OpGrant, Tuple: Tuple{Object: "doc:readme", Relation: "viewer", Subject: "user:a@b.com"}, NotBeforeUnixNano: 20, NotAfterUnixNano: 10}}}, CodeInvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Write(ctx, tt.req)
			if !errors.Is(err, sentinelForCode(tt.code)) {
				t.Errorf("err = %v, want code %s", err, tt.code)
			}
		})
	}
}

func TestWriteAtomicBatchAborts(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)

	// One valid mutation followed by an invalid one: nothing should be applied.
	req := &WriteRequest{Mutations: []Mutation{
		{Op: OpGrant, Tuple: Tuple{Object: "doc:readme", Relation: "viewer", Subject: "user:a@b.com"}},
		{Op: OpGrant, Tuple: Tuple{Object: "doc:readme", Relation: "ghostrel", Subject: "user:b@b.com"}},
	}}
	if _, err := svc.Write(ctx, req); !errors.Is(err, ErrRelationUndeclared) {
		t.Fatalf("err = %v, want ErrRelationUndeclared", err)
	}
	if _, ok := getForwardMeta(t, svc, "doc:readme", "viewer", "user:a@b.com"); ok {
		t.Error("valid mutation from an aborted batch was persisted")
	}
	checkIndexInvariant(t, svc)
}

func TestWritePreconditions(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)

	// must_exist on a missing tuple (revoke) -> PreconditionFailed.
	req := &WriteRequest{Mutations: []Mutation{{
		Op: OpRevoke, Precondition: PreconditionMustExist,
		Tuple: Tuple{Object: "doc:readme", Relation: "viewer", Subject: "user:a@b.com"},
	}}}
	if _, err := svc.Write(ctx, req); !errors.Is(err, ErrPreconditionFailed) {
		t.Errorf("must_exist on missing = %v, want ErrPreconditionFailed", err)
	}

	// Grant, then must_not_exist grant -> PreconditionFailed.
	svc.Write(ctx, grantReq("doc:readme", "viewer", "user:a@b.com"))
	req = &WriteRequest{Mutations: []Mutation{{
		Op: OpGrant, Precondition: PreconditionMustNotExist,
		Tuple: Tuple{Object: "doc:readme", Relation: "viewer", Subject: "user:a@b.com"},
	}}}
	if _, err := svc.Write(ctx, req); !errors.Is(err, ErrPreconditionFailed) {
		t.Errorf("must_not_exist on existing = %v, want ErrPreconditionFailed", err)
	}
	checkIndexInvariant(t, svc)
}

func TestWriteEmptyAndNil(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)

	resp, err := svc.Write(ctx, &WriteRequest{})
	if err != nil || resp.Applied != 0 {
		t.Errorf("empty write = (%v, %v), want (Applied 0, nil)", resp, err)
	}
	if _, err := svc.Write(ctx, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("nil write = %v, want ErrInvalidArgument", err)
	}
}

func TestWriteCaseFold(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t, WithEmailCaseFold(true))

	if _, err := svc.Write(ctx, grantReq("doc:readme", "viewer", "user:Alice@Example.COM")); err != nil {
		t.Fatal(err)
	}
	// Stored under the lower-cased canonical subject.
	if _, ok := getForwardMeta(t, svc, "doc:readme", "viewer", "user:alice@example.com"); !ok {
		t.Error("case-folded forward record missing")
	}
	checkIndexInvariant(t, svc)
}

func deleteMut(object string) Mutation {
	return Mutation{Op: OpDelete, Tuple: Tuple{Object: object}}
}

func TestDeleteObject(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	grantAll(t, svc,
		tup("doc:readme", "owner", "user:alice@example.com"),
		tup("doc:readme", "viewer", "user:bob@example.com"),
		tup("doc:readme", "viewer", "group:eng#member"),
		tup("doc:readme", "parent", "folder:proj"),
		tup("doc:other", "viewer", "user:carol@example.com"),
	)
	// Delete everything on doc:readme (object deletion cleanup).
	resp, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{deleteMut("doc:readme")}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Applied != 4 {
		t.Errorf("Applied = %d, want 4", resp.Applied)
	}
	if got := readAll(t, svc, ReadRequest{Object: "doc:readme"}, 0); len(got) != 0 {
		t.Errorf("doc:readme still has %d tuples", len(got))
	}
	// An unrelated object is untouched.
	if got := readAll(t, svc, ReadRequest{Object: "doc:other"}, 0); len(got) != 1 {
		t.Errorf("doc:other should be untouched, got %d", len(got))
	}
	checkIndexInvariant(t, svc)
}

func TestDeleteEmptyObject(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	resp, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{deleteMut("doc:nothing")}})
	if err != nil || resp.Applied != 0 {
		t.Errorf("delete of empty object = (%v, %v), want (Applied 0, nil)", resp, err)
	}
}

func TestDeleteRejectsRelationOrSubject(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	bad := []Mutation{
		{Op: OpDelete, Tuple: Tuple{Object: "doc:readme", Relation: "viewer"}},
		{Op: OpDelete, Tuple: Tuple{Object: "doc:readme", Subject: "user:a@b.com"}},
		{Op: OpDelete, Precondition: PreconditionMustExist, Tuple: Tuple{Object: "doc:readme"}},
	}
	for _, m := range bad {
		if _, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{m}}); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("delete %+v = %v, want ErrInvalidArgument", m.Tuple, err)
		}
	}
}

func TestDeleteIgnoresConfig(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	// Delete against an unregistered namespace is a clean no-op, not an error —
	// delete works on stored keys regardless of config.
	resp, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{deleteMut("ghost:x")}})
	if err != nil {
		t.Fatalf("delete on unregistered namespace: %v", err)
	}
	if resp.Applied != 0 {
		t.Errorf("Applied = %d, want 0", resp.Applied)
	}
}

func TestWriteBatchMultipleThenInvariant(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)

	req := &WriteRequest{Mutations: []Mutation{
		{Op: OpGrant, Tuple: Tuple{Object: "doc:readme", Relation: "owner", Subject: "user:a@b.com"}},
		{Op: OpGrant, Tuple: Tuple{Object: "doc:readme", Relation: "viewer", Subject: "group:eng#member"}},
		{Op: OpGrant, Tuple: Tuple{Object: "doc:readme", Relation: "parent", Subject: "folder:proj"}},
		{Op: OpGrant, Tuple: Tuple{Object: "group:eng", Relation: "member", Subject: "user:b@b.com"}},
	}}
	resp, err := svc.Write(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Applied != 4 {
		t.Errorf("Applied = %d, want 4", resp.Applied)
	}
	checkIndexInvariant(t, svc)
}
