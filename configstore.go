// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"fmt"
	"sort"

	"github.com/visvasity/kv"
)

// --- Validation (§5.2, §5.3, §12.5) ---------------------------------------

// validateConfig checks a NamespaceConfig for storage (§12.5): a valid,
// non-reserved namespace, valid relation names, well-formed rewrites that only
// reference declared relations where required, and no statically detectable
// computed_userset cycle.
func validateConfig(cfg *NamespaceConfig) error {
	if cfg == nil {
		return &Error{Code: CodeInvalidArgument, Message: "nil config"}
	}
	if err := validateNamespace(cfg.Namespace); err != nil {
		return err
	}
	if cfg.Namespace == userScheme {
		return &Error{Code: CodeInvalidArgument, Message: "namespace 'user' is reserved for the principal scheme"}
	}
	for _, name := range sortedRelationNames(cfg) {
		if err := validateRelation(name); err != nil {
			return err
		}
		if err := validateRewrite(cfg.Relations[name], cfg.Relations, true); err != nil {
			return fmt.Errorf("relation %q: %w", name, err)
		}
	}
	return detectComputedCycle(cfg)
}

// validateRewrite validates a single rewrite node. A top-level relation rewrite
// that is empty defaults to _this and is valid (§5.1); a nested empty rewrite is
// an error (§5.3). Exactly one node kind must be set otherwise.
func validateRewrite(rw Rewrite, relations map[string]Rewrite, topLevel bool) error {
	if isEmptyRewrite(rw) {
		if topLevel {
			return nil // defaults to _this
		}
		return &Error{Code: CodeInvalidArgument, Message: "empty rewrite node"}
	}
	if n := rewriteNodeCount(rw); n != 1 {
		return &Error{Code: CodeInvalidArgument, Message: fmt.Sprintf("rewrite must set exactly one node kind, found %d", n)}
	}
	switch {
	case rw.This != nil:
		return nil
	case rw.ComputedUserset != nil:
		if err := validateRelation(rw.ComputedUserset.Relation); err != nil {
			return err
		}
		if _, ok := relations[rw.ComputedUserset.Relation]; !ok {
			return &Error{Code: CodeRelationUndeclared, Message: "computed_userset references undeclared relation: " + rw.ComputedUserset.Relation, Relation: rw.ComputedUserset.Relation}
		}
		return nil
	case rw.TupleToUserset != nil:
		// The tupleset relation is on this namespace and must be declared; the
		// computed_userset relation is evaluated on the target (possibly other)
		// namespace, so it is only checked syntactically here (§12.5).
		if err := validateRelation(rw.TupleToUserset.Tupleset); err != nil {
			return err
		}
		if err := validateRelation(rw.TupleToUserset.ComputedUserset); err != nil {
			return err
		}
		if _, ok := relations[rw.TupleToUserset.Tupleset]; !ok {
			return &Error{Code: CodeRelationUndeclared, Message: "tuple_to_userset references undeclared tupleset relation: " + rw.TupleToUserset.Tupleset, Relation: rw.TupleToUserset.Tupleset}
		}
		return nil
	case rw.Union != nil:
		if len(rw.Union.Children) == 0 {
			return &Error{Code: CodeInvalidArgument, Message: "union has no children"}
		}
		for _, c := range rw.Union.Children {
			if err := validateRewrite(c, relations, false); err != nil {
				return err
			}
		}
		return nil
	case rw.Intersection != nil:
		if len(rw.Intersection.Children) == 0 {
			return &Error{Code: CodeInvalidArgument, Message: "intersection has no children"}
		}
		for _, c := range rw.Intersection.Children {
			if err := validateRewrite(c, relations, false); err != nil {
				return err
			}
		}
		return nil
	case rw.Exclusion != nil:
		if rw.Exclusion.Base == nil || rw.Exclusion.Subtract == nil {
			return &Error{Code: CodeInvalidArgument, Message: "exclusion requires both base and subtract"}
		}
		if err := validateRewrite(*rw.Exclusion.Base, relations, false); err != nil {
			return err
		}
		return validateRewrite(*rw.Exclusion.Subtract, relations, false)
	}
	return &Error{Code: CodeInvalidArgument, Message: "unknown rewrite node"}
}

