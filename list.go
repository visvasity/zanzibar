// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"sort"

	"github.com/visvasity/kv"
)

// --- ListUsers ------------------------------------------------------------

// ListUsers returns the user subjects that are members of Object#Relation,
// flattening usersets and inheritance but not expanding "user:*" (§10.4). It
// evaluates against one snapshot with the §6.4 limits and is paginated.
func (s *Service) ListUsers(ctx context.Context, req *ListUsersRequest) (*ListUsersResponse, error) {
	if req == nil {
		return nil, &Error{Code: CodeInvalidArgument, Message: "nil list-users request"}
	}
	ns, id, err := parseObject(req.Object)
	if err != nil {
		return nil, err
	}
	if err := validateRelation(req.Relation); err != nil {
		return nil, err
	}
	pageSize := clampReadPageSize(req.PageSize)

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
	set, err := e.collectUsers(ns+":"+id, req.Relation)
	if err != nil {
		return nil, err
	}
	users := sortedKeysOf(set)
	page, next, err := paginateStrings(users, req.PageToken, pageSize)
	if err != nil {
		return nil, err
	}
	return &ListUsersResponse{Users: page, NextPageToken: next}, nil
}

// collectUsers returns the set of user subjects in object#relation. It mirrors
// Check but accumulates leaves, computing set algebra for intersection and
// exclusion. On a cycle it yields the empty set (fail-closed), consistent with
// Check denying on a loop.
func (e *evaluator) collectUsers(object, relation string) (map[string]bool, error) {
	node := object + "#" + relation
	if e.visited[node] {
		return map[string]bool{}, nil
	}
	e.depth++
	if e.depth > e.s.opts.maxDepth {
		e.depth--
		return nil, &Error{Code: CodeDepthExceeded, Message: "evaluation depth exceeded", Object: object, Relation: relation}
	}
	e.visited[node] = true
	defer func() {
		delete(e.visited, node)
		e.depth--
	}()

	cfg, found, err := e.configFor(namespaceOf(object))
	if err != nil {
		return nil, err
	}
	if !found {
		return map[string]bool{}, nil
	}
	rw, ok := cfg.Relations[relation]
	if !ok {
		return map[string]bool{}, nil
	}
	return e.collectRewrite(object, relation, rw)
}

func (e *evaluator) collectRewrite(object, relation string, rw Rewrite) (map[string]bool, error) {
	switch {
	case isEmptyRewrite(rw) || rw.This != nil:
		set := map[string]bool{}
		err := e.eachSubject(object, relation, func(subj string, m tupleMeta) (bool, error) {
			if !m.activeAt(e.asOf) {
				return false, nil
			}
			sub, err := parseSubject(subj)
			if err != nil {
				return false, nil
			}
			switch {
			case sub.kind == kindUser && !sub.wildcard:
				set[subj] = true // a concrete user leaf; "user:*" is intentionally skipped
			case sub.kind == kindUserset:
				child, err := e.collectUsers(sub.namespace+":"+sub.id, sub.relation)
				if err != nil {
					return false, err
				}
				mergeInto(set, child)
			}
			return false, nil
		})
		if err != nil {
			return nil, err
		}
		return set, nil

	case rw.ComputedUserset != nil:
		return e.collectUsers(object, rw.ComputedUserset.Relation)

	case rw.TupleToUserset != nil:
		set := map[string]bool{}
		err := e.eachSubject(object, rw.TupleToUserset.Tupleset, func(subj string, m tupleMeta) (bool, error) {
			if !m.activeAt(e.asOf) {
				return false, nil
			}
			sub, err := parseSubject(subj)
			if err != nil {
				return false, nil
			}
			if sub.kind == kindObject {
				child, err := e.collectUsers(sub.namespace+":"+sub.id, rw.TupleToUserset.ComputedUserset)
				if err != nil {
					return false, err
				}
				mergeInto(set, child)
			}
			return false, nil
		})
		if err != nil {
			return nil, err
		}
		return set, nil

	case rw.Union != nil:
		set := map[string]bool{}
		for _, c := range rw.Union.Children {
			child, err := e.collectRewrite(object, relation, c)
			if err != nil {
				return nil, err
			}
			mergeInto(set, child)
		}
		return set, nil

	case rw.Intersection != nil:
		var acc map[string]bool
		for _, c := range rw.Intersection.Children {
			child, err := e.collectRewrite(object, relation, c)
			if err != nil {
				return nil, err
			}
			if acc == nil {
				acc = child
			} else {
				acc = intersectSets(acc, child)
			}
		}
		if acc == nil {
			acc = map[string]bool{}
		}
		return acc, nil

	case rw.Exclusion != nil:
		base, err := e.collectRewrite(object, relation, *rw.Exclusion.Base)
		if err != nil {
			return nil, err
		}
		sub, err := e.collectRewrite(object, relation, *rw.Exclusion.Subtract)
		if err != nil {
			return nil, err
		}
		return minusSets(base, sub), nil
	}
	return map[string]bool{}, nil
}

