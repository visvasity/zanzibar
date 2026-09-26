// Copyright (c) 2026 Visvasity LLC

package zanzibar

// Tuple is a relation tuple: the atomic stored fact binding a subject to a
// relation on an object. The triple (Object, Relation, Subject) is its primary
// key; a given triple exists at most once.
type Tuple struct {
	// Object is "namespace:id", for example "doc:readme". Its namespace must be
	// registered or stored.
	Object string `json:"object"`

	// Relation must be declared in the object namespace's effective config.
	Relation string `json:"relation"`

	// Subject is a user ("user:<email>"), a userset ("namespace:id#relation"),
	// or an object ("namespace:id"); see the package overview for the forms.
	Subject string `json:"subject"`
}

// MutationOp selects whether a [Mutation] grants or revokes its tuple.
type MutationOp string

const (
	// OpGrant upserts the tuple. Granting an existing tuple sets its validity
	// interval to the mutation's bounds (extending or shortening access) and is
	// otherwise idempotent.
	OpGrant MutationOp = "grant"

	// OpRevoke deletes the tuple. Revoking a non-existent tuple is a no-op
	// unless a precondition requires its presence.
	OpRevoke MutationOp = "revoke"
)

// Precondition is an optional guard evaluated against the current state before a
// [Mutation] is applied. If it fails, the entire enclosing [WriteRequest] is
// aborted.
type Precondition string

const (
	// PreconditionNone imposes no guard (the zero value).
	PreconditionNone Precondition = ""

	// PreconditionMustExist requires the tuple to exist before the mutation.
	PreconditionMustExist Precondition = "must_exist"

	// PreconditionMustNotExist requires the tuple to be absent before the mutation.
	PreconditionMustNotExist Precondition = "must_not_exist"
)

// Mutation is a single grant or revoke within a [WriteRequest]. All mutations
// in a request are applied atomically in one transaction.
type Mutation struct {
	// Op is grant or revoke.
	Op MutationOp `json:"op"`

	// Tuple is the relation tuple being granted or revoked.
	Tuple Tuple `json:"tuple"`

	// Precondition optionally guards the mutation; empty means no guard.
	Precondition Precondition `json:"precondition,omitempty"`

	// CreatedAtUnixNano optionally records a caller-supplied creation timestamp
	// on grant. The library keeps no clock of its own; a zero value records no
	// timestamp. This metadata never affects Check results.
	CreatedAtUnixNano int64 `json:"createdAtUnixNano,omitempty"`

	// NotBeforeUnixNano and NotAfterUnixNano define an optional half-open
	// validity interval [NotBefore, NotAfter) for a granted tuple, in
	// nanoseconds since the Unix epoch. A zero bound means unbounded on that
	// side. Outside the interval the tuple is inactive: Check treats it as
	// absent when evaluated with an AsOf time within scope. When both bounds are
	// non-zero, NotBefore must be strictly less than NotAfter. See the package
	// overview and SPEC.md §6.6.
	NotBeforeUnixNano int64 `json:"notBeforeUnixNano,omitempty"`
	NotAfterUnixNano  int64 `json:"notAfterUnixNano,omitempty"`
}