func isEmptyRewrite(rw Rewrite) bool {
	return rewriteNodeCount(rw) == 0
}

func rewriteNodeCount(rw Rewrite) int {
	n := 0
	if rw.This != nil {
		n++
	}
	if rw.ComputedUserset != nil {
		n++
	}
	if rw.TupleToUserset != nil {
		n++
	}
	if rw.Union != nil {
		n++
	}
	if rw.Intersection != nil {
		n++
	}
	if rw.Exclusion != nil {
		n++
	}
	return n
}

// collectComputedTargets gathers, into set, every relation referenced by a
// computed_userset node anywhere in rw's tree. These are the same-object edges
// used for cycle detection; This and tuple_to_userset create no such edge.
func collectComputedTargets(rw Rewrite, set map[string]struct{}) {
	switch {
	case rw.ComputedUserset != nil:
		set[rw.ComputedUserset.Relation] = struct{}{}
	case rw.Union != nil:
		for _, c := range rw.Union.Children {
			collectComputedTargets(c, set)
		}
	case rw.Intersection != nil:
		for _, c := range rw.Intersection.Children {
			collectComputedTargets(c, set)
		}
	case rw.Exclusion != nil:
		if rw.Exclusion.Base != nil {
			collectComputedTargets(*rw.Exclusion.Base, set)
		}
		if rw.Exclusion.Subtract != nil {
			collectComputedTargets(*rw.Exclusion.Subtract, set)
		}
	}
}

