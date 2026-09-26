# Zanzibar — Implementation Plan

**Status:** Engineering plan (for review)
**Companion:** `SPEC.md` (normative). Section references like §6.6 point there.
**Last updated:** 2026-09-26

This document describes *how* to build the library specified in `SPEC.md`, in
what order, and how each step is verified. It is a plan, not a contract; the
SPEC governs behavior where the two disagree.

---

## 1. Guiding principles

1. **Bottom-up, always testable.** Each phase produces something exercisable
   against an in-memory `kv.Database` before the next phase starts. No phase
   depends on code that does not yet have tests.
2. **Check is the pivot.** `Expand`, `ListObjects`, and `ListUsers` all reuse the
   Check traversal. Build the pure foundation, reach a minimal end-to-end Check
   as early as possible, then thicken.
3. **Fail-closed everywhere.** Every error path denies (§6.5). This is asserted
   by tests, not assumed.
4. **Snapshot discipline.** Every read operation opens exactly one `kv.Snapshot`
   and reads *everything* — tuples and effective config — through it (§6.1, §9).
   Every write runs in one `kv.Transaction`.
5. **Backend-agnostic.** All persistence goes through the `kv` interface (§11).
   Tests run first on `kvmemdb`, then the same suite runs against a real backend
   to prove portability.
6. **No ambient clock.** All timestamps — tuple creation, validity-interval
   bounds, and the Check `AsOf` — are caller-supplied (§6.6, §11.3). Nothing in
   the core calls the wall clock, which keeps evaluation deterministic and
   tests hermetic.

---

## 2. Prerequisites and tooling

- **Go toolchain:** go **1.26+** is required — `httphelp` declares `go 1.26.0`.
  (Note: a sandbox pinned to an older toolchain cannot build the HTTP layer; the
  pure/`kv`-only layers build on 1.23+.)
- **Module deps:**
  - `github.com/visvasity/kv` + `kv/kvutil` — persistence and gob helpers.
  - `github.com/visvasity/httphelp` — HTTP mount + RPC helpers (Phase 8).
  - `github.com/visvasity/kvmemdb` — **test-only** in-memory backend.
- **`go.sum`:** not yet generated (the pins in `go.mod` were added offline). Run
  `go mod tidy` in a go-1.26 environment before the first build.
- **Formatting/vetting:** `gofmt`, `go vet`, and `go test -race` on every phase.

---

## 3. Package and file layout

Single package `zanzibar`. The public surface already exists as stubs; the
implementation adds unexported files beside them.

| File | State | Contents |
|------|-------|----------|
| `doc.go` | exists | package overview |
| `config.go` | exists | `NamespaceConfig`, `Rewrite` and node types |
| `tuple.go` | exists | `Tuple`, `Mutation`, ops, preconditions, interval fields |
| `api.go` | exists | request/response types, `UsersetNode` |
| `service.go` | exists (stubs) | `Service`, `New`, `Option`s, method signatures |
| `http.go` | exists (stubs) | `Handler`/`ConfigHandler`, `Client`/`ConfigClient`, path consts |
| `errors.go` | exists (stubs) | `ErrorCode`, sentinels, `*Error` |
| `subject.go` | **new** | subject parsing + object/relation/email validation (§12) |
| `keys.go` | **new** | key builders + prefix ranges for config head/history/forward/reverse (§11), incl. order-preserving version encoding |
| `codec.go` | **new** | `tupleMeta` gob encode/decode via `kvutil` |
| `configstore.go` | **new** | effective-config resolution, seed, read/write/list (§5.5) |
| `tuplestore.go` | **new** | grant/revoke, dual-index maintenance, Read (§8, §10.2) |
| `eval.go` | **new** | Check: rewrite evaluation, cycle/depth, interval filter (§6) |
| `expand.go` | **new** | Expand tree builder (§10.1) |
| `list.go` | **new** | ListObjects / ListUsers (§10.3, §10.4) |
| `*_test.go` | **new** | per-file unit tests + integration/conformance suites |

---

## 4. Component dependency graph

```
subject.go ─┐
keys.go   ──┼─▶ configstore.go ─┐
codec.go  ──┘                   ├─▶ eval.go ─▶ expand.go
             tuplestore.go ─────┘        └───▶ list.go
                                                 │
service.go (wires all) ─────────────────────────┤
                                                 ▼
                                    http.go (handlers + client)
```

Build left-to-right. `eval.go` needs config + tuple reads; everything below it
reuses `eval.go`.

---

## 5. Phased plan

