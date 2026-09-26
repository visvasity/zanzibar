# Zanzibar — Minimalist Access Control Library

**Status:** Normative specification (v1 draft)
**Module:** `github.com/visvasity/zanzibar`
**Last updated:** 2026-09-25

This document is the normative specification for `zanzibar`, a minimalist
relationship-based access control (ReBAC) library modeled on Google's
[Zanzibar](https://research.google/pubs/pub48190/). It defines the data model,
the evaluation semantics, the persistence layout on top of the
`github.com/visvasity/kv` key-value API, the Go API, and the HTTP API
(`net/http` handlers) that higher-level applications mount on the HTTP server of
their choice.

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this
document are to be interpreted as described in RFC 2119.

---

## 1. Scope

### 1.1 Goals

1. Provide a small, dependency-light library that answers the question *"does
   subject S have relation R on object O?"* and lets applications grant and
   revoke such relationships.
2. Support **user groups** (a group is an object whose members are subjects)
   and arbitrary nesting of groups.
3. Support **computed usersets** (relation implication, e.g. an `editor` is
   also a `viewer`).
4. Support **parent-folder permission inheritance** (an object inherits
   permissions from a parent object, transitively).
5. Persist all state through the `kv.Database` interface only, using its
   transactions and snapshots for atomicity and point-in-time consistency.
6. Expose the check/grant/revoke/query operations as standard `net/http`
   handlers that the embedding application mounts on any HTTP server, with no
   third-party HTTP dependency.

### 1.2 Non-goals (explicitly out of scope)

1. **OAuth, authentication, session management, and user identity.** Users are
   identified solely by their email address (§3.2). Establishing *who* the
   caller is, and whether the caller is permitted to invoke the admin surface,
   is the responsibility of the embedding application (§14).
2. **Zookies / external-consistency tokens.** v1 uses snapshot-per-request
   consistency (§9). Client-visible consistency tokens are a non-goal;
   §18 discusses possible future work.
3. **Storage sharding, replication, watch/changefeed APIs, and caching
   tiers.** These are properties of the underlying `kv` backend, not of this
   library.
4. **A general policy/rule language beyond userset rewrites.** Attribute-based
   conditions (ABAC) — recurring schedules ("weekdays 09:00–17:00"), IP/device
   predicates, and request-context caveats — are out of scope for v1. The one
   exception is per-tuple **validity intervals** (a fixed `[not-before,
   not-after)` time window on a grant), which are supported as a bounded,
   first-class feature; see §6.6, §8, and §11.3.

---

## 2. Terminology

- **Namespace** — a category of objects (e.g. `doc`, `folder`, `group`). A
  namespace has a *configuration* (§5) that declares its relations.
- **Object** — an addressable entity `namespace:id` (e.g. `doc:readme`).
- **Relation** — a named edge type on a namespace (e.g. `viewer`, `member`,
  `parent`).
- **Subject** (a.k.a. *user* in the Zanzibar paper) — the entity a relation
  points at: a user, a userset, or an object reference (§3.2).
- **Relation tuple** (or **tuple**) — the atomic stored fact
  `object#relation@subject` (§3.3).
- **Userset** — the set of subjects reachable from `object#relation`.
- **Userset rewrite** — the expression that defines how a relation's userset is
  computed from stored tuples and other relations (§5.2).
- **Check** — the decision procedure that answers whether a subject is a member
  of a userset (§6).

---

## 3. Data model

### 3.1 Objects

An object is written `namespace:id`.

```
object      = namespace ":" id
namespace   = 1*( ALPHA / DIGIT / "_" / "-" )
id          = 1*( %x21-7E except "/" "#" ":" whitespace )   ; see §12
```

- `namespace` **MUST** match a registered/stored namespace configuration
  (§5) at evaluation time; writes to an object in an unknown namespace
  **MUST** be rejected.
- `id` is opaque to the library. It **MUST NOT** contain `/`, `#`, `:`, or
  ASCII whitespace, and **MUST** be non-empty (§12).

### 3.2 Subjects

A subject is one of three forms:

| Form | Syntax | Meaning |
|------|--------|---------|
| **User** | `user:<email>` | A concrete principal, identified by email. |
| **Userset** | `namespace:id#relation` | Every subject in the userset `namespace:id#relation`. |
| **Object** | `namespace:id` | A bare object reference; used only as the subject of a *tupleset* relation for inheritance (§7). |

```
subject     = user-subject / userset-subject / object-subject
user-subject    = "user:" email
userset-subject = object "#" relation
object-subject  = object
email       = ; a syntactically valid email address, see §12
relation    = 1*( ALPHA / DIGIT / "_" / "-" )
```

- Email addresses are the **only** user identity the library understands. The
  library **MUST NOT** interpret, normalize, or authenticate email beyond the
  canonicalization in §12.4. It stores and compares them as opaque strings
  after canonicalization.
- The optional wildcard user `user:*` **MAY** be supported by an
  implementation to mean "every user"; if supported it **MUST** be documented
  and it matches only *user* subjects, never usersets or objects. Whether
  `user:*` is enabled **MUST** be a per-namespace-relation configuration
  choice, off by default.

### 3.3 Relation tuples

A relation tuple binds a subject to a relation on an object:

```
tuple       = object "#" relation "@" subject
```

Examples:

```
doc:readme#viewer@user:alice@example.com        ; Alice may view doc:readme
doc:readme#viewer@group:eng#member              ; members of group:eng may view it
doc:readme#parent@folder:proj                   ; doc:readme's parent is folder:proj
group:eng#member@user:bob@example.com           ; Bob is a member of group:eng
group:eng#member@group:eng-backend#member       ; nested group membership
```

- The triple `(object, relation, subject)` is the primary key of a tuple. A
  given triple **MUST** exist at most once; a grant of an existing triple is
  idempotent (§8).
- A tuple **MAY** carry non-key metadata (§11.3): a creation timestamp, which
  is not part of tuple identity and **MUST NOT** affect Check results; and an
  optional **validity interval** `[not-before, not-after)`, which is likewise
  not part of tuple identity but **does** gate membership at Check time as
  defined in §6.6. Because the interval is metadata rather than identity, a
  given triple has at most one interval in effect at a time — a later grant of
  the same triple replaces the interval (§8.3).

---

## 4. Groups and computed usersets (informative)

Groups and relation implication are **not** special mechanisms; they fall out
of the tuple model plus userset rewrites (§5):

- A **group** is simply an object (conventionally in a `group` namespace) with
  a `member` relation. Membership is a tuple
  `group:eng#member@user:alice@example.com`. Nested groups are the tuple
  `group:eng#member@group:eng-backend#member`, resolved by the `_this` +
  userset-expansion rule (§6.3).
- A **computed userset** (e.g. "every `editor` is a `viewer`") is expressed by
  a `computed_userset` term in the `viewer` rewrite that references the
  `editor` relation (§5.2).

The normative behavior of both is defined by §5 and §6.

---

## 5. Namespace configuration

### 5.1 Structure

Each namespace has a configuration declaring its relations and, per relation, a
**userset rewrite** expression. Conceptually:

```
NamespaceConfig {
    Namespace : string
    Version   : uint64         ; store-assigned; see §5.5
    Relations : map[relation-name] Relation
}

Relation {
    Rewrite : Rewrite          ; how this relation's userset is computed
}
```

`Version` is a monotonically increasing revision number assigned by the store,
**per namespace**, on each successful write (§5.5). A namespace that has never
been written has no stored config and, by convention, version `0`. Callers do
not choose the value; they read it back and use it for compare-and-set writes
and for cache invalidation.

If a relation's `Rewrite` is absent/empty, it defaults to `_this` (direct
tuples only). A relation that is referenced by a tuple or a rewrite but not
declared in the namespace config **MUST** cause a validation error at write
time and **MUST** be treated as an empty userset at Check time (fail-closed).