// detectComputedCycle rejects configs whose computed_userset references form a
// cycle within the same namespace (§12.5), which would not terminate under a
// single object without the runtime cycle guard.
func detectComputedCycle(cfg *NamespaceConfig) error {
	edges := make(map[string][]string, len(cfg.Relations))
	for name, rw := range cfg.Relations {
		set := make(map[string]struct{})
		collectComputedTargets(rw, set)
		var targets []string
		for t := range set {
			if _, ok := cfg.Relations[t]; ok {
				targets = append(targets, t)
			}
		}
		sort.Strings(targets)
		edges[name] = targets
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(cfg.Relations))
	var stack []string
	var visit func(string) error
	visit = func(n string) error {
		color[n] = gray
		stack = append(stack, n)
		for _, m := range edges[n] {
			switch color[m] {
			case gray:
				return &Error{Code: CodeInvalidArgument, Message: "computed_userset cycle: " + cyclePath(stack, m)}
			case white:
				if err := visit(m); err != nil {
					return err
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return nil
	}
	for _, name := range sortedRelationNames(cfg) {
		if color[name] == white {
			if err := visit(name); err != nil {
				return err
			}
		}
	}
	return nil
}

func cyclePath(stack []string, back string) string {
	// Render from where the cycle re-enters, e.g. "a -> b -> a".
	start := 0
	for i, n := range stack {
		if n == back {
			start = i
			break
		}
	}
	path := append(append([]string(nil), stack[start:]...), back)
	out := ""
	for i, n := range path {
		if i > 0 {
			out += " -> "
		}
		out += n
	}
	return out
}

func sortedRelationNames(cfg *NamespaceConfig) []string {
	names := make([]string, 0, len(cfg.Relations))
	for name := range cfg.Relations {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func cloneConfig(cfg *NamespaceConfig) (*NamespaceConfig, error) {
	data, err := gobEncode(cfg)
	if err != nil {
		return nil, err
	}
	var out NamespaceConfig
	if err := gobDecode(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- Reads ----------------------------------------------------------------

// effectiveConfig reads a namespace's current config through the operation's
// reader (§5.5). It returns found=false when the namespace is unregistered.
func (s *Service) effectiveConfig(ctx context.Context, r kv.Getter, namespace string) (*NamespaceConfig, bool, error) {
	var cfg NamespaceConfig
	found, err := getGob(ctx, r, configHeadKey(s.opts.keyPrefix, namespace), &cfg)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	return &cfg, true, nil
}

// ReadConfig returns the current stored config for the namespace (§13.3).
func (s *Service) ReadConfig(ctx context.Context, namespace string) (*NamespaceConfig, error) {
	if err := validateNamespace(namespace); err != nil {
		return nil, err
	}
	snap, err := s.db.NewSnapshot(ctx)
	if err != nil {
		return nil, fromKV(err)
	}
	defer snap.Discard(ctx)

	cfg, found, err := s.effectiveConfig(ctx, snap, namespace)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &Error{Code: CodeNamespaceUnregistered, Message: "no config stored for namespace: " + namespace}
	}
	return cfg, nil
}

// ListConfigs returns all stored current configs in namespace order (§13.3).
func (s *Service) ListConfigs(ctx context.Context) ([]NamespaceConfig, error) {
	snap, err := s.db.NewSnapshot(ctx)
	if err != nil {
		return nil, fromKV(err)
	}
	defer snap.Discard(ctx)

	beg, end := configHeadRange(s.opts.keyPrefix)
	return scanGob[NamespaceConfig](ctx, snap, beg, end)
}

// ReadConfigVersion returns the immutable config stored at a specific past
// version (§5.5, §11.2).
func (s *Service) ReadConfigVersion(ctx context.Context, namespace string, version uint64) (*NamespaceConfig, error) {
	if err := validateNamespace(namespace); err != nil {
		return nil, err
	}
	snap, err := s.db.NewSnapshot(ctx)
	if err != nil {
		return nil, fromKV(err)
	}
	defer snap.Discard(ctx)

	var cfg NamespaceConfig
	found, err := getGob(ctx, snap, configHistoryKey(s.opts.keyPrefix, namespace, version), &cfg)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &Error{Code: CodeNamespaceUnregistered, Message: fmt.Sprintf("no config version %d for namespace: %s", version, namespace)}
	}
	return &cfg, nil
}

// ListConfigVersions returns the stored versions of a namespace in ascending
// order (§13.3).
func (s *Service) ListConfigVersions(ctx context.Context, namespace string) ([]uint64, error) {
	if err := validateNamespace(namespace); err != nil {
		return nil, err
	}
	snap, err := s.db.NewSnapshot(ctx)
	if err != nil {
		return nil, fromKV(err)
	}
	defer snap.Discard(ctx)

	beg, end := configHistoryRange(s.opts.keyPrefix, namespace)
	configs, err := scanGob[NamespaceConfig](ctx, snap, beg, end)
	if err != nil {
		return nil, err
	}
	versions := make([]uint64, len(configs))
	for i := range configs {
		versions[i] = configs[i].Version
	}
	return versions, nil
}

// --- Write (compare-and-set + history) ------------------------------------

// WriteConfig creates or updates a namespace config with compare-and-set on
// Version, writing the head and an immutable history record atomically (§5.5,
// §11.2). It returns the stored config carrying its newly assigned Version.
func (s *Service) WriteConfig(ctx context.Context, cfg *NamespaceConfig) (*NamespaceConfig, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	prefix := s.opts.keyPrefix

	var result *NamespaceConfig
	err := s.inTx(ctx, func(tx kv.Transaction) error {
		// The read of the head record joins the transaction's read set, so a
		// concurrent committed update to an existing namespace is detected as a
		// conflict at commit (§9.3).
		var cur NamespaceConfig
		found, err := getGob(ctx, tx, configHeadKey(prefix, cfg.Namespace), &cur)
		if err != nil {
			return err
		}
		var curVer uint64
		if found {
			curVer = cur.Version
		}
		if cfg.Version != curVer {
			return &Error{
				Code:    CodeConflict,
				Message: fmt.Sprintf("config version mismatch for %q: stored=%d, submitted=%d", cfg.Namespace, curVer, cfg.Version),
			}
		}
		stored, err := cloneConfig(cfg)
		if err != nil {
			return err
		}
		stored.Version = curVer + 1
		if err := setGob(ctx, tx, configHeadKey(prefix, cfg.Namespace), stored); err != nil {
			return err
		}
		if err := setGob(ctx, tx, configHistoryKey(prefix, cfg.Namespace, stored.Version), stored); err != nil {
			return err
		}
		result = stored
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
