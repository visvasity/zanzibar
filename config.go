// Copyright (c) 2026 Visvasity LLC

package zanzibar

// NamespaceConfig declares the relations of a single namespace and, for each
// relation, the userset rewrite that defines how its set of subjects is
// computed. It is the unit of hybrid configuration: an instance may be
// registered in Go with [WithNamespace] or stored in the database with
// [Service.WriteConfig], the stored form taking precedence.
//
// Its JSON encoding is, for example:
//
//	{
//	  "namespace": "doc",
//	  "relations": {
//	    "parent": { "this": {} },
//	    "owner":  { "this": {} },
//	    "editor": { "union": { "children": [
//	      { "this": {} },
//	      { "computedUserset": { "relation": "owner" } }
//	    ]}},
//	    "viewer": { "union": { "children": [
//	      { "this": {} },
//	      { "computedUserset": { "relation": "editor" } },
//	      { "tupleToUserset": { "tupleset": "parent", "computedUserset": "viewer" } }
//	    ]}}
//	  }
//	}
//
// A relation whose rewrite is the zero [Rewrite] is treated as This (direct
// tuples only).
type NamespaceConfig struct {
	// Namespace is the category of objects this config governs, matching
	// 1*( ALPHA / DIGIT / "_" / "-" ).
	Namespace string `json:"namespace"`

	// Version is the store-assigned, per-namespace revision (§5.5). It is 0 for
	// a namespace that has never been written. On [Service.WriteConfig] the
	// submitted Version is the compare-and-set expectation (0 to create); on a
	// successful write and on [Service.ReadConfig] it is the currently stored
	// revision. Callers do not choose the value; they read it back and pass it
	// to the next write.
	Version uint64 `json:"version"`

	// Relations maps each declared relation name to its userset rewrite.
	Relations map[string]Rewrite `json:"relations"`
}

// Rewrite is a userset rewrite expression: exactly one of its fields is
// non-nil, identifying the node kind. It mirrors the discriminated union
// described in SPEC.md §5.2. A Rewrite with zero or more than one non-nil field
// is invalid and is rejected at configuration-load time.
type Rewrite struct {
	// This selects the subjects stored directly for the relation being defined.
	This *This `json:"this,omitempty"`

	// ComputedUserset selects another relation on the same object, expressing
	// relation implication.
	ComputedUserset *ComputedUserset `json:"computedUserset,omitempty"`

	// TupleToUserset follows a tupleset relation to other objects and selects a
	// relation on each, expressing parent inheritance.
	TupleToUserset *TupleToUserset `json:"tupleToUserset,omitempty"`

	// Union is the set union of its children; membership holds if any child holds.
	Union *SetOperation `json:"union,omitempty"`

	// Intersection is the set intersection; membership holds only if every child holds.
	Intersection *SetOperation `json:"intersection,omitempty"`

	// Exclusion is set difference; membership holds if Base holds and Subtract does not.
	Exclusion *Exclusion `json:"exclusion,omitempty"`
}

// This is the userset rewrite node selecting subjects from tuples stored
// directly for the relation being defined. Userset subjects among them are
// expanded recursively during evaluation; object subjects are not.
type This struct{}

// ComputedUserset is the userset rewrite node selecting the subjects of another
// relation on the same object.
type ComputedUserset struct {
	// Relation is the other relation, on the same object, whose userset is included.
	Relation string `json:"relation"`
}

// TupleToUserset is the userset rewrite node implementing inheritance. For every
// tuple object#Tupleset@X where X is an object subject, it selects the subjects
// of X#ComputedUserset.
type TupleToUserset struct {
	// Tupleset is the relation whose object-valued subjects are followed.
	Tupleset string `json:"tupleset"`

	// ComputedUserset is the relation evaluated on each followed object.
	ComputedUserset string `json:"computedUserset"`
}

// SetOperation is the child list shared by the Union and Intersection rewrite
// nodes.
type SetOperation struct {
	// Children are the operand rewrites.
	Children []Rewrite `json:"children"`
}

// Exclusion is the operand pair of the exclusion (set-difference) rewrite node.
type Exclusion struct {
	// Base is the rewrite whose subjects are included.
	Base *Rewrite `json:"base"`

	// Subtract is the rewrite whose subjects are removed from Base.
	Subtract *Rewrite `json:"subtract"`
}