### 5.2 Userset rewrite expressions

A `Rewrite` is exactly one of the following node kinds:

1. **`_this`** — the set of subjects from tuples stored directly for
   `object#relation` (the relation being defined). Usersets and objects stored
   as subjects are expanded per §6.3.

2. **`computed_userset { relation: R }`** — the userset of `object#R` on the
   **same** object. This expresses relation implication (e.g. `viewer`
   includes `editor`).

3. **`tuple_to_userset { tupleset: T, computed_userset: R }`** — for every
   tuple `object#T@X` where `X` is an **object** subject (§3.2), the userset
   `X#R`. This is the inheritance primitive (§7). If `X` is not an object
   subject, that tuple **MUST** be ignored by this node.

4. **`union { children: [...] }`** — set union of child rewrites. Membership
   holds if **any** child says so.

5. **`intersection { children: [...] }`** — set intersection. Membership holds
   only if **all** children say so.

6. **`exclusion { base: Rewrite, subtract: Rewrite }`** — set difference.
   Membership holds if `base` says so **and** `subtract` does not.

An implementation **MUST** support kinds 1–5. Kind 6 (`exclusion`) is
**RECOMMENDED**; if unsupported it **MUST** be rejected at config-load time
rather than silently ignored.

### 5.3 Canonical JSON encoding of a rewrite (normative)

```json
{ "this": {} }
{ "computedUserset": { "relation": "editor" } }
{ "tupleToUserset": { "tupleset": "parent", "computedUserset": "viewer" } }
{ "union":        { "children": [ <rewrite>, ... ] } }
{ "intersection": { "children": [ <rewrite>, ... ] } }
{ "exclusion":    { "base": <rewrite>, "subtract": <rewrite> } }
```

Exactly one top-level key **MUST** be present in a rewrite object; a rewrite
object with zero or more than one key **MUST** be rejected.

### 5.4 Example configuration

```json
{
  "namespace": "doc",
  "relations": {
    "parent": { "this": {} },

    "owner":  { "this": {} },

    "editor": {
      "union": { "children": [
        { "this": {} },
        { "computedUserset": { "relation": "owner" } }
      ]}
    },

    "viewer": {
      "union": { "children": [
        { "this": {} },
        { "computedUserset": { "relation": "editor" } },
        { "tupleToUserset": { "tupleset": "parent", "computedUserset": "viewer" } }
      ]}
    }
  }
}
```

This says: an owner is an editor; an editor is a viewer; and a viewer of a
document is also anyone who is a viewer of its `parent` (folder inheritance).

### 5.5 Stored configuration and compare-and-set

Namespace configuration lives **only** in the `kv` database; it is the single
source of truth. There is no in-process schema registry and no Go-registered
defaults: the library provides no `WithNamespace`-style seeding. Applications
create and evolve schema exclusively through `WriteConfig` (§13.3), and a
freshly initialized database has no namespaces until they do.

Resolution of the effective config for a namespace is therefore simple:

1. If a stored config exists in the database (§11.2) for the namespace, it is
   the effective config.
2. Otherwise the namespace is **unregistered**: all Check results against it are
   empty (fail-closed) and all writes to it **MUST** be rejected.

Because there is no in-memory fallback, every evaluation reads the effective
config from the operation's snapshot (§6.1). Implementations **MAY** cache
configs keyed by `(namespace, Version)` and **MUST** invalidate on a version
change; they **MUST** observe a committed config change before serving requests
accepted after the commit.

**Compare-and-set writes.** `WriteConfig` uses optimistic concurrency on
`Version` to make schema evolution race-safe (important under rolling upgrades,
where more than one writer may exist):

- To **create** a namespace, the caller submits a config whose `Version` is `0`
  (or unset). The write succeeds only if no config is currently stored; the
  store assigns `Version = 1`.
- To **update** a namespace, the caller submits a config whose `Version` equals
  the currently stored version (obtained via `ReadConfig`). The write succeeds
  only if the stored version still matches; the store assigns
  `stored.Version + 1`.
- If the submitted `Version` does not match the stored state, the write **MUST**
  be rejected with a conflict error (§15) and **MUST NOT** modify anything. The
  caller re-reads and retries (read-modify-write).

A successful `WriteConfig` **MUST** return the stored config carrying its newly
assigned `Version`.

