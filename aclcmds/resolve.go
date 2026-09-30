// Copyright (c) 2026 Visvasity LLC

package aclcmds

import "context"

// Subject scheme tokens. A user subject is "user:<value>"; the value is either
// an email-shaped identity or the wildcard "*".
const (
	userPrefix    = "user:"
	wildcardValue = "*"
)

// SubjectResolver converts the value of a user subject (the part after "user:")
// from a human email into an application identity — typically a synthetic
// identity produced by github.com/visvasity/userdb.
//
// It is called only for user subjects that IsSynthetic reports as NOT already
// synthetic. It receives the caller's context so it may perform I/O (a directory
// lookup, an RPC).
//
// On a logical miss (no identity is mapped to the email), the RECOMMENDED
// behavior is to log a warning and return the input value unchanged with a nil
// error; aclcmds then uses "user:<input>" verbatim, which still matches any
// legacy email-based grant during a migration. Reserve a non-nil error for
// infrastructure failures (backend unreachable), which abort the command.
type SubjectResolver func(ctx context.Context, email string) (id string, err error)

// IsSynthetic reports whether a user-subject value is ALREADY a synthetic
// identity (so it must be passed through untouched rather than resolved or
// re-formatted). It is purely syntactic — no I/O — for example a suffix match
// on the identity domain: strings.HasSuffix(id, "@id.example.invalid").
//
// It is the switch that enables subject mapping: if it is nil, no mapping is
// performed and Resolve/Format are ignored (subjects pass through unchanged).
// When set, it keeps mapping idempotent by preventing re-resolution of
// already-synthetic subjects.
type IsSynthetic func(id string) bool

// SubjectFormatter converts a synthetic identity back into a human-readable
// value (an email) for display in command output. It is called only for user
// subjects that IsSynthetic reports as synthetic.
//
// Like SubjectResolver, on a logical miss it SHOULD log a warning and return
// the input unchanged with a nil error; reserve a non-nil error for
// infrastructure failures.
type SubjectFormatter func(ctx context.Context, id string) (display string, err error)

// SubjectOptions configures how commands map user subjects between human emails
// (as entered and displayed) and application identities (as stored and
// evaluated). A zero value maps nothing — subjects are used verbatim.
//
// IsSynthetic is the switch: mapping happens only when it is set. If it is nil,
// Resolve and Format are ignored and every subject passes through unchanged.
type SubjectOptions struct {
	// Resolve maps a human-email subject to an identity on command input.
	Resolve SubjectResolver
	// IsSynthetic classifies a value as already-synthetic (skip mapping); nil
	// disables mapping entirely.
	IsSynthetic IsSynthetic
	// Format maps a synthetic identity back to a human email for output.
	Format SubjectFormatter
}

// mapInput resolves a subject supplied on the command line. Non-user subjects,
// the wildcard, and already-synthetic user subjects are returned unchanged. It
// is a no-op unless both Resolve and IsSynthetic are set.
func (o SubjectOptions) mapInput(ctx context.Context, s string) (string, error) {
	if o.Resolve == nil || o.IsSynthetic == nil {
		return s, nil
	}
	v, ok := userValue(s)
	if !ok || v == wildcardValue || o.IsSynthetic(v) {
		return s, nil
	}
	id, err := o.Resolve(ctx, v)
	if err != nil {
		return "", err
	}
	return userPrefix + id, nil
}

// mapOutput formats a subject for display. Non-user subjects, the wildcard, and
// non-synthetic user subjects are returned unchanged. It is a no-op unless both
// Format and IsSynthetic are set.
func (o SubjectOptions) mapOutput(ctx context.Context, s string) (string, error) {
	if o.Format == nil || o.IsSynthetic == nil {
		return s, nil
	}
	v, ok := userValue(s)
	if !ok || v == wildcardValue || !o.IsSynthetic(v) {
		return s, nil
	}
	display, err := o.Format(ctx, v)
	if err != nil {
		return "", err
	}
	return userPrefix + display, nil
}

// userValue returns the value of a "user:<value>" subject and true, or "" and
// false for any non-user subject.
func userValue(s string) (string, bool) {
	if len(s) >= len(userPrefix) && s[:len(userPrefix)] == userPrefix {
		return s[len(userPrefix):], true
	}
	return "", false
}
