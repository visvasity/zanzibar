// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"sort"
)

// Expand returns the userset tree for Object#Relation without flattening it to
// leaf users (§10.1). It evaluates against one snapshot, applies the cycle and
// depth guards (marking cut-off nodes Truncated rather than erroring), and
// excludes tuples inactive at the request AsOf (§6.6).
func (s *Service) Expand(ctx context.Context, req *ExpandRequest) (*ExpandResponse, error) {
	if req == nil {
		return nil, &Error{Code: CodeInvalidArgument, Message: "nil expand request"}
	}
	ns, id, err := parseObject(req.Object)
	if err != nil {
		return nil, err
	}
	if err := validateRelation(req.Relation); err != nil {
		return nil, err
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
		configs: make(map[string]*NamespaceConfig),
		visited: make(map[string]bool),
	}
	tree, err := e.expandRelation(ns+":"+id, req.Relation)
	if err != nil {
		return nil, err
	}
	return &ExpandResponse{Tree: tree}, nil
}

// expandRelation builds the tree node for object#relation, applying the cycle
// and depth guards. Cut-off or empty relations yield a marker node rather than
// an error (§10.1). The returned node carries its Object.
func (e *evaluator) expandRelation(object, relation string) (UsersetNode, error) {
	node := object + "#" + relation
	if e.visited[node] {
		return UsersetNode{Object: object, Relation: relation, Truncated: true}, nil
	}
	e.depth++
	if e.depth > e.s.opts.maxDepth {
		e.depth--
		return UsersetNode{Object: object, Relation: relation, Truncated: true}, nil
	}
	e.visited[node] = true
	defer func() {
		delete(e.visited, node)
		e.depth--
	}()

	cfg, found, err := e.configFor(namespaceOf(object))
	if err != nil {
		return UsersetNode{}, err
	}
	if !found {
		return UsersetNode{Kind: NodeThis, Object: object, Relation: relation}, nil // empty userset
	}
	rw, ok := cfg.Relations[relation]
	if !ok {
		return UsersetNode{Kind: NodeThis, Object: object, Relation: relation}, nil
	}
	n, err := e.expandRewrite(object, relation, rw)
	if err != nil {
		return UsersetNode{}, err
	}
	n.Object = object
	return n, nil
}

// expandRewrite builds the node for one rewrite of object#relation (§10.1).
func (e *evaluator) expandRewrite(object, relation string, rw Rewrite) (UsersetNode, error) {
	switch {
	case isEmptyRewrite(rw) || rw.This != nil:
		subs, err := e.directSubjects(object, relation)
		if err != nil {
			return UsersetNode{}, err
		}
		return UsersetNode{Kind: NodeThis, Subjects: subs}, nil

	case rw.ComputedUserset != nil:
		child, err := e.expandRelation(object, rw.ComputedUserset.Relation)
		if err != nil {
			return UsersetNode{}, err
		}
		return UsersetNode{Kind: NodeComputedUserset, Relation: rw.ComputedUserset.Relation, Children: []UsersetNode{child}}, nil

	case rw.TupleToUserset != nil:
		parents, err := e.tuplesetObjects(object, rw.TupleToUserset.Tupleset)
		if err != nil {
			return UsersetNode{}, err
		}
		children := make([]UsersetNode, 0, len(parents))
		for _, p := range parents {
			child, err := e.expandRelation(p, rw.TupleToUserset.ComputedUserset)
			if err != nil {
				return UsersetNode{}, err
			}
			children = append(children, child)
		}
		return UsersetNode{Kind: NodeTupleToUserset, Tupleset: rw.TupleToUserset.Tupleset, Relation: rw.TupleToUserset.ComputedUserset, Children: children}, nil

	case rw.Union != nil:
		children, err := e.expandChildren(object, relation, rw.Union.Children)
		if err != nil {
			return UsersetNode{}, err
		}
		return UsersetNode{Kind: NodeUnion, Children: children}, nil

	case rw.Intersection != nil:
		children, err := e.expandChildren(object, relation, rw.Intersection.Children)
		if err != nil {
			return UsersetNode{}, err
		}
		return UsersetNode{Kind: NodeIntersection, Children: children}, nil

	case rw.Exclusion != nil:
		base, err := e.expandRewrite(object, relation, *rw.Exclusion.Base)
		if err != nil {
			return UsersetNode{}, err
		}
		sub, err := e.expandRewrite(object, relation, *rw.Exclusion.Subtract)
		if err != nil {
			return UsersetNode{}, err
		}
		return UsersetNode{Kind: NodeExclusion, Children: []UsersetNode{base, sub}}, nil
	}
	return UsersetNode{Kind: NodeThis}, nil
}

func (e *evaluator) expandChildren(object, relation string, rws []Rewrite) ([]UsersetNode, error) {
	out := make([]UsersetNode, 0, len(rws))
	for _, rw := range rws {
		n, err := e.expandRewrite(object, relation, rw)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// directSubjects returns the active direct subject strings for object#relation,
// in sorted order.
func (e *evaluator) directSubjects(object, relation string) ([]string, error) {
	var subs []string
	err := e.eachSubject(object, relation, func(subj string, m tupleMeta) (bool, error) {
		if m.activeAt(e.asOf) {
			subs = append(subs, subj)
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(subs)
	return subs, nil
}

// tuplesetObjects returns the active object-subject references stored for
// object#tupleset, in sorted order. Non-object subjects are ignored (§5.2).
func (e *evaluator) tuplesetObjects(object, tupleset string) ([]string, error) {
	var objs []string
	err := e.eachSubject(object, tupleset, func(subj string, m tupleMeta) (bool, error) {
		if !m.activeAt(e.asOf) {
			return false, nil
		}
		sub, err := parseSubject(subj)
		if err != nil {
			return false, nil
		}
		if sub.kind == kindObject {
			objs = append(objs, sub.namespace+":"+sub.id)
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(objs)
	return objs, nil
}
