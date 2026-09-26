// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"errors"
	"testing"
)

func TestNewDefaults(t *testing.T) {
	svc := newTestService(t)
	if svc == nil {
		t.Fatal("New returned nil service")
	}
	if svc.db == nil {
		t.Error("service db is nil")
	}
	if got, want := svc.opts.keyPrefix, DefaultKeyPrefix; got != want {
		t.Errorf("keyPrefix = %q, want %q", got, want)
	}
	if got, want := svc.opts.maxDepth, DefaultMaxDepth; got != want {
		t.Errorf("maxDepth = %d, want %d", got, want)
	}
	if got, want := svc.opts.commitRetries, DefaultCommitRetries; got != want {
		t.Errorf("commitRetries = %d, want %d", got, want)
	}
	if svc.opts.emailCaseFold {
		t.Error("emailCaseFold = true, want false by default")
	}
}

func TestNewNilDB(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("New(nil) succeeded, want error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("New(nil) error = %v, want ErrInvalidArgument", err)
	}
}

func TestOptionsApplied(t *testing.T) {
	svc := newTestService(t,
		WithKeyPrefix("acl/"),
		WithMaxDepth(7),
		WithCommitRetries(0),
		WithEmailCaseFold(true),
	)
	if got, want := svc.opts.keyPrefix, "acl/"; got != want {
		t.Errorf("keyPrefix = %q, want %q", got, want)
	}
	if got, want := svc.opts.maxDepth, 7; got != want {
		t.Errorf("maxDepth = %d, want %d", got, want)
	}
	if got, want := svc.opts.commitRetries, 0; got != want {
		t.Errorf("commitRetries = %d, want %d", got, want)
	}
	if !svc.opts.emailCaseFold {
		t.Error("emailCaseFold = false, want true")
	}
}

func TestOptionValidation(t *testing.T) {
	tests := []struct {
		name string
		opt  Option
	}{
		{"empty key prefix", WithKeyPrefix("")},
		{"zero max depth", WithMaxDepth(0)},
		{"negative max depth", WithMaxDepth(-1)},
		{"negative commit retries", WithCommitRetries(-1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(newTestDB(), tt.opt)
			if err == nil {
				t.Fatalf("New with %s succeeded, want error", tt.name)
			}
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func TestNewNilOptionIgnored(t *testing.T) {
	// A nil Option in the variadic list must be skipped, not panic.
	svc, err := New(newTestDB(), nil, WithMaxDepth(5), nil)
	if err != nil {
		t.Fatalf("New with nil options: %v", err)
	}
	if svc.opts.maxDepth != 5 {
		t.Errorf("maxDepth = %d, want 5", svc.opts.maxDepth)
	}
}

func TestOptionErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom")
	bad := func(*options) error { return sentinel }
	_, err := New(newTestDB(), Option(bad))
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want sentinel to propagate", err)
	}
}