| # | Phase | Complexity | SPEC refs | Depends on |
|---|-------|-----------|-----------|-----------|
| 0 | Scaffolding + harness | S | §13, §15 | — |
| 1 | Keys, codecs, validation | M | §11, §12 | 0 |
| 2 | Config layer | M | §5, §5.5, §12.5 | 1 |
| 3 | Write path (+ intervals) | M | §8, §6.6 | 2 |
| — | **Milestone: vertical slice** | — | — | 3 + 5.1 |
| 4 | Read (stored tuples) | S | §10.2, §11.4 | 3 |
| 5 | Check engine (+ intervals) | L | §6, §6.6 | 2, 3 |
| 6 | Expand | M | §10.1 | 5 |
| 7 | ListObjects / ListUsers | L | §10.3, §10.4 | 5 |
| 8 | HTTP + client | M | §14 | 5 (min), all |
| 9 | Conformance + hardening | M | §17 | all |

### Phase 0 — Scaffolding + test harness

**Goal:** the package builds and a `Service` can be constructed over a test db.

**Work items**
- Implement the private `options` struct + option application in `New`.
- Implement `errors.go`: `Error.Error()`, `Error.Unwrap()` (maps `Code` →
  sentinel), and an internal `fromKV(err)` translator so `os.ErrNotExist` /
  `os.ErrInvalid` / `os.ErrClosed` become the right `ErrorCode` and never leak as
  allow decisions (§15).
- `New` stores `db` + resolved options on `Service` (no seeding yet).
- Test helper `newTestService(t, opts...)` backed by `kvmemdb`.

**Tests:** `New` returns a usable `*Service`; `errors.Is`/`errors.As` behave.
**Exit criteria:** `go build ./...` and `go test ./...` green; `go vet` clean.

### Phase 1 — Keys, codecs, validation (pure, no I/O)

**Goal:** a fully unit-tested pure core with zero kv dependency.

**Work items**
- `subject.go`: parse a subject string into one of {user, userset, object}
  (§3.2); validate `namespace`/`relation`/`id`/`email` and enforce the
  `/ # :`/whitespace exclusions (§12) that keep keys unambiguous.
- `keys.go`: builders for `c/`, `t/`, `s/` keys (§11.1) and the three prefix
  ranges (§11.4) via `kvutil.PrefixRange`.
- `codec.go`: `tupleMeta{CreatedAtUnixNano, NotBeforeUnixNano, NotAfterUnixNano}`
  encode/decode via `kvutil.SetGob`/`GetGob`.

**Tests:** table-driven parse/validate cases (valid + every rejection reason);
key round-trip and prefix-containment properties; codec round-trip.
**Exit criteria:** ≥ the parsing/validation branches covered; no I/O in this file
set.

### Phase 2 — Config layer

**Goal:** DB-only schema management with snapshot-consistent reads and
compare-and-set writes. The store is the single source of truth; there is no
`WithNamespace` and no in-process default (§5.5).

**Work items**
- `NamespaceConfig` validation (§12.5): exactly-one-node per `Rewrite` (§5.3);
  `computedUserset`/`tupleToUserset` targets declared; static `computedUserset`
  cycle detection.
- `configstore.go`: `effectiveConfig(ctx, snap, namespace)` = one `GetGob` of the
  **head** record via the operation snapshot (§6.1); `WriteConfig` with
  **compare-and-set on `Version`** (0 to create; else must equal stored; assign
  `stored+1`; mismatch → `CodeConflict`, no change) that, in one tx, updates the
  head **and appends an immutable history record** `h/<ns>/<enc(v)>` (§11.2),
  returning the stored config with its new `Version`; `ReadConfig`,
  `ListConfigs` (scan `c/`), `ReadConfigVersion`, `ListConfigVersions`
  (scan `h/<ns>/`), each carrying `Version`.
- Config cache keyed by `(namespace, Version)`, invalidated on version change.

**Tests (`kvmemdb`):** create-then-conflicting-create; update with stale version
→ conflict; update with correct version → `Version+1`; head/history written
atomically and never diverge; history records immutable across later writes;
`ReadConfigVersion`/`ListConfigVersions` round-trip; rollback-forward produces a
new version equal in content to an old one; validation and static-cycle
rejections; cache invalidation on version bump.
**Exit criteria:** effective config resolves deterministically under snapshot;
concurrent writers cannot clobber (CAS holds); full version history is retained.

### Phase 3 — Write path (with validity intervals)

**Goal:** tuples persist atomically and correctly, including time-boxing.

**Work items**
- Validate each mutation against the effective config (§8.4): namespace
  registered, relation declared, subject well-formed and its namespace
  registered.
- **Interval validation (§8.4/§6.6):** when both bounds are non-zero, require
  `NotBefore < NotAfter`; reject empty/inverted intervals.
- `grant`/`revoke` maintaining forward (`t/`) **and** reverse (`s/`) records in
  the *same* transaction (§11.3). Grant is idempotent but **sets/replaces the
  interval** and preserves the creation stamp (§8.3).
