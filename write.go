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
	relation      string
	subjStr       string // canonical subject string (email case-folded if enabled)
	subjNamespace string // "" for a user subject; namespace for userset/object subjects
	precondition  Precondition
	meta          tupleMeta
}

// Write applies an ordered batch of grants and revokes atomically (§8). All
// mutations are validated statically first; config-dependent checks and the
// writes then run inside a single transaction.
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
			changed, err := s.applyMutation(ctx, tx, &parsed[i], configs)
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
	if m.Op != OpGrant && m.Op != OpRevoke {
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
	if err := validateRelation(m.Tuple.Relation); err != nil {
		return parsedMutation{}, err
	}
	sub, err := parseSubject(m.Tuple.Subject)
	if err != nil {
		return parsedMutation{}, err
	}
	if sub.wildcard {
		// The "user:*" wildcard is a per-relation config feature that is off by
		// default and not yet configurable, so it cannot be granted (§3.2).
		return parsedMutation{}, &Error{Code: CodeInvalidArgument, Message: "wildcard subject 'user:*' is not enabled"}
	}
	if err := validInterval(m.NotBeforeUnixNano, m.NotAfterUnixNano); err != nil {
		return parsedMutation{}, err
	}

	subjNamespace := ""
	if sub.kind == kindUserset || sub.kind == kindObject {
		subjNamespace = sub.namespace
	}

	return parsedMutation{
		op:            m.Op,
		object:        ns + ":" + id,
		namespace:     ns,
		relation:      m.Tuple.Relation,
		subjStr:       canonicalSubject(sub, s.opts.emailCaseFold),
		subjNamespace: subjNamespace,
		precondition:  m.Precondition,
		meta: tupleMeta{
			CreatedAtUnixNano: m.CreatedAtUnixNano,
			NotBeforeUnixNano: m.NotBeforeUnixNano,
			NotAfterUnixNano:  m.NotAfterUnixNano,
		},
	}, nil
}

// applyMutation runs config-dependent validation and applies one mutation within
// the transaction. It reports whether the mutation changed stored state (§8.3).
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

	// Preconditions (§8.3) are evaluated against current state.
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