// --- ListObjects ----------------------------------------------------------

// ListObjects returns the object ids in Namespace on which Subject holds
// Relation (§10.3). It gathers a candidate superset via the reverse index, then
// confirms each candidate with a full Check on the same snapshot, so the result
// is sound and complete relative to Check (subject to the depth limit). It is
// paginated. Candidate gathering may over-read; confirmation is what enforces
// intersection, exclusion, and time-window semantics.
func (s *Service) ListObjects(ctx context.Context, req *ListObjectsRequest) (*ListObjectsResponse, error) {
	if req == nil {
		return nil, &Error{Code: CodeInvalidArgument, Message: "nil list-objects request"}
	}
	if err := validateNamespace(req.Namespace); err != nil {
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
		return nil, &Error{Code: CodeInvalidArgument, Message: "wildcard 'user:*' is not a valid list-objects subject"}
	}
	target := canonicalSubject(sub, s.opts.emailCaseFold)
	pageSize := clampReadPageSize(req.PageSize)

	snap, err := s.db.NewSnapshot(ctx)
	if err != nil {
		return nil, fromKV(err)
	}
	defer snap.Discard(ctx)

	g := &candidateGatherer{
		s:        s,
		ctx:      ctx,
		r:        snap,
		configs:  make(map[string]*NamespaceConfig),
		computed: make(map[string]map[string][]string),
		ttu:      make(map[string]map[[2]string][]string),
		visited:  make(map[nodeKey]bool),
	}
	candidates, err := g.gather(target, req.Namespace, req.Relation)
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, object := range candidates {
		ok, err := s.evalCheck(ctx, snap, object, req.Relation, target, req.AsOfUnixNano, g.configs)
		if err != nil {
			return nil, err
		}
		if ok {
			ids = append(ids, object[len(req.Namespace)+1:]) // strip "namespace:"
		}
	}
	sort.Strings(ids)
	page, next, err := paginateStrings(ids, req.PageToken, pageSize)
	if err != nil {
		return nil, err
	}
	return &ListObjectsResponse{ObjectIDs: page, NextPageToken: next}, nil
}

type nodeKey struct{ object, relation string }

// candidateGatherer performs the backward reachability that yields a superset of
// (object, relation) nodes the subject may belong to (§10.3).
type candidateGatherer struct {
	s        *Service
	ctx      context.Context
	r        kv.Reader
	configs  map[string]*NamespaceConfig
	computed map[string]map[string][]string    // namespace -> (target relation -> relations referencing it via computed_userset)
	ttu      map[string]map[[2]string][]string // namespace -> ((tupleset, computedRel) -> relations)
	visited  map[nodeKey]bool
}

// gather returns the candidate objects (full "namespace:id") in the target
// namespace that hold the target relation for the subject.
func (g *candidateGatherer) gather(subject, namespace, relation string) ([]string, error) {
	candSet := map[string]bool{}
	var queue []nodeKey

	// Seed with the memberships implied by direct tuples object#rel@subject.
	seeds, err := g.reverseNodes(subject)
	if err != nil {
		return nil, err
	}
	queue = append(queue, seeds...)

	for len(queue) > 0 {
		n := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if g.visited[n] {
			continue
		}
		g.visited[n] = true

		if n.relation == relation && namespaceOf(n.object) == namespace {
			candSet[n.object] = true
		}

		// (a) same-object computed_userset: relations that include this relation.
		cfg, err := g.configFor(namespaceOf(n.object))
		if err != nil {
			return nil, err
		}
		if cfg != nil {
			for _, rel2 := range g.computedMap(cfg)[n.relation] {
				queue = append(queue, nodeKey{n.object, rel2})
			}
		}

		// (b) parent -> child via tuple_to_userset: tuples Y#tuprel@<this object>.
		parents, err := g.reverseNodes(n.object)
		if err != nil {
			return nil, err
		}
		for _, y := range parents {
			ycfg, err := g.configFor(namespaceOf(y.object))
			if err != nil {
				return nil, err
			}
			if ycfg == nil {
				continue
			}
			for _, relY := range g.ttuMap(ycfg)[[2]string{y.relation, n.relation}] {
				queue = append(queue, nodeKey{y.object, relY})
			}
		}

		// (c) this userset used as a subject: tuples Z#r2@<this object#relation>.
		usersets, err := g.reverseNodes(n.object + "#" + n.relation)
		if err != nil {
			return nil, err
		}
		queue = append(queue, usersets...)
	}

	out := make([]string, 0, len(candSet))
	for o := range candSet {
		out = append(out, o)
	}
	return out, nil
}

