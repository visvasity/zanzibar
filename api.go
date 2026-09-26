// Copyright (c) 2026 Visvasity LLC

package zanzibar

// CheckRequest asks whether Subject is a member of the userset Object#Relation.
type CheckRequest struct {
	// Object is "namespace:id".
	Object string `json:"object"`
	// Relation is the relation to test on Object.
	Relation string `json:"relation"`
	// Subject is the subject whose membership is tested.
	Subject string `json:"subject"`
	// AsOfUnixNano is the caller-supplied evaluation time for validity-interval
	// filtering (see [Mutation.NotBeforeUnixNano] and SPEC.md §6.6). A value <= 0
	// means "not supplied", which treats every interval-bearing tuple as
	// inactive; callers using time-based access must set it.
	AsOfUnixNano int64 `json:"asOfUnixNano,omitempty"`
}

// CheckResponse carries the boolean decision. Allowed is false for an ordinary
// negative result; it is not an error.
type CheckResponse struct {
	Allowed bool `json:"allowed"`
}

// WriteRequest is an ordered batch of mutations applied atomically.
type WriteRequest struct {
	Mutations []Mutation `json:"mutations"`
}

// WriteResponse reports the outcome of a successful [WriteRequest].
type WriteResponse struct {
	// Applied is the number of mutations that changed state; idempotent grants
	// and no-op revokes may not be counted.
	Applied int `json:"applied"`
}

// ReadRequest queries stored tuples (not computed membership). Empty fields are
// wildcards; at least Object or Subject should be set so the query maps onto an
// indexed prefix scan.
type ReadRequest struct {
	// Object, when set, restricts results to this exact "namespace:id".
	Object string `json:"object,omitempty"`
	// Namespace, when set, restricts results to objects in this namespace.
	Namespace string `json:"namespace,omitempty"`
	// Relation, when set, restricts results to this exact relation.
	Relation string `json:"relation,omitempty"`
	// Subject, when set, restricts results to this exact subject.
	Subject string `json:"subject,omitempty"`

	// PageSize bounds the number of tuples returned; zero selects a server default.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken continues a previous read; empty starts at the beginning.
	PageToken string `json:"pageToken,omitempty"`
}

// TupleRecord is a stored tuple together with its metadata, as returned by
// [Service.Read]. Unlike Check, Read reports stored facts and does not filter by
// a time window, so the validity-interval bounds are surfaced directly.
type TupleRecord struct {
	// Tuple is the identity triple.
	Tuple Tuple `json:"tuple"`
	// CreatedAtUnixNano is the recorded creation timestamp, or 0 if unset.
	CreatedAtUnixNano int64 `json:"createdAtUnixNano,omitempty"`
	// NotBeforeUnixNano and NotAfterUnixNano are the validity-interval bounds
	// (§6.6); 0 means unbounded on that side.
	NotBeforeUnixNano int64 `json:"notBeforeUnixNano,omitempty"`
	NotAfterUnixNano  int64 `json:"notAfterUnixNano,omitempty"`
}

// ReadResponse returns matching tuple records in deterministic key order.
type ReadResponse struct {
	Records []TupleRecord `json:"records"`
	// NextPageToken is non-empty when more results remain.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// ExpandRequest asks for the userset tree of Object#Relation.
type ExpandRequest struct {
	Object   string `json:"object"`
	Relation string `json:"relation"`
	// AsOfUnixNano is the evaluation time for validity-interval filtering
	// (SPEC.md §6.6); see [CheckRequest.AsOfUnixNano] for its meaning.
	AsOfUnixNano int64 `json:"asOfUnixNano,omitempty"`
}

// ExpandResponse returns the userset structure without flattening to leaf users.
type ExpandResponse struct {
	Tree UsersetNode `json:"tree"`
}

// ListObjectsRequest asks for the objects in Namespace on which Subject holds
// Relation.
type ListObjectsRequest struct {
	Namespace string `json:"namespace"`
	Relation  string `json:"relation"`
	Subject   string `json:"subject"`

	// AsOfUnixNano is the evaluation time for validity-interval filtering
	// (SPEC.md §6.6); see [CheckRequest.AsOfUnixNano] for its meaning.
	AsOfUnixNano int64 `json:"asOfUnixNano,omitempty"`

	PageSize  int    `json:"pageSize,omitempty"`
	PageToken string `json:"pageToken,omitempty"`
}

// ListObjectsResponse returns matching object ids (the id part only, since the
// namespace is fixed by the request) in deterministic order.
type ListObjectsResponse struct {
	ObjectIDs     []string `json:"objectIds"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}

// ListUsersRequest asks for the user subjects that are members of
// Object#Relation.
type ListUsersRequest struct {
	Object   string `json:"object"`
	Relation string `json:"relation"`

	// AsOfUnixNano is the evaluation time for validity-interval filtering
	// (SPEC.md §6.6); see [CheckRequest.AsOfUnixNano] for its meaning.
	AsOfUnixNano int64 `json:"asOfUnixNano,omitempty"`

	PageSize  int    `json:"pageSize,omitempty"`
	PageToken string `json:"pageToken,omitempty"`
}

// ListUsersResponse returns member emails, flattening usersets and inheritance
// but never expanding a "user:*" wildcard into concrete users.
type ListUsersResponse struct {
	Users         []string `json:"users"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}

// UsersetNodeKind identifies the kind of a [UsersetNode] in an expand tree.
type UsersetNodeKind string

const (
	// NodeThis is a leaf holding the subjects stored directly for a relation.
	NodeThis UsersetNodeKind = "this"
	// NodeComputedUserset links to another relation on the same object.
	NodeComputedUserset UsersetNodeKind = "computedUserset"
	// NodeTupleToUserset expands a tupleset relation into per-object subtrees.
	NodeTupleToUserset UsersetNodeKind = "tupleToUserset"
	// NodeUnion combines children by set union.
	NodeUnion UsersetNodeKind = "union"
	// NodeIntersection combines children by set intersection.
	NodeIntersection UsersetNodeKind = "intersection"
	// NodeExclusion is set difference of its first child minus its second.
	NodeExclusion UsersetNodeKind = "exclusion"
)

// UsersetNode is a node in the tree returned by [Service.Expand]. It is a
// recursive discriminated union keyed by Kind; only the fields relevant to Kind
// are populated.
type UsersetNode struct {
	// Kind identifies which rewrite node this represents.
	Kind UsersetNodeKind `json:"kind"`

	// Subjects holds the direct subject strings at a NodeThis leaf.
	Subjects []string `json:"subjects,omitempty"`

	// Children holds the operands of NodeUnion, NodeIntersection, and
	// NodeExclusion (Base then Subtract), and the per-object subtrees of
	// NodeTupleToUserset.
	Children []UsersetNode `json:"children,omitempty"`

	// Relation is the referenced relation for NodeComputedUserset and the
	// evaluated relation for NodeTupleToUserset.
	Relation string `json:"relation,omitempty"`

	// Tupleset is the followed relation for NodeTupleToUserset.
	Tupleset string `json:"tupleset,omitempty"`

	// Truncated marks a node whose expansion was cut off by the cycle guard or
	// the recursion-depth limit rather than fully expanded.
	Truncated bool `json:"truncated,omitempty"`
}