- Preconditions (`must_exist`/`must_not_exist`); atomic multi-mutation batch
  (§8.2); commit-conflict retry bounded by `WithCommitRetries` (§9.3).

**Tests:** all-or-nothing on an invalid mutation; idempotent grant; interval
replace-on-regrant; precondition failures; forward/reverse consistency invariant;
inverted-interval rejection.
**Exit criteria:** the dual-index invariant holds after arbitrary batches.

> **Milestone — first vertical slice.** With Phase 3 plus the `_this`-only slice
> of Phase 5, grant a tuple and Check it end-to-end. Add that integration test
> now; it de-risks the foundation before the rich engine work.

### Phase 4 — Read (query stored tuples)

**Goal:** raw-tuple introspection; also a debugging aid for later phases.

**Work items**
- Map `ReadRequest` filters to the right scan: by object, object+relation, or
  subject (reverse index), with namespace filtering (§10.2, §11.4).
- Deterministic key order; opaque continuation token = last-seen key.

`Read` returns `TupleRecord`s carrying the tuple plus its stored metadata
(`CreatedAt`, `NotBefore`, `NotAfter`); it reports stored facts and does **not**
filter by `AsOf` (§10.2), so time-boxed grants are visible with their bounds.

**Tests:** each filter shape; multi-page pagination; stable ordering; interval
bounds round-trip through Read.
**Exit criteria:** results match a brute-force in-test enumeration.

### Phase 5 — Check engine (core)

**Goal:** full Check semantics, built and tested in increasing complexity.

**Sub-steps (each independently tested):**
1. `_this` direct match (→ enables the vertical-slice milestone).
2. Userset-subject expansion (groups, nested groups) with the **visited-set
   cycle guard** and **depth limit** (§6.3, §6.4).
3. `computed_userset` (relation implication).
4. `tuple_to_userset` (parent inheritance, transitive) (§7).
5. `union` / `intersection` / `exclusion` (§5.2).
6. **Validity-interval filter (§6.6):** at `_this` leaves, a tuple counts only if
   active at the request `AsOf`; `AsOf` is fixed for the operation and threaded
   unchanged through recursion; absent `AsOf` (≤ 0) treats interval-bearing
   tuples as inactive (fail-closed).

Throughout: one snapshot per request; config read from that snapshot; every
error path denies.

**Tests:** direct; group + nested group + cycle termination; implication;
single-level and transitive inheritance; intersection/exclusion; depth-exceeded
→ error; unregistered/undeclared → deny; **time cases** (before window, in
window, after window, unbounded sides, missing `AsOf` with a timed tuple).
**Exit criteria:** the `payments` scenario (self + staff, with a time-boxed
support grant) passes end-to-end.

### Phase 6 — Expand

**Goal:** userset tree for debugging.

**Work items**
- Reuse the Phase 5 traversal to build a `UsersetNode` tree instead of a bool;
  mark `Truncated` at cycle/depth cutoffs; never expand `user:*`.
- Honor `AsOf`: inactive leaves excluded (§6.6); optionally annotate intervals.

**Tests:** tree shape per rewrite kind; truncation markers; time-filtered leaves.
**Exit criteria:** an expanded tree, when flattened, matches Check for the same
inputs.

### Phase 7 — ListObjects / ListUsers (most expensive)

**Goal:** enumeration APIs, sound and complete relative to Check.

**Work items**
- `ListUsers`: flatten one `object#relation`, collect + dedup user leaves,
  paginate, apply a configurable cap (§10.4).
- `ListObjects`: gather candidates from the reverse index — direct tuples,
  groups the subject transitively belongs to, parent-chain expansions implied by
  rewrites — then **confirm each candidate with a full Check** (§10.3). Document
  the over-read cost.
- Both honor `AsOf`.

**Tests:** soundness + completeness against a brute-force Check over a small
fixture; pagination; cap behavior.
**Exit criteria:** for a random fixture, `ListObjects` output equals
`{o : Check(o) == true}` at the snapshot.

### Phase 8 — HTTP + client

**Goal:** the surface applications mount.

**Work items**
- `Handler()` / `ConfigHandler()` returning `http.Handler` (a ServeMux per
  plane), each op wrapped via `httphelp.PostHandler2` in a response envelope so
  typed errors survive the wire (§14.1/§14.3).
- `Client` (data) and `ConfigClient` (config) via `httphelp.CallPostHandler`.

**Tests:** bring up `httphelp.Server` on `127.0.0.1:0` (and/or a unix socket);
round-trip every endpoint with `Client`; assert the httphelp error surface
(handler error → `{Error, ErrorType}` body at HTTP 200; `Allowed:false` is not an
error, §14.3).
**Exit criteria:** every method reachable over HTTP with matching semantics.