// reverseNodes returns the (object, relation) nodes that have a stored tuple with
// the given subject string, via the reverse index. Time windows are ignored here;
// the final Check confirmation applies them.
func (g *candidateGatherer) reverseNodes(subject string) ([]nodeKey, error) {
	beg, end := reverseSubjectRange(g.s.opts.keyPrefix, subject)
	var out []nodeKey
	var ierr error
	for k := range g.r.Ascend(g.ctx, beg, end, &ierr) {
		object, relation, _, ok := decodeTupleKey(k, g.s.opts.keyPrefix, true)
		if !ok {
			continue
		}
		out = append(out, nodeKey{object, relation})
	}
	if ierr != nil {
		return nil, fromKV(ierr)
	}
	return out, nil
}

func (g *candidateGatherer) configFor(namespace string) (*NamespaceConfig, error) {
	if cfg, ok := g.configs[namespace]; ok {
		return cfg, nil
	}
	cfg, found, err := g.s.effectiveConfig(g.ctx, g.r, namespace)
	if err != nil {
		return nil, err
	}
	if !found {
		g.configs[namespace] = nil
		return nil, nil
	}
	g.configs[namespace] = cfg
	return cfg, nil
}

// computedMap returns, for a config, target-relation -> relations whose rewrite
// references it via computed_userset.
func (g *candidateGatherer) computedMap(cfg *NamespaceConfig) map[string][]string {
	if m, ok := g.computed[cfg.Namespace]; ok {
		return m
	}
	m := map[string][]string{}
	for rel2, rw := range cfg.Relations {
		set := map[string]struct{}{}
		collectComputedTargets(rw, set)
		for target := range set {
			m[target] = append(m[target], rel2)
		}
	}
	g.computed[cfg.Namespace] = m
	return m
}

// ttuMap returns, for a config, (tupleset, computedRel) -> relations whose
// rewrite contains that tuple_to_userset node.
func (g *candidateGatherer) ttuMap(cfg *NamespaceConfig) map[[2]string][]string {
	if m, ok := g.ttu[cfg.Namespace]; ok {
		return m
	}
	m := map[[2]string][]string{}
	for relY, rw := range cfg.Relations {
		var nodes []TupleToUserset
		collectTupleToUsersets(rw, &nodes)
		for _, n := range nodes {
			key := [2]string{n.Tupleset, n.ComputedUserset}
			m[key] = append(m[key], relY)
		}
	}
	g.ttu[cfg.Namespace] = m
	return m
}

// --- shared helpers -------------------------------------------------------

func mergeInto(dst, src map[string]bool) {
	for k := range src {
		dst[k] = true
	}
}

func intersectSets(a, b map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		if b[k] {
			out[k] = true
		}
	}
	return out
}

func minusSets(base, sub map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range base {
		if !sub[k] {
			out[k] = true
		}
	}
	return out
}

func sortedKeysOf(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// paginateStrings returns the page of sorted values strictly after the cursor
// token, plus a token for the next page (empty when the page is the last).
func paginateStrings(sorted []string, token string, pageSize int) ([]string, string, error) {
	start := 0
	if token != "" {
		last, err := decodePageToken(token)
		if err != nil {
			return nil, "", err
		}
		start = sort.Search(len(sorted), func(i int) bool { return sorted[i] > last })
	}
	if start >= len(sorted) {
		return nil, "", nil
	}
	end := start + pageSize
	if end >= len(sorted) {
		return sorted[start:], "", nil
	}
	page := sorted[start:end]
	return page, encodePageToken(page[len(page)-1]), nil
}
