// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// allCodes pairs every ErrorCode with its expected sentinel.
var allCodes = []struct {
	code     ErrorCode
	sentinel error
}{
	{CodeInvalidArgument, ErrInvalidArgument},
	{CodeNamespaceUnregistered, ErrNamespaceUnregistered},
	{CodeRelationUndeclared, ErrRelationUndeclared},
	{CodePreconditionFailed, ErrPreconditionFailed},
	{CodeConflict, ErrConflict},
	{CodeDepthExceeded, ErrDepthExceeded},
	{CodeUnavailable, ErrUnavailable},
}

func TestErrorUnwrapMatchesSentinel(t *testing.T) {
	for _, tc := range allCodes {
		t.Run(string(tc.code), func(t *testing.T) {
			err := error(&Error{Code: tc.code, Message: "x"})
			if !errors.Is(err, tc.sentinel) {
				t.Errorf("errors.Is(&Error{%s}, %v) = false, want true", tc.code, tc.sentinel)
			}
			// It must not match some *other* sentinel.
			for _, other := range allCodes {
				if other.code == tc.code {
					continue
				}
				if errors.Is(err, other.sentinel) {
					t.Errorf("&Error{%s} unexpectedly matched %v", tc.code, other.sentinel)
				}
			}
		})
	}
}

func TestErrorAs(t *testing.T) {
	err := error(&Error{Code: CodeRelationUndeclared, Message: "no such relation", Relation: "viewer"})
	var ze *Error
	if !errors.As(err, &ze) {
		t.Fatal("errors.As(&Error{}) = false, want true")
	}
	if ze.Code != CodeRelationUndeclared {
		t.Errorf("Code = %q, want %q", ze.Code, CodeRelationUndeclared)
	}
	if ze.Relation != "viewer" {
		t.Errorf("Relation = %q, want %q", ze.Relation, "viewer")
	}
}

func TestErrorUnknownCodeUnwrapsNil(t *testing.T) {
	e := &Error{Code: "SomethingElse", Message: "x"}
	if got := e.Unwrap(); got != nil {
		t.Errorf("Unwrap() = %v, want nil for unknown code", got)
	}
}

func TestErrorString(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		contains []string
	}{
		{
			name:     "message only",
			err:      &Error{Code: CodeInvalidArgument, Message: "bad input"},
			contains: []string{"zanzibar", "InvalidArgument", "bad input"},
		},
		{
			name:     "full tuple context",
			err:      &Error{Code: CodeRelationUndeclared, Message: "undeclared", Object: "doc:readme", Relation: "viewer", Subject: "user:a@b.com"},
			contains: []string{"doc:readme#viewer@user:a@b.com"},
		},
		{
			name:     "partial context",
			err:      &Error{Code: CodeNamespaceUnregistered, Message: "nope", Object: "doc:readme"},
			contains: []string{"object=\"doc:readme\""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.err.Error()
			for _, want := range tt.contains {
				if !strings.Contains(s, want) {
					t.Errorf("Error() = %q, want it to contain %q", s, want)
				}
			}
		})
	}
}

func TestFromKV(t *testing.T) {
	if got := fromKV(nil); got != nil {
		t.Errorf("fromKV(nil) = %v, want nil", got)
	}

	tests := []struct {
		name     string
		in       error
		wantCode ErrorCode
	}{
		{"invalid", os.ErrInvalid, CodeInvalidArgument},
		{"closed", os.ErrClosed, CodeUnavailable},
		{"generic", errors.New("disk exploded"), CodeUnavailable},
		{"wrapped invalid", &fs.PathError{Op: "get", Path: "k", Err: os.ErrInvalid}, CodeInvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fromKV(tt.in)
			var ze *Error
			if !errors.As(got, &ze) {
				t.Fatalf("fromKV(%v) = %T, want *Error", tt.in, got)
			}
			if ze.Code != tt.wantCode {
				t.Errorf("fromKV(%v).Code = %q, want %q", tt.in, ze.Code, tt.wantCode)
			}
			if !errors.Is(got, sentinelForCode(tt.wantCode)) {
				t.Errorf("fromKV(%v) does not match sentinel for %q", tt.in, tt.wantCode)
			}
		})
	}
}

func TestFromKVPassesThroughOwnError(t *testing.T) {
	orig := &Error{Code: CodeConflict, Message: "cas mismatch"}
	got := fromKV(orig)
	var ze *Error
	if !errors.As(got, &ze) || ze.Code != CodeConflict {
		t.Fatalf("fromKV(*Error) = %v, want the original CodeConflict error", got)
	}
}
