// Copyright (c) 2026 Visvasity LLC

package schema

import (
	"context"
	"testing"

	"github.com/visvasity/kv"
	"github.com/visvasity/kvmemdb"
	"github.com/visvasity/zanzibar"
)

func TestEnsure(t *testing.T) {
	ctx := context.Background()
	svc, err := zanzibar.New(kv.DatabaseFrom(kvmemdb.New()))
	if err != nil {
		t.Fatalf("zanzibar.New: %v", err)
	}

	src := []byte(`
namespace folder {
  relation owner
}
namespace doc {
  relation parent
  relation owner = owner from parent
}
`)
	if err := Ensure(ctx, svc, src); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	for _, ns := range []string{"folder", "doc"} {
		if _, err := svc.ReadConfig(ctx, ns); err != nil {
			t.Errorf("namespace %q not created: %v", ns, err)
		}
	}

	// Idempotent: a second pass over the same service is a no-op.
	if err := Ensure(ctx, svc, src); err != nil {
		t.Fatalf("Ensure (second pass): %v", err)
	}

	// A parse error propagates (and nothing is written).
	if err := Ensure(ctx, svc, []byte("namespace {")); err == nil {
		t.Error("Ensure with invalid schema: want a parse error")
	}
}
