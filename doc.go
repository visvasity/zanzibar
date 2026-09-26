// Copyright (c) 2026 Visvasity LLC

// Package zanzibar is a minimalist relationship-based access control (ReBAC)
// library modeled on Google's Zanzibar. It answers the question "does subject S
// have relation R on object O?" and lets applications grant and revoke the
// relationships that back that decision.
//
// The authoritative, normative description of the model and its guarantees is
// SPEC.md in the module root. This doc comment is an informative overview; the
// SPEC governs where the two disagree.
//
// # Data model
//
// The atomic stored fact is a relation tuple, written object#relation@subject:
//
//	doc:readme#viewer@user:alice@example.com   // Alice may view doc:readme
//	doc:readme#viewer@group:eng#member         // members of group:eng may view it
//	doc:readme#parent@folder:proj              // doc:readme's parent is folder:proj
//	group:eng#member@user:bob@example.com      // Bob is a member of group:eng
//
// An object is "namespace:id" (for example doc:readme). A subject is one of
// three forms:
//
//   - a user, "user:<email>" — the only principal identity the library
//     understands; establishing who a user is (authentication, OAuth, sessions)
//     is out of scope and is the embedding application's responsibility;
//   - a userset, "namespace:id#relation" — every subject reachable from that
//     relation, which is how group membership (and nested groups) compose;
//   - an object, "namespace:id" — a bare object reference, used only as the
//     subject of a tupleset relation for parent inheritance.
//
// # Namespace configuration and userset rewrites
//
// Each namespace declares its relations and, per relation, a userset rewrite
// (see [Rewrite]) describing how that relation's set of subjects is computed
// from stored tuples and other relations:
//
//   - This        — subjects from tuples stored directly for object#relation;
//   - ComputedUserset — another relation on the same object (relation
//     implication, e.g. every editor is a viewer);
//   - TupleToUserset  — for each tuple object#tupleset@X where X is an object,
//     the relation R on X (the parent-inheritance primitive);
//   - Union, Intersection, Exclusion — set combinators over the above.
//
// Groups and permission inheritance are not special mechanisms; they are
// ordinary consequences of tuples plus rewrites. A group is just an object with
// a member relation; folder inheritance is just a TupleToUserset over a parent
// relation.
//
// Configuration is hybrid: defaults may be registered in Go with
// [WithNamespace] and overridden at runtime by configs stored in the database
// (see [Service.WriteConfig]). A stored config for a namespace takes precedence
// over the Go-registered default for that namespace.
//
// # Evaluation
//
// [Service.Check] decides membership by evaluating the effective rewrite,
// expanding userset subjects recursively (this is what resolves groups and
// nested groups) and following tupleset edges (this is what resolves parent
// inheritance). Evaluation is fail-closed: any error, unregistered namespace,
// undeclared relation, or exceeded recursion depth denies rather than allows,
// and cycles terminate without granting membership.
//
// Grants may be time-boxed: a tuple carries an optional validity interval (see
// [Mutation.NotBeforeUnixNano]), and Check accepts a caller-supplied evaluation
// time ([CheckRequest.AsOfUnixNano]). Tuples outside their interval at that time
// are treated as absent. The library keeps no clock, so the caller always
// supplies the time.
//
// # Consistency and persistence
//
// All state is persisted exclusively through the github.com/visvasity/kv
// key-value interface. Each read operation ([Service.Check], [Service.Expand],
// [Service.Read], [Service.ListObjects], [Service.ListUsers]) evaluates against
// a single kv.Snapshot so the whole operation observes one consistent
// point-in-time view; each mutating operation applies atomically within a
// single kv.Transaction. v1 provides snapshot-per-request consistency only:
// there are no client-visible consistency tokens (zookies).
//
// # HTTP surface
//
// [Service.Handler] returns the data-plane http.Handler (check, write, read,
// expand, list-objects, list-users) and [Service.ConfigHandler] the
// schema-administration handler; both are plain net/http handlers with no
// third-party dependency, so mount them wherever you like (net/http or any
// router that accepts an http.Handler), behind your own middleware. They are
// separate so the high-privilege config plane can sit behind stricter
// authorization. The library is identity-agnostic: it performs no authentication
// and makes no decision about who may call the admin surface — the embedding
// application MUST gate the mutating surface with its own authn/authz. See
// [Client] and [ConfigClient] for the matching typed clients.
package zanzibar