**Version history.** Every successful write is retained: the store keeps an
immutable record of the `NamespaceConfig` at each version (§11.2), not just the
current one. Old versions are readable via `ReadConfigVersion` and enumerable via
`ListConfigVersions` (§13.3), which enables audit and rollback and reserves the
option of version-pinned evaluation later (§18). **Rollback** is performed
forward, not by mutating history: read the desired old version, then
`WriteConfig` its relations with the *current* `Version` as the compare-and-set
expectation; this appends a new version whose content equals the old one. History
records are never rewritten. A retention/GC policy is future work (§18).

**Bootstrap discipline.** Because a fresh database fails closed until a config
is written, schema creation/evolution **SHOULD** be performed as a deliberate,
idempotent migration step (operator/CI-driven), **not** as a blind write on
every process startup — the latter, combined with replace semantics, would let
a stale binary clobber a newer schema. Compare-and-set makes such a clobber fail
rather than corrupt, but the migration discipline avoids the churn entirely.
Schema changes **SHOULD** be **additive** (introduce new relations/namespaces;
do not repurpose or remove an existing relation in place) so that binaries built
against an older revision continue to evaluate correctly against a newer stored
config during a rollout.

Loading or storing a config **MUST** validate it (§12.5) and **MUST** reject
configs that reference undeclared relations, contain rewrite cycles that are
statically detectable, or violate §5.2/§5.3. Configuration changes **MUST NOT**
retroactively rewrite stored tuples; they only change how tuples are
interpreted at Check time.

---

## 6. Check semantics

`Check(object, relation, subject)` returns a boolean: whether `subject` is a
member of the userset `object#relation`.

### 6.1 Consistency

Each top-level `Check` **MUST** be evaluated against a single
`kv.Snapshot` (§9) opened at the start of the call. Every read performed while
evaluating that Check — direct tuples, nested usersets, parent chains, and
effective configs — **MUST** use the same snapshot, so the whole evaluation
observes one point-in-time view. The snapshot **MUST** be discarded when the
Check returns.

### 6.2 Evaluation of a rewrite

`Check` evaluates the effective rewrite of `(namespace(object), relation)`:

- **`_this`**: return true iff a direct tuple `object#relation@subject` exists,
  or a direct tuple `object#relation@X` exists where `X` is a userset subject
  and `Check(X.object, X.relation, subject)` is true (§6.3). A `user:*`
  wildcard tuple, if enabled (§3.2), matches any user subject.
- **`computed_userset{R}`**: return `Check(object, R, subject)`.
- **`tuple_to_userset{T, R}`**: return true iff there exists a direct tuple
  `object#T@X` with `X` an object subject such that `Check(X, R, subject)` is
  true.
- **`union`**: true iff any child is true (short-circuit on first true).
- **`intersection`**: true iff every child is true (short-circuit on first
  false).
- **`exclusion{base, subtract}`**: true iff `base` is true and `subtract` is
  false.

### 6.3 Subject expansion under `_this`

When `_this` reads a stored subject `X`:

- If `X` equals the query subject exactly, membership holds.
- If `X` is a **userset** `o#r`, evaluate `Check(o, r, subject)` recursively.
  This is what makes group membership and nested groups work.
- If `X` is a bare **object** subject, it is **not** expanded by `_this`; such
  subjects are only meaningful to `tuple_to_userset` (§7). It contributes to
  membership only if it equals the query subject exactly (which is only
  possible when the query subject is itself an object).

### 6.4 Termination: cycles and depth

Relationship graphs and misconfigured rewrites can be cyclic. Implementations
**MUST** guarantee termination:

1. The evaluator **MUST** maintain a visited set of `(object, relation,
   subject)` evaluation nodes on the current path. Re-entering an
   already-visited node **MUST** return false for that node (a subject is not a
   member of a userset merely by virtue of a cycle).
2. The evaluator **MUST** enforce a configurable maximum recursion depth
   (default **RECOMMENDED** 100). Exceeding it **MUST** cause the Check to fail
   with an error (§15), not to silently return true or false, so the caller
   learns the graph is too deep.

### 6.5 Fail-closed principle

Any condition that prevents a definitive positive answer — unregistered
namespace, undeclared relation, unsupported rewrite kind, depth exceeded,
storage error — **MUST NOT** yield an allow. Errors propagate to the caller;
absence of a tuple yields deny.

### 6.6 Time-based access: validity intervals

A direct tuple **MAY** carry a half-open **validity interval** `[NotBefore,
NotAfter)`, expressed as two `int64` nanosecond-since-Unix-epoch bounds stored
in its metadata (§11.3). A bound of `0` means unbounded on that side, so
`NotAfter` alone expresses "valid until", `NotBefore` alone expresses "valid
from", and both zero (the common case) is an always-valid grant.

`Check`, `Expand`, `ListObjects`, and `ListUsers` requests **MAY** carry an
evaluation time `AsOf` (nanoseconds since Unix epoch). Because the library
holds no clock of its own (§11.3), `AsOf` **MUST** be supplied by the caller.

The following rules define interval semantics:

1. **Fixed for the operation.** The `AsOf` value **MUST** be fixed for the
   entire top-level operation and threaded unchanged through all recursion,
   exactly as the snapshot is (§6.1). `AsOf` and the snapshot are independent
   axes: the snapshot selects which writes are *visible*; `AsOf` selects which
   time-windowed tuples are *active*.

2. **Filtering applies only at `_this` leaves**, where tuples are stored. A
   direct tuple is **active** at `AsOf` iff
   `(NotBefore == 0 || NotBefore <= AsOf) && (NotAfter == 0 || AsOf < NotAfter)`.
   An inactive tuple contributes nothing: neither a direct subject match nor a
   userset/object to expand. `computed_userset` and `tuple_to_userset` carry no
   interval of their own; they recurse, and the filter applies at the leaves
   they reach.

3. **Fail-closed without `AsOf`.** If a request does not supply `AsOf` (a value
   `<= 0` means "not supplied"), tuples that carry no interval (both bounds `0`)
   match as usual, but any tuple carrying a non-zero `NotBefore` or `NotAfter`
   **MUST** be treated as inactive. Callers that use time-based access
   **MUST** supply `AsOf`; this keeps evaluation deterministic and preserves the
   no-ambient-clock principle.

