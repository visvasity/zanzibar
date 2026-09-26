// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"encoding/base64"
	"io"
	"strings"

	"github.com/visvasity/kv/kvutil"
)

// Read page-size bounds (§10.2).
const (
	defaultReadPageSize = 1000
	maxReadPageSize     = 10000
)

// readFilter is the validated, canonicalized form of a ReadRequest filter.
type readFilter struct {
	object    string // canonical "namespace:id", or "" (any)
	namespace string // or "" (any)
	relation  string // or "" (any)
	subject   string // canonical subject string, or "" (any)
}

// Read returns stored tuple records (not computed membership) matching the
// request's filter, in ascending key order, with opaque cursor pagination
// (§10.2). It does not filter by any time window.
func (s *Service) Read(ctx context.Context, req *ReadRequest) (*ReadResponse, error) {
	if req == nil {
		return nil, &Error{Code: CodeInvalidArgument, Message: "nil read request"}
	}
	f, err := s.validateReadFilter(req)
	if err != nil {
		return nil, err
	}
	pageSize := clampReadPageSize(req.PageSize)
	prefix := s.opts.keyPrefix

	// Choose the narrowest index scan the filter allows.
	var beg, end string
	reverse := false
	switch {
	case f.object != "":
		if f.relation != "" {
			beg, end = forwardRelationRange(prefix, f.object, f.relation)
		} else {
			beg, end = forwardObjectRange(prefix, f.object)
		}
	case f.subject != "":
		reverse = true
		beg, end = reverseSubjectRange(prefix, f.subject)
	default:
		// No object or subject: full forward scan (allowed but expensive, §10.2).
		beg, end = kvutil.PrefixRange(prefix + classForward)
	}

	// Apply the continuation cursor: resume strictly after the last emitted key.
	if req.PageToken != "" {
		last, err := decodePageToken(req.PageToken)
		if err != nil {
			return nil, err
		}
		if cur := last + "\x00"; cur > beg {
			beg = cur
		}
	}

	snap, err := s.db.NewSnapshot(ctx)
	if err != nil {
		return nil, fromKV(err)
	}
	defer snap.Discard(ctx)

	resp := &ReadResponse{}
	var ierr error
	var lastKey string
	for k, r := range snap.Ascend(ctx, beg, end, &ierr) {
		object, relation, subject, ok := decodeTupleKey(k, prefix, reverse)
		if !ok {
			continue
		}
		if !f.matches(object, relation, subject) {
			continue
		}
		// One match beyond the page: emit a continuation token and stop.
		if len(resp.Records) == pageSize {
			resp.NextPageToken = encodePageToken(lastKey)
			break
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, fromKV(err)
		}
		var m tupleMeta
		if err := gobDecode(data, &m); err != nil {
			return nil, err
		}
		resp.Records = append(resp.Records, TupleRecord{
			Tuple:             Tuple{Object: object, Relation: relation, Subject: subject},
			CreatedAtUnixNano: m.CreatedAtUnixNano,
			NotBeforeUnixNano: m.NotBeforeUnixNano,
			NotAfterUnixNano:  m.NotAfterUnixNano,
		})
		lastKey = k
	}
	if ierr != nil {
		return nil, fromKV(ierr)
	}
	return resp, nil
}

func (s *Service) validateReadFilter(req *ReadRequest) (readFilter, error) {
	var f readFilter
	if req.Object != "" {
		ns, id, err := parseObject(req.Object)
		if err != nil {
			return f, err
		}
		f.object = ns + ":" + id
	}
	if req.Namespace != "" {
		if err := validateNamespace(req.Namespace); err != nil {
			return f, err
		}
		f.namespace = req.Namespace
	}
	if req.Relation != "" {
		if err := validateRelation(req.Relation); err != nil {
			return f, err
		}
		f.relation = req.Relation
	}
	if req.Subject != "" {
		sub, err := parseSubject(req.Subject)
		if err != nil {
			return f, err
		}
		f.subject = canonicalSubject(sub, s.opts.emailCaseFold)
	}
	return f, nil
}

// matches reports whether a decoded tuple satisfies every set filter field.
func (f readFilter) matches(object, relation, subject string) bool {
	if f.object != "" && object != f.object {
		return false
	}
	if f.namespace != "" && namespaceOf(object) != f.namespace {
		return false
	}
	if f.relation != "" && relation != f.relation {
		return false
	}
	if f.subject != "" && subject != f.subject {
		return false
	}
	return true
}

// decodeTupleKey reconstructs (object, relation, subject) from a forward or
// reverse index key. Because object, relation, and subject never contain '/'
// (§12), a 3-way split is unambiguous.
func decodeTupleKey(key, prefix string, reverse bool) (object, relation, subject string, ok bool) {
	class := classForward
	if reverse {
		class = classReverse
	}
	rest, ok := strings.CutPrefix(key, prefix+class)
	if !ok {
		return "", "", "", false
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) != 3 {
		return "", "", "", false
	}
	if reverse {
		// s/<subject>/<object>/<relation>
		return parts[1], parts[2], parts[0], true
	}
	// t/<object>/<relation>/<subject>
	return parts[0], parts[1], parts[2], true
}

// namespaceOf returns the namespace part of a canonical "namespace:id" object.
func namespaceOf(object string) string {
	ns, _, _ := strings.Cut(object, ":")
	return ns
}

func clampReadPageSize(n int) int {
	if n <= 0 {
		return defaultReadPageSize
	}
	if n > maxReadPageSize {
		return maxReadPageSize
	}
	return n
}

func encodePageToken(key string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(key))
}

func decodePageToken(tok string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		return "", &Error{Code: CodeInvalidArgument, Message: "invalid page token"}
	}
	return string(b), nil
}
