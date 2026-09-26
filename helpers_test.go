// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"testing"

	"github.com/visvasity/kv"
	"github.com/visvasity/kvmemdb"
)

// newTestDB returns a fresh in-memory kv.Database for tests.
func newTestDB() kv.Database {
	return kv.DatabaseFrom(kvmemdb.New())
}

// newTestService constructs a Service over a fresh in-memory database, failing
// the test on construction error.
func newTestService(t *testing.T, opts ...Option) *Service {
	t.Helper()
	svc, err := New(newTestDB(), opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc
}