4. **Expand** evaluated with `AsOf` **MUST** reflect the active membership as of
   that time (inactive leaves excluded); an implementation **MAY** annotate
   surviving leaves with their intervals.

The interval is a single fixed window only. Recurring schedules and
request-context predicates are out of scope (§1.2) and belong to a future
conditions feature (§18).

---

## 7. Parent-folder inheritance (informative)

Inheritance is `tuple_to_userset`. Given the config in §5.4 and tuples:

```
folder:proj#viewer@user:alice@example.com
doc:readme#parent@folder:proj
```

`Check(doc:readme, viewer, user:alice@example.com)` proceeds:

1. `viewer` = union(`_this`, `computedUserset{editor}`, `tupleToUserset{parent, viewer}`).
2. `_this` and `editor` yield no match.
3. `tupleToUserset{parent, viewer}` finds `doc:readme#parent@folder:proj`
   (X = `folder:proj`, an object subject) and evaluates
   `Check(folder:proj, viewer, user:alice@example.com)`, which is true via
   `folder:proj`'s own `_this`.

Chains of `folder → folder → ...` inherit transitively because each level's
`viewer` rewrite again contains the `tuple_to_userset{parent, viewer}` term.
Cycle protection (§6.4) bounds pathological parent loops.

---

## 8. Write semantics

### 8.1 Operations

A **Write** request carries an ordered list of mutations, each one of:

- **grant** (upsert) the exact tuple `object#relation@subject`,
- **revoke** (delete) the exact tuple `object#relation@subject`, or
- **delete** every tuple stored on `object` (`object#*@*`) — the tuple carries
  only `object` (relation and subject empty).

`delete` is the operation to run when the object itself is deleted: it removes
all permissions granted **on** that object in one mutation. Removing those tuples
is sufficient for correctness — any remaining reference to the object as a
*subject* (a userset `X#r@object#rel` or an object subject `X#r@object`) resolves
to the empty set and grants nobody (fail-closed). Such references are inert
leftovers; a host that also wants to reclaim them sweeps by subject (§10.2 Read
by subject) and revokes those tuples too.

### 8.2 Atomicity

All mutations in one Write request **MUST** be applied within a single
`kv.Transaction` and committed atomically: either all take effect or none do.
On commit conflict (§9.3) the implementation **MUST** either retry the whole
request or return a conflict error; it **MUST NOT** partially apply.

### 8.3 Idempotence and preconditions

- Granting a tuple whose triple already exists **MUST** set its validity
  interval (§6.6) to the bounds carried by the mutation — this is how access is
  extended or shortened — and is otherwise idempotent. A grant carrying the same
  interval as the stored tuple is a true no-op. The creation timestamp
  **MUST NOT** be reset by a redundant grant.
- Revoking a non-existent tuple is a no-op success by default; a delete of an
  object with no stored tuples is likewise a no-op.
- A grant or revoke **MAY** carry optional preconditions (e.g. "tuple must
  exist" / "must not exist"); if a precondition fails the whole transaction
  **MUST** be aborted with a precondition error (§15). A precondition on a
  **delete** **MUST** be rejected as invalid.

### 8.4 Validation at write time

Each granted/revoked tuple **MUST** be validated (§12) before commit:

- `object`'s namespace **MUST** be registered/stored (§5.5).
- `relation` **MUST** be declared in that namespace's effective config.
- `subject` **MUST** be a syntactically valid user, userset, or object
  subject; a userset/object subject's namespace **MUST** be registered.

A **delete** validates only that `object` is syntactically well-formed; it does
**not** require the namespace to still be registered, because it operates on
stored keys and must be able to clean up even after a config change.
- The validity interval (§6.6), if present, **MUST** be well-formed: when both
  `NotBefore` and `NotAfter` are non-zero, `NotBefore` **MUST** be strictly less
  than `NotAfter`. An empty or inverted interval **MUST** be rejected with an
  invalid-argument error (§15).
- Config writes (creating/updating a `NamespaceConfig`) go through the same
  transactional path and **MUST** be validated per §5.5.

An invalid mutation **MUST** abort the entire Write transaction.

### 8.5 Deletion log and garbage collection (optional)

Deleting an object (§8.1) removes the tuples **on** it but leaves references to it
as a *subject* elsewhere. Those references are inert — they resolve to the empty
set and grant nobody — so cleaning them is a space/hygiene concern, not a
correctness one. An implementation **MAY** offer an optional background collector
to reclaim them, gated behind a construction option (off by default).

When enabled:

- An `OpDelete` **MUST**, in the same transaction, record a **tombstone** for the
  deleted object.
- A grant on an object **MUST** clear that object's tombstone (a re-created object
  is revived, and its inbound references must be preserved).
- The collector processes tombstoned objects: for each, it deletes every tuple
  that names the object as a subject — both bare-object (`X#r@object`) and userset
  (`X#r@object#rel`) forms, found via the reverse index — and then removes the
  tombstone. It **MUST** read the tombstone within the same transaction as the
  sweep, so a concurrent revival (which clears the tombstone) is serialized against
  it: revival wins and the references are kept; otherwise the sweep proceeds.

Removing the deletion log or never running the collector affects only storage
reclamation, never a Check decision. Because "an object has no tuples" is **not**
a valid deletion signal (an object can be legitimately created-but-empty), the
collector relies on the explicit tombstone, not on tuple absence.

---

## 9. Consistency and concurrency

1. **Snapshot reads.** `Check`, `Expand`, `Read`, `ListObjects`, and
   `ListUsers` **MUST** each acquire one `kv.Snapshot` for the whole operation
   and **MUST** `Discard` it before returning.
2. **Transactional writes.** All mutating operations **MUST** use
   `kv.Transaction` and **MUST** honor its `Commit` semantics, including the
   `kv` requirement that a committed transaction is never falsely reported as
   failed (the backend retries to confirm final status).
