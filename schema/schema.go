// Copyright (c) 2026 Visvasity LLC

// Package schema provides a compact text language for authoring zanzibar
// NamespaceConfigs, as an ergonomic alternative to hand-written JSON. It has no
// dependencies beyond the core zanzibar types.
//
// # Language
//
// A schema is a sequence of namespace blocks, each declaring relations:
//
//	# comments start with '#'
//	namespace account {
//	  relation platform            # no '=' means direct-only (_this)
//	  relation owner
//	  relation admin  = _this or owner or admin from platform
//	  relation member = _this or admin
//	  relation view   = member or support from platform
//	  relation manage = admin
//	  relation refund = refunder from platform
//	}
//
// A relation's rewrite expression is built from:
//
//   - _this            direct tuples (§5.2 "_this")
//   - R                the relation R on the same object (computed_userset)
//   - R from T         relation R on objects reached via tupleset T (tuple_to_userset)
//   - a or b           union
//   - a and b          intersection
//   - a except b       exclusion (a minus b)
//   - ( ... )          grouping
//
// Operator precedence, lowest to highest: or, and, except. Use parentheses to
// override. Parse produces []NamespaceConfig with Version 0 (create); semantic
// validation (declared relations, cycles) happens when the config is written.
package schema

import (
	"fmt"
	"sort"
	"strings"

	"github.com/visvasity/zanzibar"
)

// reserved words that cannot be used as namespace or relation names.
var reserved = map[string]bool{
	"namespace": true, "relation": true, "from": true,
	"or": true, "and": true, "except": true, "_this": true,
}

// Parse parses schema source into namespace configs.
func Parse(src []byte) ([]zanzibar.NamespaceConfig, error) {
	toks, err := lex(string(src))
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	return p.parseFile()
}

// Format renders configs as schema source. It is the inverse of Parse:
// Parse(Format(cfgs)) reproduces the same configs.
func Format(cfgs []zanzibar.NamespaceConfig) []byte {
	var b strings.Builder
	for i, cfg := range cfgs {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "namespace %s {\n", cfg.Namespace)
		names := make([]string, 0, len(cfg.Relations))
		for name := range cfg.Relations {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(&b, "  relation %s = %s\n", name, formatRewrite(cfg.Relations[name], precUnion))
		}
		b.WriteString("}\n")
	}
	return []byte(b.String())
}

// --- lexer ----------------------------------------------------------------

type tokenKind uint8

const (
	tokIdent tokenKind = iota
	tokLBrace
	tokRBrace
	tokLParen
	tokRParen
	tokEq
	tokEOF
)

type token struct {
	kind tokenKind
	text string
	line int
}

func lex(s string) ([]token, error) {
	var toks []token
	line := 1
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '#':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '{':
			toks = append(toks, token{tokLBrace, "{", line})
			i++
		case c == '}':
			toks = append(toks, token{tokRBrace, "}", line})
			i++
		case c == '(':
			toks = append(toks, token{tokLParen, "(", line})
			i++
		case c == ')':
			toks = append(toks, token{tokRParen, ")", line})
			i++
		case c == '=':
			toks = append(toks, token{tokEq, "=", line})
			i++
		case isIdentByte(c):
			j := i
			for j < len(s) && isIdentByte(s[j]) {
				j++
			}
			toks = append(toks, token{tokIdent, s[i:j], line})
			i = j
		default:
			return nil, fmt.Errorf("line %d: unexpected character %q", line, string(c))
		}
	}
	toks = append(toks, token{tokEOF, "", line})
	return toks, nil
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '-' ||
		(c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z')
}

// --- parser ---------------------------------------------------------------

type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) parseFile() ([]zanzibar.NamespaceConfig, error) {
	var out []zanzibar.NamespaceConfig
	seen := map[string]bool{}
	for p.peek().kind != tokEOF {
		cfg, err := p.parseNamespace()
		if err != nil {
			return nil, err
		}
		if seen[cfg.Namespace] {
			return nil, fmt.Errorf("duplicate namespace %q", cfg.Namespace)
		}
		seen[cfg.Namespace] = true
		out = append(out, cfg)
	}
	return out, nil
}

func (p *parser) parseNamespace() (zanzibar.NamespaceConfig, error) {
	if err := p.expectKeyword("namespace"); err != nil {
		return zanzibar.NamespaceConfig{}, err
	}
	name, err := p.expectName()
	if err != nil {
		return zanzibar.NamespaceConfig{}, err
	}
	if t := p.next(); t.kind != tokLBrace {
		return zanzibar.NamespaceConfig{}, fmt.Errorf("line %d: expected '{' after namespace %q", t.line, name)
	}
	relations := map[string]zanzibar.Rewrite{}
	for p.peek().kind != tokRBrace {
		if p.peek().kind == tokEOF {
			return zanzibar.NamespaceConfig{}, fmt.Errorf("unexpected EOF: missing '}' for namespace %q", name)
		}
		if err := p.expectKeyword("relation"); err != nil {
			return zanzibar.NamespaceConfig{}, err
		}
		rname, err := p.expectName()
		if err != nil {
			return zanzibar.NamespaceConfig{}, err
		}
		if _, dup := relations[rname]; dup {
			return zanzibar.NamespaceConfig{}, fmt.Errorf("duplicate relation %q in namespace %q", rname, name)
		}
		var rw zanzibar.Rewrite
		if p.peek().kind == tokEq {
			p.next()
			rw, err = p.parseUnion()
			if err != nil {
				return zanzibar.NamespaceConfig{}, err
			}
		} else {
			rw = zanzibar.Rewrite{This: &zanzibar.This{}}
		}
		relations[rname] = rw
	}
	p.next() // consume '}'
	return zanzibar.NamespaceConfig{Namespace: name, Relations: relations}, nil
}

