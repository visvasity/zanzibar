// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"

	"github.com/visvasity/kv"
)

// parsedMutation is a statically-validated mutation ready to apply.
type parsedMutation struct {
	op            MutationOp
	object        string // canonical "namespace:id"
	namespace     string // object's namespace
	relation      string // grant/revoke
	subjStr       string // canonical subject string (email case-folded if enabled)
	subjNamespace string // "" for a user subject; namespace for userset/object subjects
	precondition  Precondition
	meta          tupleMeta // grant only
}

// Write applies an ordered batch of grants, revokes, and deletes atomically
// (§8). All mutations are validated statically first; config-dependent checks
// and the writes then run inside a single transaction.
func (s *Service) Write(ctx context.Context, req *WriteRequest) (*WriteResponse, error) {
	if req == nil {
		return nil, &Error{Code: CodeInvalidArgument, Message: "nil write request"}
	}

	parsed := make([]parsedMutation, 0, len(req.Mutations))
	for i := range req.Mutations {
		pm, err := s.parseMutation(&req.Mutations[i])
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, pm)
	}

	var applied int
	err := s.inTx(ctx, func(tx kv.Transaction) error {
		applied = 0                                  // reset on each attempt/retry
		configs := make(map[string]*NamespaceConfig) // per-tx cache of effective configs

		for i := range parsed {
			pm := &parsed[i]
			if pm.op == OpDelete {
				n, err := s.applyDelete(ctx, tx, pm)
				if err != nil {
					return err
				}
				applied += n
				continue
			}
			changed, err := s.applyMutation(ctx, tx, pm, configs)
			if err != nil {
				return err
			}
			if changed {
				applied++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &WriteResponse{Applied: applied}, nil
}

// parseMutation performs static (config-independent) validation of one mutation
// and returns its canonical form (§8.4, §12).
func (s *Service) parseMutation(m *Mutation) (parsedMutation, error) {
	switch m.Op {
	case OpGrant, OpRevoke, OpDelete:
	default:
		return parsedMutation{}, &Error{Code: CodeInvalidArgument, Message: "invalid mutation op: " + string(m.Op)}
	}
	switch m.Precondition {
	case PreconditionNone, PreconditionMustExist, PreconditionMustNotExist:
	default:
		return parsedMutation{}, &Error{Code: CodeInvalidArgument, Message: "invalid precondition: " + string(m.Precondition)}
	}

	ns, id, err := parseObject(m.Tuple.Object)
	if err != nil {
		return parsedMutation{}, err
	}
	pm := parsedMutation{op: m.Op, object: ns + ":" + id, namespace: ns, precondition: m.Precondition}

	if m.Op == OpDelete {
		if m.Tuple.Relation != "" || m.Tuple.Subject != "" {
			return parsedMutation{}, &Error{Code: CodeInvalidArgument, Message: "delete takes only an object (relation and subject must be empty)"}
		}
		if m.Precondition != PreconditionNone {
			return parsedMutation{}, &Error{Code: CodeInvalidArgument, Message: "a precondition is not valid for delete"}
		}
		// CreatedAtUnixNano, if supplied, is recorded as the deletion time in the
		// tombstone (§8.5); the library keeps no clock of its own.
		pm.meta.CreatedAtUnixNano = m.CreatedAtUnixNano
		return pm, nil
	}

	// grant / revoke: an exact tuple.
	if err := validateRelation(m.Tuple.Relation); err != nil {
		return parsedMutation{}, err
	}
	pm.relation = m.Tuple.Relation
	sub, err := parseSubject(m.Tuple.Subject)
	if err != nil {
		return parsedMutation{}, err
	}
	if sub.wildcard {
		return parsedMutation{}, &Error{Code: CodeInvalidArgument, Message: "wildcard subject 'user:*' is not enabled"}
	}
	pm.subjStr = canonicalSubject(sub, s.opts.emailCaseFold)
	if sub.kind == kindUserset || sub.kind == kindObject {
		pm.subjNamespace = sub.namespace
	}
	if m.Op == OpGrant {
		if err := validInterval(m.NotBeforeUnixNano, m.NotAfterUnixNano); err != nil {
			return parsedMutation{}, err
		}
		pm.meta = tupleMeta{
			CreatedAtUnixNano: m.CreatedAtUnixNano,
			NotBeforeUnixNano: m.NotBeforeUnixNano,
			NotAfterUnixNano:  m.NotAfterUnixNano,
		}
	}
	return pm, nil
}

// applyMutation runs config-dependent validation and applies one grant or revoke
// within the transaction. It reports whether stored state changed (§8.3).
func (s *Service) applyMutation(ctx context.Context, tx kv.Transaction, pm *parsedMutation, configs map[string]*NamespaceConfig) (bool, error) {
	// The object's namespace must be registered and declare the relation.
	cfg, err := s.cachedConfig(ctx, tx, pm.namespace, configs)
	if err != nil {
		return false, err
	}
	if cfg == nil {
		return false, &Error{Code: CodeNamespaceUnregistered, Message: "namespace not registered: " + pm.namespace, Object: pm.object, Relation: pm.relation}
	}
	if _, ok := cfg.Relations[pm.relation]; !ok {
		return false, &Error{Code: CodeRelationUndeclared, Message: "relation not declared: " + pm.relation, Object: pm.object, Relation: pm.relation}
	}
	// A userset/object subject's namespace must be registered.
	if pm.subjNamespace != "" {
		subCfg, err := s.cachedConfig(ctx, tx, pm.subjNamespace, configs)
		if err != nil {
			return false, err
		}
		if subCfg == nil {
			return false, &Error{Code: CodeNamespaceUnregistered, Message: "subject namespace not registered: " + pm.subjNamespace, Object: pm.object, Relation: pm.relation, Subject: pm.subjStr}
		}
	}

	fwd := forwardKey(s.opts.keyPrefix, pm.object, pm.relation, pm.subjStr)
	rev := reverseKey(s.opts.keyPrefix, pm.subjStr, pm.object, pm.relation)

	var existing tupleMeta
	found, err := getGob(ctx, tx, fwd, &existing)
	if err != nil {
		return false, err
	}

	switch pm.precondition {
	case PreconditionMustExist:
		if !found {
			return false, &Error{Code: CodePreconditionFailed, Message: "tuple must exist", Object: pm.object, Relation: pm.relation, Subject: pm.subjStr}
		}
	case PreconditionMustNotExist:
		if found {
			return false, &Error{Code: CodePreconditionFailed, Message: "tuple must not exist", Object: pm.object, Relation: pm.relation, Subject: pm.subjStr}
		}
	}

	switch pm.op {
	case OpGrant:
		meta := pm.meta
		if found {
			// A redundant grant must not reset the creation timestamp; it only
			// (re)sets the validity interval (§8.3).
			meta.CreatedAtUnixNano = existing.CreatedAtUnixNano
			if existing.NotBeforeUnixNano == meta.NotBeforeUnixNano && existing.NotAfterUnixNano == meta.NotAfterUnixNano {
				return false, nil // true no-op
			}
		}
		if err := setGob(ctx, tx, fwd, &meta); err != nil {
			return false, err
		}
		if err := setGob(ctx, tx, rev, &meta); err != nil {
			return false, err
		}
		// A grant on an object revives it: clear any deletion tombstone so the
		// GC does not sweep references to a re-created object (§8.5). Reading and
		// writing the tombstone key serializes this against a concurrent collect.
		if s.opts.deletionLog {
			if err := del(ctx, tx, gcKey(s.opts.keyPrefix, pm.object)); err != nil {
				return false, err
			}
		}
		return true, nil

	case OpRevoke:
		if !found {
			return false, nil // no-op
		}
		if err := del(ctx, tx, fwd); err != nil {
			return false, err
		}
		if err := del(ctx, tx, rev); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, &Error{Code: CodeInvalidArgument, Message: "invalid mutation op"}
}

// applyDelete removes every tuple stored on pm.object (§8.1). It operates on
// stored keys and requires no config, so it cleans up even after a namespace's
// config has changed. It returns the number of tuples deleted.
func (s *Service) applyDelete(ctx context.Context, tx kv.Transaction, pm *parsedMutation) (int, error) {
	prefix := s.opts.keyPrefix
	beg, end := forwardObjectRange(prefix, pm.object)

	// Collect matching triples first (do not mutate while iterating).
	type triple struct{ object, relation, subject string }
	var matches []triple
	var ierr error
	for k := range tx.Ascend(ctx, beg, end, &ierr) {
		object, relation, subject, ok := decodeTupleKey(k, prefix, false)
		if !ok {
			continue
		}
		matches = append(matches, triple{object, relation, subject})
	}
	if ierr != nil {
		return 0, fromKV(ierr)
	}
	for _, m := range matches {
		if err := del(ctx, tx, forwardKey(prefix, m.object, m.relation, m.subject)); err != nil {
			return 0, err
		}
		if err := del(ctx, tx, reverseKey(prefix, m.subject, m.object, m.relation)); err != nil {
			return 0, err
		}
	}
	// Record a tombstone so the GC can later sweep references to this deleted
	// object (§8.5).
	if s.opts.deletionLog {
		if err := setGob(ctx, tx, gcKey(prefix, pm.object), &gcRecord{DeletedAtUnixNano: pm.meta.CreatedAtUnixNano}); err != nil {
			return 0, err
		}
	}
	return len(matches), nil
}

// cachedConfig reads a namespace's effective config through tx once per Write,
// returning nil when the namespace is unregistered.
func (s *Service) cachedConfig(ctx context.Context, tx kv.Transaction, namespace string, cache map[string]*NamespaceConfig) (*NamespaceConfig, error) {
	if cfg, ok := cache[namespace]; ok {
		return cfg, nil
	}
	cfg, found, err := s.effectiveConfig(ctx, tx, namespace)
	if err != nil {
		return nil, err
	}
	if !found {
		cfg = nil
	}
	cache[namespace] = cfg
	return cfg, nil
}

// canonicalSubject returns the stored form of a subject, applying email
// case-folding when enabled (§12.4) so grants and checks agree on the key.
func canonicalSubject(sub subject, caseFold bool) string {
	if sub.kind == kindUser && !sub.wildcard {
		return userPrefix + foldEmail(sub.email, caseFold)
	}
	return sub.String()
}
