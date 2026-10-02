// Copyright (c) 2026 Visvasity LLC

package schema

import (
	"context"
	"errors"

	"github.com/visvasity/zanzibar"
)

// Ensure creates in svc any namespace configs parsed from src that are not already
// stored, and is the standard way to seed a service's schema at startup. It is
// idempotent: a namespace that already exists is left untouched (so operator
// overrides written at runtime are preserved), and one created concurrently by
// another process — surfaced as a compare-and-set conflict — is treated as already
// present. A parse error, or any storage error other than those two benign cases,
// is returned.
func Ensure(ctx context.Context, svc *zanzibar.Service, src []byte) error {
	cfgs, err := Parse(src)
	if err != nil {
		return err
	}
	for i := range cfgs {
		switch _, err := svc.ReadConfig(ctx, cfgs[i].Namespace); {
		case err == nil:
			continue // already present
		case !errors.Is(err, zanzibar.ErrNamespaceUnregistered):
			return err
		}
		if _, err := svc.WriteConfig(ctx, &cfgs[i]); err != nil && !errors.Is(err, zanzibar.ErrConflict) {
			return err
		}
	}
	return nil
}
