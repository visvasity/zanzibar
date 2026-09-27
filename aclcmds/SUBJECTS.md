# aclcmds — Subject mapping (email ⇆ identity)

**Status:** Design note
**Package:** `github.com/visvasity/zanzibar/aclcmds`
**Last updated:** 2026-09-26

Zanzibar stores and evaluates opaque `user:<value>` subjects. Applications
increasingly want those values to be a **stable identity** (e.g. a synthetic
identity from `github.com/visvasity/userdb`, `acct_7f3@id.example.invalid`)
rather than a human email that can change over time or differ across
providers. But operators want to keep typing — and reading — **human emails**
on the command line.

`aclcmds` bridges the two with three optional, application-supplied callbacks.
The package itself stays identity-agnostic: it knows nothing about accounts,
`userdb`, or how emails map to identities. Zanzibar never depends on `userdb`;
the application wires the mapping in.

## The three callbacks

```go
// Forward: human email -> identity value. Does I/O.
type SubjectResolver func(ctx context.Context, email string) (id string, err error)

// Classifier: is this value ALREADY an identity? Pure, no I/O.
type IsSynthetic func(id string) bool

// Reverse: identity value -> human email, for display. Does I/O.
type SubjectFormatter func(ctx context.Context, id string) (display string, err error)
```

They are grouped into a public `SubjectOptions` struct and applied to each
command with the `SetSubjectOptions` method every subject command exposes:

```go
opts := aclcmds.SubjectOptions{
    IsSynthetic: func(id string) bool {
        return strings.HasSuffix(id, "@id.hostcheck.invalid")
    },
    Resolve: func(ctx context.Context, email string) (string, error) {
        // app: email -> account -> synthetic (e.g. via userdb).
        // On a logical miss: log a warning and return email unchanged.
        return app.identityForEmail(ctx, email)
    },
    Format: func(ctx context.Context, id string) (string, error) {
        // app: synthetic -> primary email. On a miss: return id unchanged.
        return app.emailForIdentity(ctx, id)
    },
}

// Per command:
g := new(aclcmds.Grant)
g.SetSubjectOptions(opts)

// Or configure a whole set the application assembled, via the
// SubjectOptionsSetter interface:
for _, c := range cmds {
    if s, ok := c.(aclcmds.SubjectOptionsSetter); ok {
        s.SetSubjectOptions(opts)
    }
}
```

Commands that take no subject (`delete-object`, `config`) do not implement
`SubjectOptionsSetter` and are simply skipped by such a loop.

All three operate on the **value inside `user:`** (aclcmds owns the `user:`
scheme). Non-user subjects (`group:…#…`, objects) and the wildcard `user:*` are
never touched.

## Behavior

Everything keys off the classifier, so there is no special CLI syntax — an
operator types the same `user:alice@example.com` they always did.

**Input** (`grant`, `revoke`, `check`, `list-objects`, and the `-subject` filter
of `list-tuples`), for a `user:<v>` subject:

| `IsSynthetic(v)` | Action |
|---|---|
| true  | use `user:<v>` unchanged (already an identity) |
| false | `v = SubjectResolver(v)`, use `user:<v>` |

**Output** (`list-users`, `expand`, and the subject column of `list-tuples`),
for each `user:<v>` in the result:

| `IsSynthetic(v)` | Action |
|---|---|
| true  | print `SubjectFormatter(v)` |
| false | print `user:<v>` unchanged |

## The not-found contract

Callbacks distinguish two failure kinds:

- **Logical miss** — no identity for the email, or no email for the identity.
  The callback SHOULD **log a warning and return its input unchanged with a nil
  error**. `aclcmds` uses the value verbatim and never special-cases it.
- **Infrastructure error** — backend unreachable, lookup RPC failed. Return a
  **non-nil error**; `aclcmds` aborts the command.

The passthrough behavior is deliberate and useful during migration:

- An **unknown human email** on input becomes `user:<email>`, which still
  matches any **legacy email-based grant** — no lockout while a directory is
  being backfilled.
- An **unmapped identity** on output prints as the raw identity rather than a
  blank.

## Configuration rules

- `SubjectOptions.IsSynthetic` is the **switch**: mapping happens only when it
  is set. If it is nil, `Resolve` and `Format` are ignored and every subject
  passes through unchanged (so an incomplete `SubjectOptions` degrades to the
  no-mapping default rather than misbehaving).
- With **no options applied** (or a zero `SubjectOptions`), subjects are stored
  and printed verbatim — the original behavior, unchanged and fully backward
  compatible.
- The classifier is reliable precisely because synthetic identities live in a
  non-deliverable domain the app controls (an RFC 6761 `.invalid` domain, or an
  owned null-MX domain): no real human email can share that suffix.

## Why a classifier instead of a prefix scheme

A synthetic identity is itself email-shaped, so `aclcmds` cannot tell "resolve
this" from "already resolved" by inspection. An explicit `email:` prefix would
work but burdens operators with new syntax and breaks copy-paste of identities
from output back into a command. Delegating the one bit of judgment —
*is this already an identity?* — to the application (which owns the identity
domain) keeps the CLI identical to today and makes the mapping **idempotent**:
resolved output can be fed straight back in.
