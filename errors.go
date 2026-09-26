// Copyright (c) 2026 Visvasity LLC

package zanzibar

import "errors"

// ErrorCode is a stable, documented category for an [Error]. Over HTTP it is
// surfaced as the ErrorType field of the httphelp error body.
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
	panic("not implemented")
}

// Unwrap returns the sentinel error matching e.Code, so errors.Is(e,
// Err...) reports the category.
func (e *Error) Unwrap() error {
	panic("not implemented")
}