3. **Conflicts.** On `Commit` returning a conflict, the implementation
   **SHOULD** retry a bounded number of times with fresh reads; on exhaustion
   it **MUST** return a conflict error (§15) rather than looping unbounded.
4. **No cross-request consistency tokens.** v1 provides
   snapshot-per-request consistency only. A Check reflects all writes that had
   committed before the snapshot was taken; it makes no "at least as fresh as
   token X" guarantee (§18).

---

## 10. Expand, Read, ListObjects, ListUsers

### 10.1 Expand

`Expand(object, relation)` returns the **userset tree** for `object#relation`
without flattening to leaf users — i.e. the structure of unions,
intersections, exclusions, `computed_userset` links, `tuple_to_userset`
expansions, and the direct subjects at the leaves. Expand is intended for
debugging and administrative introspection.

- Expand **MUST** use one snapshot (§9) and **MUST** apply the same cycle and
  depth limits as Check (§6.4); a node cut off by a cycle or the depth limit
  **MUST** be marked as such (`Truncated`) in the returned tree rather than
  omitted silently.
- Expand **MUST NOT** resolve `user:*` wildcards into a concrete user list.
- `_this` leaves list the direct stored subjects verbatim; a **userset** subject
  is listed as-is and **not** recursively expanded (a caller may Expand it
  separately). `computed_userset` expands into the same object's target relation;
  `tuple_to_userset` expands into one child subtree per active parent object.
  Each relation node carries the `Object` it expands, so cross-object subtrees
  remain identifiable. Inactive tuples at the request `AsOf` are excluded (§6.6).

### 10.2 Read

`Read` queries **stored tuples** (not computed membership) matching a filter.
The filter **MAY** constrain any subset of: `object` (exact), `namespace`,
`relation` (exact), and `subject` (exact). At least one of `object` or
`subject` **SHOULD** be provided so the query maps onto an indexed prefix scan
(§11). Results **MUST** be returned in a deterministic order (key order, §11.4)
and **MUST** support pagination via an opaque continuation token.

Each result is a **`TupleRecord`**: the tuple triple together with its stored
metadata (§11.3) — the creation timestamp and the validity interval
`[NotBefore, NotAfter)`. Read returns records regardless of any time window;
it does **not** filter by an `AsOf` (it reports stored facts, not active
membership), so callers inspecting time-boxed grants see the bounds directly.

### 10.3 ListObjects

`ListObjects(namespace, relation, subject)` returns the set of object ids in
`namespace` for which `Check(namespace:id, relation, subject)` would be true.

- The implementation **MUST** produce a sound and complete result relative to
  the Check semantics for the snapshot taken: every returned object **MUST**
  pass Check, and no object that would pass Check **MUST** be omitted, subject
  to the depth limit (§6.4).
- A conforming implementation **MAY** compute this by (a) gathering candidate
  objects via the reverse index (§11.4) — direct tuples, group memberships the
  subject transitively belongs to, and parent-chain expansions implied by the
  namespace rewrites — and (b) confirming each candidate with a full Check.
  This may over-read then filter; implementations **SHOULD** document the cost.
- Results **MUST** be paginated with an opaque continuation token and **MUST**
  have a deterministic order.

### 10.4 ListUsers

`ListUsers(object, relation)` returns the **user** subjects (emails) that are
members of `object#relation`, flattening usersets and parent inheritance but
**not** expanding `user:*` into concrete users.

- ListUsers **MUST** use one snapshot and the §6.4 limits.
- Because a fully-flattened userset can be large, ListUsers **MUST** be
  paginated with an opaque continuation token, and an implementation **MAY**
  impose and document a configurable maximum result size.

---

## 11. Persistence layout (KV mapping)

All state is stored through the `kv` API. Keys are strings; values are gob-
encoded via `kvutil.SetGob`/`GetGob` and range-scanned via `kvutil.AscendGob`
and `kvutil.PrefixRange`. The empty string is never used as a key (per `kv`).

### 11.1 Key prefix and record classes

All keys **MUST** live under a single configurable prefix `P` (default
`"zz1/"`; the `1` is a layout-version marker reserved for future migrations).
Record classes are distinguished by a one-character discriminator immediately
after `P`:

| Class | Key pattern | Value |
|-------|-------------|-------|
| Config head | `P + "c/" + namespace` | gob `NamespaceConfig` (the current version) |
| Config history | `P + "h/" + namespace + "/" + enc(Version)` | gob `NamespaceConfig` (immutable) |
| Forward tuple | `P + "t/" + object + "/" + relation + "/" + subject` | gob `TupleMeta` |
| Reverse tuple | `P + "s/" + subject + "/" + object + "/" + relation` | gob `TupleMeta` (or empty) |

Because `object`, `relation`, and `subject` are forbidden from containing `/`
(§12), `/` is an unambiguous field delimiter and no additional escaping is
required. Implementations **MUST** enforce that invariant at write time.

`enc(Version)` is an **order-preserving, fixed-width** encoding of the `uint64`
version (e.g. big-endian 8 bytes, or a zero-padded 20-digit decimal) so that a
history prefix scan yields versions in ascending numeric order.

### 11.2 Config records and version history

Config storage is **append-only and versioned** — v1 retains the full history of
every namespace's config, not just the current one:

- The **head** record `P+"c/"+namespace` holds the *current* `NamespaceConfig`
  and is the fast path for effective-config resolution: a single `GetGob` yields
  the config and its `Version`. Config reads during a Check **MUST** use the
  Check's snapshot (§6.1).
- For each version `v` ever written, an immutable **history** record
  `P+"h/"+namespace+"/"+enc(v)` holds the `NamespaceConfig` as of that version.
  A history record, once written, **MUST NOT** be modified or deleted in v1;
  `WriteConfig` only ever appends the next version.
- A successful `WriteConfig` (§5.5) **MUST**, within one transaction, update the
  head record and append the new history record, so the two never diverge.
- Enumerate namespaces by scanning `PrefixRange(P+"c/")` (head records only).
  Enumerate one namespace's versions by scanning
  `PrefixRange(P+"h/"+namespace+"/")`.

