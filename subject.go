// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"strings"
	"unicode"
)

// Subject scheme and wildcard tokens (§3.2, §12).
const (
	userScheme = "user"  // reserved namespace name for the principal scheme
	userPrefix = "user:" // marks a user subject
	wildcard   = "*"     // "user:*", the optional wildcard user
)

// subjectKind identifies which of the three subject forms a parsed subject is.
type subjectKind uint8

const (
	kindUser    subjectKind = iota + 1 // "user:<email>" or "user:*"
	kindUserset                        // "namespace:id#relation"
	kindObject                         // "namespace:id"
)

// subject is a parsed, validated subject (§3.2). Only the fields relevant to
// kind are populated.
type subject struct {
	kind      subjectKind
	email     string // kindUser, non-wildcard
	wildcard  bool   // kindUser, "user:*"
	namespace string // kindUserset, kindObject
	id        string // kindUserset, kindObject
	relation  string // kindUserset
}

// String returns the canonical wire form of the subject. For any valid input,
// parseSubject(x).String() == x (modulo email case-folding, which is applied
// separately at a boundary).
func (s subject) String() string {
	switch s.kind {
	case kindUser:
		if s.wildcard {
			return userPrefix + wildcard
		}
		return userPrefix + s.email
	case kindUserset:
		return s.namespace + ":" + s.id + "#" + s.relation
	case kindObject:
		return s.namespace + ":" + s.id
	default:
		return ""
	}
}

// validateNamespace checks a namespace name (§12.1): non-empty, only
// ALPHA / DIGIT / "_" / "-".
func validateNamespace(ns string) error {
	if ns == "" {
		return &Error{Code: CodeInvalidArgument, Message: "empty namespace"}
	}
	for _, r := range ns {
		if !isIdentRune(r) {
			return &Error{Code: CodeInvalidArgument, Message: "namespace has invalid character: " + ns}
		}
	}
	return nil
}

// validateRelation checks a relation name (§12.1): same charset as a namespace.
func validateRelation(rel string) error {
	if rel == "" {
		return &Error{Code: CodeInvalidArgument, Message: "empty relation"}
	}
	for _, r := range rel {
		if !isIdentRune(r) {
			return &Error{Code: CodeInvalidArgument, Message: "relation has invalid character: " + rel}
		}
	}
	return nil
}

// validateID checks an object id (§12.2): non-empty, and free of '/', '#', ':',
// whitespace, and control characters.
func validateID(id string) error {
	if id == "" {
		return &Error{Code: CodeInvalidArgument, Message: "empty object id"}
	}
	for _, r := range id {
		if r == '/' || r == '#' || r == ':' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return &Error{Code: CodeInvalidArgument, Message: "object id has forbidden character: " + id}
		}
	}
	return nil
}

// validateEmail checks a user email (§12.4): exactly one '@' with non-empty
// local and domain parts, and free of '/', '#', whitespace, and control
// characters. The email is otherwise treated as an opaque identifier.
func validateEmail(email string) error {
	if email == "" {
		return &Error{Code: CodeInvalidArgument, Message: "empty email"}
	}
	at := strings.IndexByte(email, '@')
	if at < 0 || at != strings.LastIndexByte(email, '@') {
		return &Error{Code: CodeInvalidArgument, Message: "email must contain exactly one '@': " + email}
	}
	if at == 0 || at == len(email)-1 {
		return &Error{Code: CodeInvalidArgument, Message: "email has empty local or domain part: " + email}
	}
	for _, r := range email {
		if r == '/' || r == '#' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return &Error{Code: CodeInvalidArgument, Message: "email has forbidden character: " + email}
		}
	}
	return nil
}

// parseObject splits and validates an "namespace:id" object reference.
func parseObject(s string) (namespace, id string, err error) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return "", "", &Error{Code: CodeInvalidArgument, Message: "object must be namespace:id: " + s}
	}
	namespace, id = s[:i], s[i+1:]
	if err := validateNamespace(namespace); err != nil {
		return "", "", err
	}
	if err := validateID(id); err != nil {
		return "", "", err
	}
	return namespace, id, nil
}

// parseSubject parses and validates a subject string into one of the three
// forms (§3.2, §12.3). The presence of '#' marks a userset; the "user:" prefix
// marks a user; otherwise it is an object.
func parseSubject(s string) (subject, error) {
	if s == "" {
		return subject{}, &Error{Code: CodeInvalidArgument, Message: "empty subject"}
	}
	if i := strings.IndexByte(s, '#'); i >= 0 {
		if strings.IndexByte(s[i+1:], '#') >= 0 {
			return subject{}, &Error{Code: CodeInvalidArgument, Message: "userset subject has multiple '#': " + s}
		}
		objPart, rel := s[:i], s[i+1:]
		ns, id, err := parseObject(objPart)
		if err != nil {
			return subject{}, err
		}
		if err := validateRelation(rel); err != nil {
			return subject{}, err
		}
		if ns == userScheme {
			return subject{}, &Error{Code: CodeInvalidArgument, Message: "reserved namespace 'user' cannot be a userset: " + s}
		}
		return subject{kind: kindUserset, namespace: ns, id: id, relation: rel}, nil
	}
	if rest, ok := strings.CutPrefix(s, userPrefix); ok {
		if rest == wildcard {
			return subject{kind: kindUser, wildcard: true}, nil
		}
		if err := validateEmail(rest); err != nil {
			return subject{}, err
		}
		return subject{kind: kindUser, email: rest}, nil
	}
	ns, id, err := parseObject(s)
	if err != nil {
		return subject{}, err
	}
	if ns == userScheme {
		return subject{}, &Error{Code: CodeInvalidArgument, Message: "reserved namespace 'user': " + s}
	}
	return subject{kind: kindObject, namespace: ns, id: id}, nil
}

// foldEmail lowercases the email when case-folding is enabled ([WithEmailCaseFold]),
// otherwise returns it unchanged (§12.4).
func foldEmail(email string, fold bool) string {
	if fold {
		return strings.ToLower(email)
	}
	return email
}

// isIdentRune reports whether r is allowed in a namespace or relation name.
func isIdentRune(r rune) bool {
	return r == '_' || r == '-' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z')
}
