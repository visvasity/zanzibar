// Copyright (c) 2026 Visvasity LLC

package schema

import (
	"reflect"
	"testing"

	"github.com/visvasity/zanzibar"
)

func TestParseBasics(t *testing.T) {
	src := `
# the account tenant
namespace account {
  relation platform
  relation owner
  relation admin = _this or owner or admin from platform
  relation both  = a and b
  relation only_a = a except b
}`
	cfgs, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfgs) != 1 || cfgs[0].Namespace != "account" {
		t.Fatalf("got %+v", cfgs)
	}
	rel := cfgs[0].Relations

	// no '=' means direct-only.
	if rel["platform"].This == nil {
		t.Errorf("platform should be _this, got %+v", rel["platform"])
	}
	// union with computed + tuple_to_userset.
	adm := rel["admin"]
	if adm.Union == nil || len(adm.Union.Children) != 3 {
		t.Fatalf("admin should be a 3-way union, got %+v", adm)
	}
	if adm.Union.Children[0].This == nil {
		t.Errorf("admin child 0 should be _this")
	}
	if cu := adm.Union.Children[1].ComputedUserset; cu == nil || cu.Relation != "owner" {
		t.Errorf("admin child 1 should be computed owner, got %+v", adm.Union.Children[1])
	}
	if ttu := adm.Union.Children[2].TupleToUserset; ttu == nil || ttu.Tupleset != "platform" || ttu.ComputedUserset != "admin" {
		t.Errorf("admin child 2 should be 'admin from platform', got %+v", adm.Union.Children[2])
	}
	// intersection and exclusion.
	if rel["both"].Intersection == nil {
		t.Errorf("both should be intersection")
	}
	if rel["only_a"].Exclusion == nil {
		t.Errorf("only_a should be exclusion")
	}
}

func TestParsePrecedenceAndParens(t *testing.T) {
	// 'or' is lowest: a or b and c  ==  a or (b and c)
	cfgs, err := Parse([]byte(`namespace n { relation r = a or b and c }`))
	if err != nil {
		t.Fatal(err)
	}
	r := cfgs[0].Relations["r"]
	if r.Union == nil || len(r.Union.Children) != 2 {
		t.Fatalf("expected top-level union of 2, got %+v", r)
	}
	if r.Union.Children[1].Intersection == nil {
		t.Errorf("second child should be an intersection, got %+v", r.Union.Children[1])
	}

	// Parentheses override: (a or b) and c  ==  intersection at top.
	cfgs, _ = Parse([]byte(`namespace n { relation r = (a or b) and c }`))
	r = cfgs[0].Relations["r"]
	if r.Intersection == nil || r.Intersection.Children[0].Union == nil {
		t.Errorf("expected top-level intersection with a union child, got %+v", r)
	}
}

func TestRoundTrip(t *testing.T) {
	cfgs := []zanzibar.NamespaceConfig{
		{
			Namespace: "platform",
			Relations: map[string]zanzibar.Rewrite{
				"admin":   {This: &zanzibar.This{}},
				"support": {Union: &zanzibar.SetOperation{Children: []zanzibar.Rewrite{{This: &zanzibar.This{}}, {ComputedUserset: &zanzibar.ComputedUserset{Relation: "admin"}}}}},
			},
		},
		{
			Namespace: "account",
			Relations: map[string]zanzibar.Rewrite{
				"platform": {This: &zanzibar.This{}},
				"owner":    {This: &zanzibar.This{}},
				"admin": {Union: &zanzibar.SetOperation{Children: []zanzibar.Rewrite{
					{This: &zanzibar.This{}},
					{ComputedUserset: &zanzibar.ComputedUserset{Relation: "owner"}},
					{TupleToUserset: &zanzibar.TupleToUserset{Tupleset: "platform", ComputedUserset: "admin"}},
				}}},
				"only_a": {Exclusion: &zanzibar.Exclusion{
					Base:     &zanzibar.Rewrite{ComputedUserset: &zanzibar.ComputedUserset{Relation: "a"}},
					Subtract: &zanzibar.Rewrite{ComputedUserset: &zanzibar.ComputedUserset{Relation: "b"}},
				}},
				"mixed": {Union: &zanzibar.SetOperation{Children: []zanzibar.Rewrite{
					{ComputedUserset: &zanzibar.ComputedUserset{Relation: "a"}},
					{Intersection: &zanzibar.SetOperation{Children: []zanzibar.Rewrite{
						{ComputedUserset: &zanzibar.ComputedUserset{Relation: "b"}},
						{ComputedUserset: &zanzibar.ComputedUserset{Relation: "c"}},
					}}},
				}}},
			},
		},
	}
	got, err := Parse(Format(cfgs))
	if err != nil {
		t.Fatalf("Parse(Format): %v", err)
	}
	// Compare as maps keyed by namespace (Parse preserves order, but be robust).
	want := map[string]zanzibar.NamespaceConfig{}
	for _, c := range cfgs {
		want[c.Namespace] = c
	}
	for _, c := range got {
		if !reflect.DeepEqual(c, want[c.Namespace]) {
			t.Errorf("round-trip mismatch for %s:\n got %#v\nwant %#v", c.Namespace, c, want[c.Namespace])
		}
	}
	if len(got) != len(cfgs) {
		t.Errorf("got %d namespaces, want %d", len(got), len(cfgs))
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		`namespace {}`,                           // missing name
		`namespace n { relation }`,               // missing relation name
		`namespace n { relation r = }`,           // empty expression
		`namespace n { relation or }`,            // reserved word as name
		`namespace n { relation r = a or }`,      // dangling operator
		`namespace n { relation r = (a or b }`,   // unbalanced paren
		`namespace n { relation a  relation a }`, // duplicate relation
		`namespace n {`,                          // unterminated block
		`namespace n { relation r = a from }`,    // missing tupleset
	}
	for _, s := range bad {
		if _, err := Parse([]byte(s)); err == nil {
			t.Errorf("Parse(%q) = nil error, want error", s)
		}
	}
}