Retaining full history is what lets v1 support audit and rollback (§13.3) and
keeps the door open for version-pinned evaluation later (§18) without a storage
migration. A retention/GC policy for old versions is out of scope for v1 (§18);
absent it, history grows by one small record per config change, which is
negligible at typical schema-change cadence.

### 11.3 Tuple records and metadata

Both the forward and reverse records for a tuple **MUST** be written and
deleted together within the same transaction, so the two indexes never diverge.

`TupleMeta` **MUST** be small. Its `CreatedAtUnixNano` **MUST NOT** affect Check.
Its validity-interval bounds `NotBeforeUnixNano`/`NotAfterUnixNano` **do** gate
membership, per §6.6:

```
TupleMeta {
    CreatedAtUnixNano : int64   ; optional creation stamp; never affects Check
    NotBeforeUnixNano : int64   ; validity-interval lower bound; 0 = unbounded
    NotAfterUnixNano  : int64   ; validity-interval upper bound; 0 = unbounded
}
```

The library takes no wall-clock dependency of its own: every timestamp — the
creation stamp, the interval bounds, and the `AsOf` used to evaluate a Check
(§6.6) — **MUST** be supplied by the caller (the host provides the clock),
keeping the core deterministic and testable.

### 11.4 Index usage

- **Direct subjects of `object#relation`** (needed by `_this` and
  `tuple_to_userset`): scan `PrefixRange(P+"t/"+object+"/"+relation+"/")`.
- **Objects a subject is directly bound to** (needed by `Read` by subject and
  by `ListObjects` candidate gathering): scan
  `PrefixRange(P+"s/"+subject+"/")`.
- **Read by object**: scan `PrefixRange(P+"t/"+object+"/")` (optionally
  further constrained by `+relation+"/"`).

All scans use `kv.Ranger` ascending order, so results are in lexicographic key
order; that order is the deterministic pagination order (§10.2), and
continuation tokens **MUST** encode the last-seen key (opaquely).

---

## 12. Validation and canonicalization

### 12.1 Namespaces and relations

`namespace` and `relation` **MUST** match `1*( ALPHA / DIGIT / "_" / "-" )`
and be non-empty. Comparison is case-sensitive.

### 12.2 Object ids

`id` **MUST** be non-empty and **MUST NOT** contain `/`, `#`, `:`, or ASCII
whitespace or control characters.

### 12.3 Subjects

A subject string **MUST** parse unambiguously into exactly one of the three
forms in §3.2. The presence of `#` marks a userset subject; the `user:` prefix
marks a user subject; otherwise it is an object subject. Object and userset
subject components are validated per §12.1–§12.2.

### 12.4 Emails

- The library treats the email as an opaque identifier and performs only
  minimal canonicalization: it **MUST** reject an email containing `/`, `#`,
  ASCII whitespace, or control characters (so it cannot corrupt the key
  layout), and it **MUST** require exactly one `@` with non-empty local and
  domain parts.
- The library **MUST NOT** lowercase, Unicode-normalize, or otherwise rewrite
  the email unless explicitly configured to, because identity equivalence is
  the embedding application's policy, not this library's. If case-folding is
  desired it **MUST** be a documented, opt-in option applied consistently to
  both writes and checks.

### 12.5 Config validation

A `NamespaceConfig` **MUST** be rejected if any relation's rewrite: has zero or
multiple node keys (§5.3); references (via `computed_userset` or
`tuple_to_userset`) a relation not declared in **some** namespace reachable by
the reference (a `computed_userset` relation **MUST** exist on the same
namespace; a `tuple_to_userset` tupleset relation **MUST** exist on the same
namespace); or forms a statically-detectable `computed_userset` cycle within a
single object.

---

## 13. Go API

The Go API is the primary integration surface; the HTTP API (§14) is a thin
wrapper over it. Exact identifiers are illustrative but the shapes are
normative.

### 13.1 Construction

```go
// Service is the access-control engine. It is safe for concurrent use.
type Service struct { /* ... */ }

// New constructs a Service backed by the given kv.Database.
func New(db kv.Database, opts ...Option) (*Service, error)

type Option func(*config) error
```

### 13.2 Options

```go
// Note: there is no WithNamespace option. Namespace configs live only in the
// database and are created/evolved via WriteConfig (§5.5).

// WithKeyPrefix overrides the default "zz1/" key prefix (§11.1).
func WithKeyPrefix(prefix string) Option

// WithMaxDepth sets the recursion depth limit (§6.4); default 100.
func WithMaxDepth(n int) Option

// WithCommitRetries bounds transaction-conflict retries (§9.3).
func WithCommitRetries(n int) Option

// WithEmailCaseFold enables case-folding of user emails (§12.4); off by default.
func WithEmailCaseFold(bool) Option
```

### 13.3 Core methods

Every read method opens and discards one snapshot; every write method runs one
transaction (§9).

```go
func (s *Service) Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error)
func (s *Service) Write(ctx context.Context, req *WriteRequest) (*WriteResponse, error)
func (s *Service) Read(ctx context.Context, req *ReadRequest) (*ReadResponse, error)
func (s *Service) Expand(ctx context.Context, req *ExpandRequest) (*ExpandResponse, error)
func (s *Service) ListObjects(ctx context.Context, req *ListObjectsRequest) (*ListObjectsResponse, error)
func (s *Service) ListUsers(ctx context.Context, req *ListUsersRequest) (*ListUsersResponse, error)

// Config administration (stored configs, §5.5). WriteConfig performs a
// compare-and-set on cfg.Version (0 to create) and returns the stored config
// carrying its newly assigned Version; a version mismatch is a conflict error.
func (s *Service) WriteConfig(ctx context.Context, cfg *NamespaceConfig) (*NamespaceConfig, error)
func (s *Service) ReadConfig(ctx context.Context, namespace string) (*NamespaceConfig, error)
func (s *Service) ListConfigs(ctx context.Context) ([]NamespaceConfig, error)

// Version history (§5.5, §11.2). ReadConfigVersion returns the immutable config
// at a specific past version; ListConfigVersions returns the stored versions in
// ascending order.
func (s *Service) ReadConfigVersion(ctx context.Context, namespace string, version uint64) (*NamespaceConfig, error)
func (s *Service) ListConfigVersions(ctx context.Context, namespace string) ([]uint64, error)
```

