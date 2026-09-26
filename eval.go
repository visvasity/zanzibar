// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"io"

	"github.com/visvasity/kv"
)

// Check reports whether the request's subject is a member of the userset
// Object#Relation (§6). It evaluates against a single snapshot, is fail-closed
// (any error or exceeded depth denies rather than allows), and terminates on
// cycles.
func (s *Service) Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error) {
	if req == nil {
		return nil, &Error{Code: CodeInvalidArgument, Message: "nil check request"}
	}
	ns, id, err := parseObject(req.Object)
	if err != nil {
		return nil, err
	}
	if err := validateRelation(req.Relation); err != nil {
		return nil, err
	}
	sub, err := parseSubject(req.Subject)
	if err != nil {
		return nil, err
	}
	if sub.wildcard {
		return nil, &Error{Code: CodeInvalidArgument, Message: "wildcard 'user:*' is not a valid check subject"}
	}

	snap, err := s.db.NewSnapshot(ctx)
	if err != nil {
		return nil, fromKV(err)
	}
	defer snap.Discard(ctx)

	e := &evaluator{
		s:       s,
		ctx:     ctx,
		r:       snap,
		asOf:    req.AsOfUnixNano,
		target:  canonicalSubject(sub, s.opts.emailCaseFold),
		configs: make(map[string]*NamespaceConfig),
		visited: make(map[string]bool),
	}
	allowed, err := e.check(ns+":"+id, req.Relation)
	if err != nil {
		return nil, err
	}
	return &CheckResponse{Allowed: allowed}, nil
}

// evaluator carries the per-Check state: the shared snapshot, the query subject,
// the evaluation time, a per-call config cache, and the recursion guards. It is
// scoped to a single Check/Expand call and is not safe for concurrent use.
type evaluator struct {
	s       *Service
	ctx     context.Context
	r       kv.Reader
	asOf    int64
	target  string // canonical query subject
	configs map[string]*NamespaceConfig
	visited map[string]bool // (object#relation) nodes on the current path
	depth   int
}

// check evaluates whether the target subject is a member of object#relation.
// It enforces the cycle guard and depth limit (§6.4) and is fail-closed: an
// unregistered namespace or undeclared relation denies (§6.5).
func (e *evaluator) check(object, relation string) (bool, error) {
	node := object + "#" + relation
	if e.visited[node] {
		return false, nil // cycle: membership does not arise from a loop
	}
	e.depth++
	if e.depth > e.s.opts.maxDepth {
		e.depth--
		return false, &Error{Code: CodeDepthExceeded, Message: "evaluation depth exceeded", Object: object, Relation: relation}
	}
	e.visited[node] = true
	defer func() {
		delete(e.visited, node)
		e.depth--
	}()

	cfg, found, err := e.configFor(namespaceOf(object))
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil // unregistered namespace -> deny
	}
	rw, ok := cfg.Relations[relation]
	if !ok {
		return false, nil // undeclared relation -> deny
	}
	return e.evalRewrite(object, relation, rw)
}

// evalRewrite evaluates one rewrite node for object#relation (§6.2). union,
// intersection, and exclusion operate on the same node; computed_userset,
// tuple_to_userset, and userset subjects recurse into new nodes.
func (e *evaluator) evalRewrite(object, relation string, rw Rewrite) (bool, error) {
	switch {
	case isEmptyRewrite(rw) || rw.This != nil:
		return e.evalThis(object, relation)
	case rw.ComputedUserset != nil:
		return e.check(object, rw.ComputedUserset.Relation)
	case rw.TupleToUserset != nil:
		return e.evalTupleToUserset(object, rw.TupleToUserset.Tupleset, rw.TupleToUserset.ComputedUserset)
	case rw.Union != nil:
		for _, c := range rw.Union.Children {
			ok, err := e.evalRewrite(object, relation, c)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case rw.Intersection != nil:
		for _, c := range rw.Intersection.Children {
			ok, err := e.evalRewrite(object, relation, c)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	case rw.Exclusion != nil:
		base, err := e.evalRewrite(object, relation, *rw.Exclusion.Base)
		if err != nil || !base {
			return false, err
		}
		sub, err := e.evalRewrite(object, relation, *rw.Exclusion.Subtract)
		if err != nil {
			return false, err
		}
		return !sub, nil
	}
	return false, nil
}

// evalThis evaluates the _this node: the target matches a direct subject, or a
// stored userset subject whose userset contains the target (§6.3). Inactive
// tuples (outside their validity interval at asOf) are ignored (§6.6).
func (e *evaluator) evalThis(object, relation string) (bool, error) {
	var result bool
	err := e.eachSubject(object, relation, func(subj string, m tupleMeta) (bool, error) {
		if !m.activeAt(e.asOf) {
			return false, nil // inactive: skip
		}
		if subj == e.target {
			result = true
			return true, nil
		}
		sub, err := parseSubject(subj)
		if err != nil {
			return false, nil // skip a malformed stored subject (fail-closed)
		}
		if sub.kind == kindUserset {
			ok, err := e.check(sub.namespace+":"+sub.id, sub.relation)
			if err != nil {
				return false, err
			}
			if ok {
				result = true
				return true, nil
			}
		}
		return false, nil
	})
	return result, err
}

// evalTupleToUserset evaluates the tuple_to_userset node: for each active tuple
// object#tupleset@X where X is an object subject, it checks X#computedRel (§5.2,
// §7). Non-object subjects are ignored.
func (e *evaluator) evalTupleToUserset(object, tupleset, computedRel string) (bool, error) {
	var result bool
	err := e.eachSubject(object, tupleset, func(subj string, m tupleMeta) (bool, error) {
		if !m.activeAt(e.asOf) {
			return false, nil
		}
		sub, err := parseSubject(subj)
		if err != nil {
			return false, nil
		}
		if sub.kind == kindObject {
			ok, err := e.check(sub.namespace+":"+sub.id, computedRel)
			if err != nil {
				return false, err
			}
			if ok {
				result = true
				return true, nil
			}
		}
		return false, nil
	})
	return result, err
}

// eachSubject iterates the direct subjects stored for object#relation through
// the shared reader, invoking fn with each subject and its metadata. fn returns
// stop=true to end iteration early.
func (e *evaluator) eachSubject(object, relation string, fn func(subj string, m tupleMeta) (stop bool, err error)) error {
	beg, end := forwardRelationRange(e.s.opts.keyPrefix, object, relation)
	var ierr error
	for k, r := range e.r.Ascend(e.ctx, beg, end, &ierr) {
		_, _, subj, ok := decodeTupleKey(k, e.s.opts.keyPrefix, false)
		if !ok {
			continue
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return fromKV(err)
		}
		var m tupleMeta
		if err := gobDecode(data, &m); err != nil {
			return err
		}
		stop, err := fn(subj, m)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
	return fromKV(ierr)
}

// configFor returns the effective config for a namespace, cached per Check and
// read through the shared snapshot (§6.1).
func (e *evaluator) configFor(namespace string) (*NamespaceConfig, bool, error) {
	if cfg, ok := e.configs[namespace]; ok {
		return cfg, cfg != nil, nil
	}
	cfg, found, err := e.s.effectiveConfig(e.ctx, e.r, namespace)
	if err != nil {
		return nil, false, err
	}
	if !found {
		e.configs[namespace] = nil
		return nil, false, nil
	}
	e.configs[namespace] = cfg
	return cfg, true, nil
}
