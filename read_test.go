// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
)

// seedTuples grants a fixed set of tuples and returns the service.
func seedReadService(t *testing.T) *Service {
	t.Helper()
	ctx := context.Background()
	svc := newConfiguredService(t)
	muts := []Mutation{
		{Op: OpGrant, Tuple: Tuple{Object: "doc:readme", Relation: "owner", Subject: "user:alice@example.com"}},
		{Op: OpGrant, Tuple: Tuple{Object: "doc:readme", Relation: "viewer", Subject: "user:bob@example.com"}},
		{Op: OpGrant, Tuple: Tuple{Object: "doc:readme", Relation: "viewer", Subject: "group:eng#member"}},
		{Op: OpGrant, Tuple: Tuple{Object: "doc:readme", Relation: "parent", Subject: "folder:proj"}},
		{Op: OpGrant, Tuple: Tuple{Object: "doc:notes", Relation: "viewer", Subject: "user:alice@example.com"}},
		{Op: OpGrant, Tuple: Tuple{Object: "folder:proj", Relation: "viewer", Subject: "user:alice@example.com"}},
	}
	if _, err := svc.Write(ctx, &WriteRequest{Mutations: muts}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return svc
}

// readAll drains all pages for a request (with the given page size) and returns
// the flattened records.
func readAll(t *testing.T, svc *Service, req ReadRequest, pageSize int) []TupleRecord {
	t.Helper()
	ctx := context.Background()
	var out []TupleRecord
	req.PageSize = pageSize
	seen := map[string]bool{}
	for {
		resp, err := svc.Read(ctx, &req)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		for _, r := range resp.Records {
			key := r.Tuple.Object + "#" + r.Tuple.Relation + "@" + r.Tuple.Subject
			if seen[key] {
				t.Fatalf("duplicate record across pages: %s", key)
			}
			seen[key] = true
			out = append(out, r)
		}
		if resp.NextPageToken == "" {
			break
		}
		req.PageToken = resp.NextPageToken
	}
	return out
}

func tripleSet(recs []TupleRecord) []string {
	var out []string
	for _, r := range recs {
		out = append(out, fmt.Sprintf("%s#%s@%s", r.Tuple.Object, r.Tuple.Relation, r.Tuple.Subject))
	}
	sort.Strings(out)
	return out
}

func TestReadByObject(t *testing.T) {
	svc := seedReadService(t)
	got := tripleSet(readAll(t, svc, ReadRequest{Object: "doc:readme"}, 0))
	want := []string{
		"doc:readme#owner@user:alice@example.com",
		"doc:readme#parent@folder:proj",
		"doc:readme#viewer@group:eng#member",
		"doc:readme#viewer@user:bob@example.com",
	}
	if !equalStrings(got, want) {
		t.Errorf("by object:\n got %v\nwant %v", got, want)
	}
}

func TestReadByObjectRelation(t *testing.T) {
	svc := seedReadService(t)
	got := tripleSet(readAll(t, svc, ReadRequest{Object: "doc:readme", Relation: "viewer"}, 0))
	want := []string{
		"doc:readme#viewer@group:eng#member",
		"doc:readme#viewer@user:bob@example.com",
	}
	if !equalStrings(got, want) {
		t.Errorf("by object+relation:\n got %v\nwant %v", got, want)
	}
}

func TestReadExactTuple(t *testing.T) {
	svc := seedReadService(t)
	got := readAll(t, svc, ReadRequest{Object: "doc:readme", Relation: "owner", Subject: "user:alice@example.com"}, 0)
	if len(got) != 1 {
		t.Fatalf("exact read returned %d, want 1", len(got))
	}
}

func TestReadBySubject(t *testing.T) {
	svc := seedReadService(t)
	got := tripleSet(readAll(t, svc, ReadRequest{Subject: "user:alice@example.com"}, 0))
	want := []string{
		"doc:notes#viewer@user:alice@example.com",
		"doc:readme#owner@user:alice@example.com",
		"folder:proj#viewer@user:alice@example.com",
	}
	if !equalStrings(got, want) {
		t.Errorf("by subject:\n got %v\nwant %v", got, want)
	}
}

func TestReadBySubjectRelation(t *testing.T) {
	svc := seedReadService(t)
	got := tripleSet(readAll(t, svc, ReadRequest{Subject: "user:alice@example.com", Relation: "viewer"}, 0))
	want := []string{
		"doc:notes#viewer@user:alice@example.com",
		"folder:proj#viewer@user:alice@example.com",
	}
	if !equalStrings(got, want) {
		t.Errorf("by subject+relation:\n got %v\nwant %v", got, want)
	}
}

func TestReadByNamespace(t *testing.T) {
	svc := seedReadService(t)
	got := tripleSet(readAll(t, svc, ReadRequest{Namespace: "folder"}, 0))
	want := []string{"folder:proj#viewer@user:alice@example.com"}
	if !equalStrings(got, want) {
		t.Errorf("by namespace:\n got %v\nwant %v", got, want)
	}
}

func TestReadAll(t *testing.T) {
	svc := seedReadService(t)
	got := readAll(t, svc, ReadRequest{}, 0)
	if len(got) != 6 {
		t.Errorf("read-all returned %d records, want 6", len(got))
	}
}

func TestReadPaginationCompleteAndDeterministic(t *testing.T) {
	svc := seedReadService(t)
	// Page size 2 across the by-object result set (4 records).
	full := tripleSet(readAll(t, svc, ReadRequest{Object: "doc:readme"}, 100))
	paged := tripleSet(readAll(t, svc, ReadRequest{Object: "doc:readme"}, 2))
	if !equalStrings(full, paged) {
		t.Errorf("paged result != full result:\n full=%v\npaged=%v", full, paged)
	}
}

func TestReadOrderIsAscendingKey(t *testing.T) {
	svc := seedReadService(t)
	ctx := context.Background()
	resp, err := svc.Read(ctx, &ReadRequest{Object: "doc:readme"})
	if err != nil {
		t.Fatal(err)
	}
	// Records must be in ascending (relation, subject) order for a fixed object.
	prev := ""
	for _, r := range resp.Records {
		cur := r.Tuple.Relation + "/" + r.Tuple.Subject
		if prev != "" && cur < prev {
			t.Errorf("records not in ascending order: %q before %q", prev, cur)
		}
		prev = cur
	}
}

func TestReadSurfacesInterval(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t)
	_, err := svc.Write(ctx, &WriteRequest{Mutations: []Mutation{{
		Op:                OpGrant,
		Tuple:             Tuple{Object: "doc:readme", Relation: "viewer", Subject: "user:temp@example.com"},
		CreatedAtUnixNano: 500, NotBeforeUnixNano: 1000, NotAfterUnixNano: 2000,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := svc.Read(ctx, &ReadRequest{Object: "doc:readme", Relation: "viewer", Subject: "user:temp@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Records) != 1 {
		t.Fatalf("got %d records, want 1", len(resp.Records))
	}
	r := resp.Records[0]
	if r.CreatedAtUnixNano != 500 || r.NotBeforeUnixNano != 1000 || r.NotAfterUnixNano != 2000 {
		t.Errorf("interval metadata = %+v, want 500/1000/2000", r)
	}
}

func TestReadCaseFoldSubjectFilter(t *testing.T) {
	ctx := context.Background()
	svc := newConfiguredService(t, WithEmailCaseFold(true))
	svc.Write(ctx, grantReq("doc:readme", "viewer", "user:Alice@Example.COM"))

	// Filtering by a differently-cased email finds the folded record.
	resp, err := svc.Read(ctx, &ReadRequest{Subject: "user:ALICE@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Records) != 1 || resp.Records[0].Tuple.Subject != "user:alice@example.com" {
		t.Errorf("case-fold subject filter = %+v, want 1 folded record", resp.Records)
	}
}

func TestReadInvalidInputs(t *testing.T) {
	ctx := context.Background()
	svc := seedReadService(t)
	if _, err := svc.Read(ctx, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("nil req = %v, want ErrInvalidArgument", err)
	}
	if _, err := svc.Read(ctx, &ReadRequest{Object: "nocolon"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad object = %v, want ErrInvalidArgument", err)
	}
	if _, err := svc.Read(ctx, &ReadRequest{Subject: "user:bad"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad subject = %v, want ErrInvalidArgument", err)
	}
	if _, err := svc.Read(ctx, &ReadRequest{PageToken: "!!!not-base64!!!"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad token = %v, want ErrInvalidArgument", err)
	}
}

func TestReadEmptyResult(t *testing.T) {
	svc := seedReadService(t)
	got := readAll(t, svc, ReadRequest{Object: "doc:ghost"}, 0)
	if len(got) != 0 {
		t.Errorf("read of absent object returned %d records, want 0", len(got))
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