### 13.4 Request/response types (essential shapes)

```go
type Tuple struct {
    Object   string // "namespace:id"
    Relation string
    Subject  string // user:/userset/object form (§3.2)
}

// AsOfUnixNano is the caller-supplied evaluation time (§6.6); <= 0 means
// "not supplied", which treats every interval-bearing tuple as inactive.
type CheckRequest  struct { Object, Relation, Subject string; AsOfUnixNano int64 }
type CheckResponse struct { Allowed bool }

type Mutation struct {
    Op    string // "grant" | "revoke" | "delete" (delete: Tuple has only Object)
    Tuple Tuple
    // Optional precondition, e.g. "must_exist" | "must_not_exist" (§8.3).
    Precondition string
    // Optional caller-supplied creation timestamp (§11.3).
    CreatedAtUnixNano int64
    // Optional validity interval (§6.6); 0 bound = unbounded on that side.
    // On grant these set/replace the stored tuple's window.
    NotBeforeUnixNano int64
    NotAfterUnixNano  int64
}
type WriteRequest  struct { Mutations []Mutation }
type WriteResponse struct { Applied int }

type ReadRequest struct {
    // Filter; empty fields are wildcards. At least Object or Subject SHOULD be set.
    Object, Namespace, Relation, Subject string
    PageToken string
    PageSize  int
}
type TupleRecord struct {
    Tuple             Tuple
    CreatedAtUnixNano int64 // 0 if unset
    NotBeforeUnixNano int64 // validity-interval bounds (§6.6); 0 = unbounded
    NotAfterUnixNano  int64
}
type ReadResponse struct { Records []TupleRecord; NextPageToken string }

type ExpandRequest  struct { Object, Relation string; AsOfUnixNano int64 }
type ExpandResponse struct { Tree UsersetNode }

type ListObjectsRequest  struct { Namespace, Relation, Subject string; AsOfUnixNano int64; PageToken string; PageSize int }
type ListObjectsResponse struct { ObjectIDs []string; NextPageToken string }

type ListUsersRequest  struct { Object, Relation string; AsOfUnixNano int64; PageToken string; PageSize int }
type ListUsersResponse struct { Users []string; NextPageToken string }
```

`UsersetNode` is a recursive discriminated union mirroring §5.2 (a `kind`
field plus per-kind children and, at leaves, the direct subjects), an `object`
field naming the object a relation node expands, and a boolean marking nodes
truncated by cycle/depth limits (§10.1).

---

## 14. HTTP API

### 14.1 Mounting

The library exposes its operations as two `http.Handler` values that the host
mounts wherever it likes (`net/http` or any router that accepts an
`http.Handler`), so it depends on no particular server type and pulls in no
third-party HTTP package:

```go
// Handler serves the data plane: check, write, read, expand, list-objects,
// list-users (each at "/<suffix>" relative to the handler root).
func (s *Service) Handler() http.Handler

// ConfigHandler serves the schema-administration plane: write, read, list,
// read-version, list-versions.
func (s *Service) ConfigHandler() http.Handler
```

The two planes are separate handlers so the host can mount them at different
paths and behind different authorization: the config plane rewrites a
namespace's entire authorization semantics and is high-privilege (§16). Each
handler serves its endpoints at a leading-slash path relative to its root, so it
is mounted with prefix stripping, for example:

```go
mux.Handle("/api/authz/", http.StripPrefix("/api/authz", svc.Handler()))
mux.Handle("/api/authz-admin/", http.StripPrefix("/api/authz-admin", svc.ConfigHandler()))
```

The host controls exposure by where it mounts each handler, which listeners it
serves them on, and the authn/authz middleware it wraps them with (§14.4).

### 14.2 Endpoints

All endpoints use `POST` with request body content-type `application/json` or
`application/gob`. The response is encoded in the **same** content-type as the
request. The handlers are plain `net/http` handlers with no third-party
dependency.

Data plane ([Service.Handler], paths relative to its mount root):

| Method + Path | Request | Response |
|---------------|---------|----------|
| `POST /check`        | `CheckRequest`       | `CheckResponse` |
| `POST /write`        | `WriteRequest`       | `WriteResponse` |
| `POST /read`         | `ReadRequest`        | `ReadResponse` |
| `POST /expand`       | `ExpandRequest`      | `ExpandResponse` |
| `POST /list-objects` | `ListObjectsRequest` | `ListObjectsResponse` |
| `POST /list-users`   | `ListUsersRequest`   | `ListUsersResponse` |

Config plane ([Service.ConfigHandler], paths relative to its mount root):

| Method + Path | Request | Response |
|---------------|---------|----------|
| `POST /write`         | `NamespaceConfig` (with expected `Version`) | `NamespaceConfig` (with assigned `Version`) |
| `POST /read`          | `{ "namespace": ... }` | `NamespaceConfig` |
| `POST /list`          | `{}`                 | `{ "configs": [ ... ] }` |
| `POST /read-version`  | `{ "namespace": ..., "version": N }` | `NamespaceConfig` |
| `POST /list-versions` | `{ "namespace": ... }` | `{ "versions": [ ... ] }` |

The request/response bodies are the JSON/gob encodings of the Go types in §13.
A logical error is returned in an envelope (`{ "error": { "code", ... } }`) so a
typed client reconstructs the category; see §14.3 and §14.5.

### 14.3 Error and status conventions

A successful call and a logical failure both return HTTP **200** with a
response envelope: `{ "data": <RESP> }` on success, or
`{ "error": { "code", "message", ... } }` when the operation fails. Malformed
requests (bad method, unsupported content-type, undecodable body) return `4xx`
with a plain-text body. This convention lets a typed client reconstruct the
error category (§15) rather than lose it:

- Callers **MUST** treat a non-200 status as a transport/format failure.
- Callers **MUST** inspect the decoded envelope's `error` field to detect a
  logical failure (validation error, precondition failure, depth exceeded,
  conflict) even on 200 (§15).
