// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrorCode is a stable, documented category for an [Error]. Over HTTP it is
// surfaced as the code field of the error envelope (§14.3).
type ErrorCode string

const (
	// CodeInvalidArgument indicates a malformed object, relation, subject,
	// email, or config.
	CodeInvalidArgument ErrorCode = "InvalidArgument"

	// CodeNamespaceUnregistered indicates the object's namespace has no
	// effective config.
	CodeNamespaceUnregistered ErrorCode = "NamespaceUnregistered"

	// CodeRelationUndeclared indicates the relation is not declared in the
	// namespace's effective config.
	CodeRelationUndeclared ErrorCode = "RelationUndeclared"

	// CodePreconditionFailed indicates a write precondition was not met; the
	// write was aborted.
	CodePreconditionFailed ErrorCode = "PreconditionFailed"

	// CodeConflict indicates an optimistic-concurrency conflict: either a
	// transaction-commit conflict after exhausting retries, or a WriteConfig
	// compare-and-set version mismatch (§5.5). The caller re-reads and retries.
	CodeConflict ErrorCode = "Conflict"

	// CodeDepthExceeded indicates evaluation exceeded the configured recursion
	// depth; the operation denies with this error.
	CodeDepthExceeded ErrorCode = "DepthExceeded"

	// CodeUnavailable indicates an underlying kv storage failure.
	CodeUnavailable ErrorCode = "Unavailable"
)

// Sentinel errors matching each [ErrorCode], for use with errors.Is. A returned
// error wraps the sentinel corresponding to its category.
var (
	ErrInvalidArgument       = errors.New("zanzibar: invalid argument")
	ErrNamespaceUnregistered = errors.New("zanzibar: namespace unregistered")
	ErrRelationUndeclared    = errors.New("zanzibar: relation undeclared")
	ErrPreconditionFailed    = errors.New("zanzibar: precondition failed")
	ErrConflict              = errors.New("zanzibar: transaction conflict")
	ErrDepthExceeded         = errors.New("zanzibar: evaluation depth exceeded")
	ErrUnavailable           = errors.New("zanzibar: storage unavailable")
)

// Error is the typed error returned by [Service] methods. It carries a stable
// [ErrorCode] and, via Unwrap, the matching sentinel from this package so both
// errors.Is (against the sentinels) and errors.As (to *Error, to read Code and
// context) work.
type Error struct {
	// Code is the error category.
	Code ErrorCode `json:"code"`

	// Message is a human-readable description.
	Message string `json:"message"`

	// Object, Relation, and Subject identify the offending tuple fields when
	// applicable; empty otherwise.
	Object   string `json:"object,omitempty"`
	Relation string `json:"relation,omitempty"`
	Subject  string `json:"subject,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("zanzibar: ")
	if e.Code != "" {
		b.WriteString(string(e.Code))
		b.WriteString(": ")
	}
	if e.Message != "" {
		b.WriteString(e.Message)
	} else {
		b.WriteString("error")
	}
	// Append tuple context when present, for debuggability.
	switch {
	case e.Object != "" && e.Relation != "" && e.Subject != "":
		fmt.Fprintf(&b, " (%s#%s@%s)", e.Object, e.Relation, e.Subject)
	case e.Object != "" || e.Relation != "" || e.Subject != "":
		fmt.Fprintf(&b, " (object=%q relation=%q subject=%q)", e.Object, e.Relation, e.Subject)
	}
	return b.String()
}

// Unwrap returns the sentinel error matching e.Code, so errors.Is(e, Err...)
// reports the category. It returns nil for an unknown code.
func (e *Error) Unwrap() error {
	return sentinelForCode(e.Code)
}

// sentinelForCode maps an [ErrorCode] to its package sentinel, or nil.
func sentinelForCode(c ErrorCode) error {
	switch c {
	case CodeInvalidArgument:
		return ErrInvalidArgument
	case CodeNamespaceUnregistered:
		return ErrNamespaceUnregistered
	case CodeRelationUndeclared:
		return ErrRelationUndeclared
	case CodePreconditionFailed:
		return ErrPreconditionFailed
	case CodeConflict:
		return ErrConflict
	case CodeDepthExceeded:
		return ErrDepthExceeded
	case CodeUnavailable:
		return ErrUnavailable
	default:
		return nil
	}
}

// fromKV translates a low-level error returned by the kv layer into an [*Error]
// with an appropriate category, so storage conditions never leak as allow
// decisions (§15). It returns nil for a nil error. Callers that treat
// os.ErrNotExist as an expected "absent" condition should test for it before
// calling fromKV; here it is mapped to CodeUnavailable as a conservative,
// fail-closed default.
func fromKV(err error) error {
	if err == nil {
		return nil
	}
	// If it is already one of ours, pass it through unchanged.
	var ze *Error
	if errors.As(err, &ze) {
		return err
	}
	switch {
	case errors.Is(err, os.ErrInvalid):
		return &Error{Code: CodeInvalidArgument, Message: err.Error()}
	case errors.Is(err, os.ErrClosed):
		return &Error{Code: CodeUnavailable, Message: err.Error()}
	default:
		return &Error{Code: CodeUnavailable, Message: err.Error()}
	}
}
