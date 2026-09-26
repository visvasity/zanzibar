// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"errors"
	"testing"
)

func TestValidateNamespace(t *testing.T) {
	valid := []string{"doc", "folder", "group", "a", "A1", "with_underscore", "with-dash", "MixedCase123"}
	for _, s := range valid {
		if err := validateNamespace(s); err != nil {
			t.Errorf("validateNamespace(%q) = %v, want nil", s, err)
		}
	}
	invalid := []string{"", "has space", "has/slash", "has:colon", "has#hash", "dot.ted", "emoji😀", "a@b"}
	for _, s := range invalid {
		if err := validateNamespace(s); err == nil {
			t.Errorf("validateNamespace(%q) = nil, want error", s)
		} else if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("validateNamespace(%q) err = %v, want ErrInvalidArgument", s, err)
		}
	}
}

func TestValidateID(t *testing.T) {
	valid := []string{"readme", "a@b.com", "dot.ted", "123", "u-n_d.e@r", "UUID-1234"}
	for _, s := range valid {
		if err := validateID(s); err != nil {
			t.Errorf("validateID(%q) = %v, want nil", s, err)
		}
	}
	invalid := []string{"", "has space", "a/b", "a#b", "a:b", "tab\ther", "new\nline"}
	for _, s := range invalid {
		if err := validateID(s); err == nil {
			t.Errorf("validateID(%q) = nil, want error", s)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	valid := []string{"alice@example.com", "a.b+c@sub.domain.io", "x@y"}
	for _, s := range valid {
		if err := validateEmail(s); err != nil {
			t.Errorf("validateEmail(%q) = %v, want nil", s, err)
		}
	}
	invalid := []string{"", "noatsign", "two@@at.com", "a@b@c", "@nolocal.com", "nodomain@", "has space@x.com", "has/slash@x.com", "has#hash@x.com"}
	for _, s := range invalid {
		if err := validateEmail(s); err == nil {
			t.Errorf("validateEmail(%q) = nil, want error", s)
		}
	}
}

func TestParseObject(t *testing.T) {
	tests := []struct {
		in      string
		ns, id  string
		wantErr bool
	}{
		{"doc:readme", "doc", "readme", false},
		{"folder:proj", "folder", "proj", false},
		{"group:eng", "group", "eng", false},
		{"user:alice@example.com", "user", "alice@example.com", false}, // syntactically an object; 'user' reserved is enforced by parseSubject
		{"nocolon", "", "", true},
		{"doc:", "", "", true},    // empty id
		{":readme", "", "", true}, // empty ns
		{"doc:a:b", "", "", true}, // id contains ':'
		{"bad ns:x", "", "", true},
	}
	for _, tt := range tests {
		ns, id, err := parseObject(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseObject(%q) = (%q,%q,nil), want error", tt.in, ns, id)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseObject(%q) err = %v, want nil", tt.in, err)
			continue
		}
		if ns != tt.ns || id != tt.id {
			t.Errorf("parseObject(%q) = (%q,%q), want (%q,%q)", tt.in, ns, id, tt.ns, tt.id)
		}
	}
}

func TestParseSubject(t *testing.T) {
	tests := []struct {
		in       string
		kind     subjectKind
		wildcard bool
		wantErr  bool
	}{
		{"user:alice@example.com", kindUser, false, false},
		{"user:*", kindUser, true, false},
		{"group:eng#member", kindUserset, false, false},
		{"doc:readme#viewer", kindUserset, false, false},
		{"folder:proj", kindObject, false, false},
		// errors
		{"", 0, false, true},
		{"user:notanemail", 0, false, true},               // no '@'
		{"user:a@b#member", 0, false, true},               // email cannot contain '#' -> parses as userset over 'user'
		{"group:eng#member#extra", 0, false, true},        // multiple '#'
		{"user:alice@example.com#member", 0, false, true}, // userset over reserved 'user'
		{"group:eng#", 0, false, true},                    // empty relation
		{"bad ns:x", 0, false, true},
		{"nocolon", 0, false, true},
	}
	for _, tt := range tests {
		s, err := parseSubject(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseSubject(%q) = %+v, want error", tt.in, s)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSubject(%q) err = %v, want nil", tt.in, err)
			continue
		}
		if s.kind != tt.kind {
			t.Errorf("parseSubject(%q).kind = %d, want %d", tt.in, s.kind, tt.kind)
		}
		if s.wildcard != tt.wildcard {
			t.Errorf("parseSubject(%q).wildcard = %v, want %v", tt.in, s.wildcard, tt.wildcard)
		}
	}
}

func TestSubjectRoundTrip(t *testing.T) {
	inputs := []string{
		"user:alice@example.com",
		"user:*",
		"group:eng#member",
		"doc:readme#viewer",
		"folder:proj",
		"group:eng-backend#member",
	}
	for _, in := range inputs {
		s, err := parseSubject(in)
		if err != nil {
			t.Fatalf("parseSubject(%q): %v", in, err)
		}
		if got := s.String(); got != in {
			t.Errorf("round-trip: parseSubject(%q).String() = %q", in, got)
		}
	}
}

func TestFoldEmail(t *testing.T) {
	if got := foldEmail("Alice@Example.COM", false); got != "Alice@Example.COM" {
		t.Errorf("foldEmail(no fold) = %q, want unchanged", got)
	}
	if got := foldEmail("Alice@Example.COM", true); got != "alice@example.com" {
		t.Errorf("foldEmail(fold) = %q, want lowercased", got)
	}
}