### Phase 9 — Conformance + hardening

**Goal:** confidence and portability.

**Work items**
- §17 conformance suite: canonical `doc`/`folder`/`group`/`payments` namespaces
  with golden Check outcomes.
- Property test: random relationship graphs vs. a simple reference evaluator, for
  termination and consistency.
- Concurrency test: parallel writes + checks exercising commit-retry and
  snapshot isolation under `-race`.
- `example_payments_test.go`; short README pointing to SPEC + this doc.
- Re-run the full suite against a real backend (`kvpg`/`kvbadger`).

**Exit criteria:** conformance checklist (§8 below) all green on ≥2 backends.

---

## 6. Cross-cutting concerns

- **Snapshot discipline:** a single helper opens/`Discard`s the snapshot per read
  op and passes it (plus `AsOf`) down the traversal; config reads use the same
  snapshot. Audited by a test that counts snapshot open/close.
- **Index consistency:** forward + reverse writes/deletes always in one tx; a
  reusable invariant check (`every t/ has a matching s/ and vice versa`) runs in
  write tests.
- **Termination:** visited set keyed by `(object, relation, subject)`; depth
  counter with configurable max. Property-tested against adversarial cycles.
- **Determinism:** no `time.Now()`, no `math/rand`, no map-iteration-order
  dependence in results (sort before returning / paginating).
- **Pagination tokens:** opaque, encode the last-seen key; stable under
  concurrent writes (a token never skips or duplicates already-emitted keys
  within a snapshot).
- **Error mapping:** one `fromKV` + one `newError(code, ...)` path so every
  category in §15 has a single source.

---

## 7. Risk register

| Risk | Phase | Mitigation |
|------|-------|-----------|
| Cycle/depth logic subtly wrong → hang or false allow | 5 | Build guard in 5.2 first; property-test adversarial graphs |
| Forward/reverse indexes diverge | 3 | Single-tx writes; invariant assertion in tests |
| Config or interval read outside the snapshot → fleet inconsistency | 2,5 | One snapshot threaded everywhere; snapshot-count test |
| `ListObjects` candidate gathering misses objects | 7 | Brute-force completeness cross-check (non-negotiable) |
| Retried Write batch not idempotent | 3 | Reapply-safe grant/revoke; retry test |
| Missing `AsOf` silently allows timed grant | 3,5 | Fail-closed rule + explicit "missing AsOf" test |
| Old binary misparses a newer stored config | 2,8 | §5.3 "exactly one node, else reject"; see Open Decision #4 (FormatVersion) |

---

## 8. Definition of done (maps to §17 conformance)

- [ ] Tuples/configs stored and interpreted per §3, §5, §11.
- [ ] Check obeys §6: expansion (§6.3), termination (§6.4), fail-closed (§6.5),
      snapshot consistency (§6.1/§9), interval filtering (§6.6).
- [ ] Groups (§4) and inheritance (§7) work with no special-case paths diverging
      from Check.
- [ ] Writes atomic and validated (§8), including interval well-formedness.
- [ ] HTTP surface matches §14; error model matches §15.
- [ ] All persistence via `kv` (§11); suite green on ≥2 backends.

---

## 9. Open decisions to resolve

These affect the plan and should be settled before the phase that depends on
them.

1. **Config source-of-truth — RESOLVED.** DB is the single source of truth: no
   `WithNamespace`, no in-process default; namespaces are created/evolved via
   `WriteConfig` with compare-and-set on `Version` (§5.5). Bootstrap is a
   deliberate migration step, and schema changes are additive.
2. **Skew detection.** Optionally expose a hash or the `Version` set of the
   effective schema (health endpoint or log) so a mixed-version fleet is
   observable. Cheap; can land in Phase 8.
3. **Interval introspection — RESOLVED.** `Read` returns
   `TupleRecord{Tuple + CreatedAt/NotBefore/NotAfter}` (§10.2). Interval
   annotations on `Expand`'s `UsersetNode` leaves remain optional/deferred.
4. **Per-namespace config version pinning in requests — OPEN (under
   discussion).** Whether Check should carry an expected `Version` per namespace
   as (a) an *assertion* (evaluate latest, fail on incompatibility — cheap,
   needs no history) or (b) a *selector* (evaluate that stored version — needs
   multi-version config storage + GC + a bundle/lockfile + a security-tightening
   override). Leaning assertion-mode-if-any for v1; selector mode deferred.
5. **Config `FormatVersion` + `DeleteConfig`.** A `NamespaceConfig.FormatVersion`
   lets an older binary reject a too-new config outright (belt-and-suspenders
   over §5.3). `DeleteConfig` is intentionally omitted for now (deleting a live
   config fails-closed fleet-wide).