func (p *parser) parseUnion() (zanzibar.Rewrite, error) {
	first, err := p.parseInter()
	if err != nil {
		return zanzibar.Rewrite{}, err
	}
	children := []zanzibar.Rewrite{first}
	for p.isKeyword("or") {
		p.next()
		c, err := p.parseInter()
		if err != nil {
			return zanzibar.Rewrite{}, err
		}
		children = append(children, c)
	}
	if len(children) == 1 {
		return children[0], nil
	}
	return zanzibar.Rewrite{Union: &zanzibar.SetOperation{Children: children}}, nil
}

func (p *parser) parseInter() (zanzibar.Rewrite, error) {
	first, err := p.parseExcept()
	if err != nil {
		return zanzibar.Rewrite{}, err
	}
	children := []zanzibar.Rewrite{first}
	for p.isKeyword("and") {
		p.next()
		c, err := p.parseExcept()
		if err != nil {
			return zanzibar.Rewrite{}, err
		}
		children = append(children, c)
	}
	if len(children) == 1 {
		return children[0], nil
	}
	return zanzibar.Rewrite{Intersection: &zanzibar.SetOperation{Children: children}}, nil
}

func (p *parser) parseExcept() (zanzibar.Rewrite, error) {
	base, err := p.parsePrimary()
	if err != nil {
		return zanzibar.Rewrite{}, err
	}
	for p.isKeyword("except") {
		p.next()
		sub, err := p.parsePrimary()
		if err != nil {
			return zanzibar.Rewrite{}, err
		}
		b, s := base, sub
		base = zanzibar.Rewrite{Exclusion: &zanzibar.Exclusion{Base: &b, Subtract: &s}}
	}
	return base, nil
}

func (p *parser) parsePrimary() (zanzibar.Rewrite, error) {
	t := p.peek()
	switch {
	case t.kind == tokLParen:
		p.next()
		rw, err := p.parseUnion()
		if err != nil {
			return zanzibar.Rewrite{}, err
		}
		if c := p.next(); c.kind != tokRParen {
			return zanzibar.Rewrite{}, fmt.Errorf("line %d: expected ')'", c.line)
		}
		return rw, nil
	case t.kind == tokIdent && t.text == "_this":
		p.next()
		return zanzibar.Rewrite{This: &zanzibar.This{}}, nil
	case t.kind == tokIdent && !reserved[t.text]:
		p.next()
		if p.isKeyword("from") {
			p.next()
			tupleset, err := p.expectName()
			if err != nil {
				return zanzibar.Rewrite{}, err
			}
			return zanzibar.Rewrite{TupleToUserset: &zanzibar.TupleToUserset{Tupleset: tupleset, ComputedUserset: t.text}}, nil
		}
		return zanzibar.Rewrite{ComputedUserset: &zanzibar.ComputedUserset{Relation: t.text}}, nil
	default:
		return zanzibar.Rewrite{}, fmt.Errorf("line %d: expected a relation expression, got %q", t.line, tokenLabel(t))
	}
}

func (p *parser) isKeyword(kw string) bool {
	t := p.peek()
	return t.kind == tokIdent && t.text == kw
}

func (p *parser) expectKeyword(kw string) error {
	t := p.next()
	if t.kind != tokIdent || t.text != kw {
		return fmt.Errorf("line %d: expected %q, got %q", t.line, kw, tokenLabel(t))
	}
	return nil
}

func (p *parser) expectName() (string, error) {
	t := p.next()
	if t.kind != tokIdent {
		return "", fmt.Errorf("line %d: expected a name, got %q", t.line, tokenLabel(t))
	}
	if reserved[t.text] {
		return "", fmt.Errorf("line %d: %q is a reserved word and cannot be a name", t.line, t.text)
	}
	return t.text, nil
}

func tokenLabel(t token) string {
	if t.kind == tokEOF {
		return "EOF"
	}
	return t.text
}

// --- formatter ------------------------------------------------------------

const (
	precUnion = iota
	precInter
	precExcept
	precPrimary
)

func formatRewrite(rw zanzibar.Rewrite, parentPrec int) string {
	switch {
	case rw.This != nil:
		return "_this"
	case rw.ComputedUserset != nil:
		return rw.ComputedUserset.Relation
	case rw.TupleToUserset != nil:
		return rw.TupleToUserset.ComputedUserset + " from " + rw.TupleToUserset.Tupleset
	case rw.Union != nil:
		return joinChildren(rw.Union.Children, " or ", precUnion, parentPrec)
	case rw.Intersection != nil:
		return joinChildren(rw.Intersection.Children, " and ", precInter, parentPrec)
	case rw.Exclusion != nil:
		var base, sub zanzibar.Rewrite
		if rw.Exclusion.Base != nil {
			base = *rw.Exclusion.Base
		}
		if rw.Exclusion.Subtract != nil {
			sub = *rw.Exclusion.Subtract
		}
		s := formatRewrite(base, precExcept) + " except " + formatRewrite(sub, precExcept)
		return wrapIf(precExcept < parentPrec, s)
	default:
		return "_this"
	}
}

func joinChildren(children []zanzibar.Rewrite, sep string, prec, parentPrec int) string {
	parts := make([]string, len(children))
	for i, c := range children {
		parts[i] = formatRewrite(c, prec)
	}
	return wrapIf(prec < parentPrec, strings.Join(parts, sep))
}

func wrapIf(cond bool, s string) string {
	if cond {
		return "(" + s + ")"
	}
	return s
}