- A successful `check` returns `{ "data": { "allowed": true|false } }` and no
  `error`. `allowed:false` is a normal negative decision, **not** an error.

### 14.4 Authentication and authorization of the HTTP surface

Per §1.2, the library is identity-agnostic:

- The library **MUST NOT** implement authentication for its endpoints.
- The embedding application **MUST** wrap or gate the mutating surface (the data
  plane's `/write` and the entire config plane) — and, where appropriate, the
  read endpoints — with its own authn/authz middleware before exposing the
  handlers. The two-handler split (§14.1) exists so the high-privilege config
  plane can be mounted separately and behind stricter authorization.
- The authenticated caller's email, when the host wants the library to record
  it or use it in self-service checks, is passed **explicitly** in the request
  (e.g. inside a `Subject`), never inferred by the library from transport
  state. The library evaluates using only the request body and **MUST NOT**
  derive authorization decisions from transport-level identity.

### 14.5 Client helpers

The library provides two typed clients mirroring the two handlers, each pointed
at wherever its handler is mounted:

```go
type Client       struct{ /* ... */ } // data plane
type ConfigClient struct{ /* ... */ } // config plane
func NewClient(baseURL string, httpClient *http.Client) *Client
func NewConfigClient(baseURL string, httpClient *http.Client) *ConfigClient
```

`Client` has `Check`/`Write`/`Read`/`Expand`/`ListObjects`/`ListUsers`;
`ConfigClient` has `WriteConfig`/`ReadConfig`/`ListConfigs`/`ReadConfigVersion`/
`ListConfigVersions`. Both decode the response envelope and return a
reconstructed `*Error` (so `errors.Is`/`errors.As` work against the §15
sentinels), giving callers a checked round-trip without hand-encoding gob/JSON.

---

## 15. Error model

The library **MUST** distinguish at least these error categories, each mapped
to a stable, documented `ErrorType` string (§14.3) and, in Go, to a sentinel or
typed error suitable for `errors.Is`/`errors.As`:

| Category | When | Fail mode |
|----------|------|-----------|
| `InvalidArgument` | Malformed object/relation/subject/email/config (§12). | Deny/reject. |
| `NamespaceUnregistered` | Object namespace has no effective config (§5.5). | Deny/reject. |
| `RelationUndeclared` | Relation not in effective config. | Deny/reject. |
| `PreconditionFailed` | Write precondition not met (§8.3). | Abort write. |
| `Conflict` | Transaction commit conflict after retries (§9.3). | Abort; caller may retry. |
| `DepthExceeded` | Evaluation exceeded max depth (§6.4). | Deny with error. |
| `Unavailable` | Underlying `kv` I/O failure. | Deny with error. |

Storage-mapped conditions from `kv` — `os.ErrNotExist`, `os.ErrInvalid`,
`os.ErrClosed` — **MUST** be translated into the appropriate category above and
**MUST NOT** leak as allow decisions.

---

## 16. Security considerations

1. **Fail closed.** Every error path denies (§6.5). This is a security
   property, not merely an implementation detail, and **MUST** be preserved.
2. **Identity is external.** The library never authenticates; a
   misconfiguration in the host's authn layer can expose the mutating surface.
   Hosts **MUST** protect `write` and `config/*` (§14.4).
3. **Config is privileged.** Whoever can write a `NamespaceConfig` can
   effectively rewrite all authorization semantics for a namespace. Hosts
   **MUST** treat `config/write` as a high-privilege administrative operation.
4. **Injection safety of keys.** The `/`,`#`,`:` and whitespace exclusions
   (§12) are what keep the flat key space unambiguous; they **MUST** be
   enforced on every write to prevent one tuple from impersonating another via
   crafted ids/emails.
5. **Enumeration cost.** `ListObjects`/`ListUsers`/`Expand` can be expensive
   and can reveal the shape of the permission graph. Hosts **SHOULD** gate and
   rate-limit them independently of `check`.
6. **Timestamps come from the host** (§11.3), so the library carries no
   ambient clock authority and remains deterministic under test.

---

## 17. Conformance

An implementation is conformant iff:

1. It stores and interprets tuples and configs exactly per §3, §5, §11.
2. `Check` obeys §6, including subject expansion (§6.3), termination (§6.4),
   fail-closed (§6.5), and snapshot consistency (§6.1/§9).
3. Group nesting (§4) and parent inheritance (§7) work as specified as
   consequences of §5/§6 with no special-case code paths that diverge from
   Check semantics.
4. Writes are atomic and validated (§8).
5. The HTTP surface matches §14 and the error model matches §15.
6. All persistence goes through the `kv.Database` interface (§11); the library
   introduces no other storage dependency.

---

## 18. Future work (non-normative)

- **Consistency tokens (zookies).** Return a version marker from `Write` and
  accept an "at least as fresh as" bound on `Check` for external consistency.
- **Caching.** A leopard-style flattened-userset index for hot groups, and a
  check-result cache keyed by a version marker.
- **Watch API.** A changefeed of tuple mutations, if/when the `kv` backend
  exposes ordered change streams.
- **Conditions/ABAC.** Optional attribute predicates on tuples or rewrites,
  including recurring time schedules and request-context caveats — the general
  case beyond the fixed validity intervals shipped in v1 (§6.6).
- **Version-pinned (selector-mode) evaluation.** Let a Check request pin an
  expected `Version` per namespace and evaluate against those exact configs. v1
  already retains the config history (§5.5, §11.2) this would build on; the added
  machinery is a per-request version *bundle* (transitive over reachable
  namespaces), bundle-consistency validation, and a "latest override" so security
  tightenings still apply fleet-wide immediately. An intermediate step is
  *assertion mode*: pin expected versions as a guard that fails closed on
  incompatibility while still evaluating the latest config.
- **Config-history retention/GC.** A policy to prune old config versions no
  longer needed for audit or pinning, safe against any versions still referenced.
- **Wildcards and public access.** Broader `user:*` / public-share semantics if
  demanded by callers.
