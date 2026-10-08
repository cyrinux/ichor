package ichorgo

import (
	"slices"
	"strings"
	"unicode"
)

// Calico's selector language, the one its policies name endpoints and namespaces with:
//
//	all()                      global()
//	app == 'db'                app != 'db'
//	has(app)                   !has(app)
//	app in {'a', 'b'}          app not in {'a', 'b'}
//	app contains 'd'           app starts with 'd'    app ends with 'b'
//	!e    e1 && e2    e1 || e2    (e)
//
// The app keeps the expression as written (it is what the user wrote and reads) and
// evaluates it against a pod's labels, the same way Felix does.

// The labels Calico adds to every workload endpoint, which its selectors can use.
const (
	calicoNamespaceLabel      = "projectcalico.org/namespace"
	calicoOrchestratorLabel   = "projectcalico.org/orchestrator"
	calicoServiceAccountLabel = "projectcalico.org/serviceaccount"
	// calicoNameLabel is the name of a namespace (or a service account) for a selector on it.
	calicoNameLabel = "projectcalico.org/name"
)

// calicoSelector is a parsed selector. A nil selector selects nothing; an empty expression
// everything (all()).
type calicoSelector struct {
	src  string
	expr calicoExpr
}

// calicoExpr is one node of a parsed selector.
type calicoExpr interface {
	matches(labelSet) bool
}

type (
	calicoAll    struct{}
	calicoGlobal struct{}
	calicoNot    struct{ e calicoExpr }
	calicoAnd    struct{ l, r calicoExpr }
	calicoOr     struct{ l, r calicoExpr }
	calicoHas    struct{ key string }
	calicoCmp    struct {
		key, op string // ==, !=, contains, starts, ends
		value   string
	}
	calicoIn struct {
		key    string
		values []string
		not    bool
	}
)

func (calicoAll) matches(labelSet) bool { return true }

// global() names the endpoints of no namespace (host endpoints, global network sets): never a pod.
func (calicoGlobal) matches(labelSet) bool  { return false }
func (n calicoNot) matches(l labelSet) bool { return !n.e.matches(l) }
func (a calicoAnd) matches(l labelSet) bool { return a.l.matches(l) && a.r.matches(l) }
func (o calicoOr) matches(l labelSet) bool  { return o.l.matches(l) || o.r.matches(l) }

func (h calicoHas) matches(l labelSet) bool {
	_, ok := l[h.key]

	return ok
}

func (c calicoCmp) matches(l labelSet) bool {
	got, ok := l[c.key]

	switch c.op {
	case "==":
		return ok && got == c.value
	case "!=":
		return !ok || got != c.value
	case "contains":
		return ok && strings.Contains(got, c.value)
	case "starts":
		return ok && strings.HasPrefix(got, c.value)
	case "ends":
		return ok && strings.HasSuffix(got, c.value)
	}

	return false
}

func (i calicoIn) matches(l labelSet) bool {
	got, ok := l[i.key]
	in := ok && slices.Contains(i.values, got)

	if i.not {
		// "not in" matches an endpoint without the label too (Calico's semantics).
		return !in
	}

	return in
}

// parseCalicoSelector parses src. An expression Calico would refuse selects nothing, so a
// policy the app cannot read never claims pods it may not select.
func parseCalicoSelector(src string) *calicoSelector {
	src = strings.TrimSpace(src)
	if src == "" {
		return &calicoSelector{expr: calicoAll{}}
	}

	p := &calicoParser{tokens: tokenizeCalico(src)}
	expr := p.parseOr()

	if p.failed || p.pos != len(p.tokens) {
		expr = nil
	}

	return &calicoSelector{src: src, expr: expr}
}

func (s *calicoSelector) matches(labels labelSet) bool {
	return s != nil && s.expr != nil && s.expr.matches(labels)
}

// String is the expression as written; "" for all().
func (s *calicoSelector) String() string {
	if s == nil {
		return ""
	}

	return s.src
}

// rekey returns s with every label key mapped through key, so a selector on a namespace's
// or a service account's labels can be evaluated on the pod's label set.
func (s *calicoSelector) rekey(key func(string) string) *calicoSelector {
	if s == nil || s.expr == nil {
		return s
	}

	return &calicoSelector{src: s.src, expr: rekeyExpr(s.expr, key)}
}

func rekeyExpr(e calicoExpr, key func(string) string) calicoExpr {
	switch n := e.(type) {
	case calicoNot:
		return calicoNot{rekeyExpr(n.e, key)}
	case calicoAnd:
		return calicoAnd{rekeyExpr(n.l, key), rekeyExpr(n.r, key)}
	case calicoOr:
		return calicoOr{rekeyExpr(n.l, key), rekeyExpr(n.r, key)}
	case calicoHas:
		return calicoHas{key(n.key)}
	case calicoCmp:
		n.key = key(n.key)

		return n
	case calicoIn:
		n.key = key(n.key)

		return n
	}

	return e
}

// namespaceLabelKey maps a namespace selector's key to the pod's label set: the namespace's
// name is the namespace label, its other labels carry the namespace prefix.
func namespaceLabelKey(key string) string {
	if key == calicoNameLabel {
		return ciliumNamespaceLabel
	}

	return ciliumNamespaceLabelPrefix + key
}

// serviceAccountLabelKey maps a service account selector's key: only the account's name is
// known from the pod; its other labels are not read, so they never match.
func serviceAccountLabelKey(key string) string {
	if key == calicoNameLabel {
		return calicoServiceAccountLabel
	}

	return "\x00serviceaccount:" + key
}

// isGlobal tells whether s is global(): the endpoints of no namespace.
func (s *calicoSelector) isGlobal() bool {
	if s == nil {
		return false
	}

	_, ok := s.expr.(calicoGlobal)

	return ok
}

// pins is the value s requires key to have, when the whole expression is a conjunction
// including "key == 'value'"; "" otherwise. It tells the namespace a selector pins.
func (s *calicoSelector) pins(key string) string {
	if s == nil || s.expr == nil {
		return ""
	}

	return pinnedValue(s.expr, key)
}

func pinnedValue(e calicoExpr, key string) string {
	switch n := e.(type) {
	case calicoCmp:
		if n.op == "==" && n.key == key {
			return n.value
		}
	case calicoAnd:
		if v := pinnedValue(n.l, key); v != "" {
			return v
		}

		return pinnedValue(n.r, key)
	}

	return ""
}

// --- tokens ---

type calicoToken struct {
	kind  byte // 'i' identifier/keyword, 's' string, 'p' punctuation/operator
	value string
}

func tokenizeCalico(src string) []calicoToken {
	var out []calicoToken

	for i := 0; i < len(src); {
		c := src[i]

		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '\'' || c == '"':
			end := strings.IndexByte(src[i+1:], c)
			if end < 0 {
				return append(out, calicoToken{kind: '?'})
			}

			out = append(out, calicoToken{kind: 's', value: src[i+1 : i+1+end]})
			i += end + 2
		case strings.HasPrefix(src[i:], "==") || strings.HasPrefix(src[i:], "!=") ||
			strings.HasPrefix(src[i:], "&&") || strings.HasPrefix(src[i:], "||"):
			out = append(out, calicoToken{kind: 'p', value: src[i : i+2]})
			i += 2
		case strings.IndexByte("(){},!", c) >= 0:
			out = append(out, calicoToken{kind: 'p', value: string(c)})
			i++
		case isCalicoKeyChar(rune(c)):
			start := i
			for i < len(src) && isCalicoKeyChar(rune(src[i])) {
				i++
			}

			out = append(out, calicoToken{kind: 'i', value: src[start:i]})
		default:
			return append(out, calicoToken{kind: '?'})
		}
	}

	return out
}

func isCalicoKeyChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' || r == '/'
}

// --- parser ---

type calicoParser struct {
	tokens []calicoToken
	pos    int
	failed bool
}

func (p *calicoParser) peek() calicoToken {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}

	return calicoToken{}
}

func (p *calicoParser) accept(kind byte, value string) bool {
	t := p.peek()
	if t.kind == kind && t.value == value {
		p.pos++

		return true
	}

	return false
}

func (p *calicoParser) expect(kind byte, value string) {
	if !p.accept(kind, value) {
		p.failed = true
	}
}

func (p *calicoParser) parseOr() calicoExpr {
	left := p.parseAnd()
	for p.accept('p', "||") {
		left = calicoOr{left, p.parseAnd()}
	}

	return left
}

func (p *calicoParser) parseAnd() calicoExpr {
	left := p.parseUnary()
	for p.accept('p', "&&") {
		left = calicoAnd{left, p.parseUnary()}
	}

	return left
}

func (p *calicoParser) parseUnary() calicoExpr {
	switch {
	case p.accept('p', "!"):
		return calicoNot{p.parseUnary()}
	case p.accept('p', "("):
		e := p.parseOr()
		p.expect('p', ")")

		return e
	}

	return p.parsePrimary()
}

func (p *calicoParser) parsePrimary() calicoExpr {
	t := p.peek()
	if t.kind != 'i' {
		p.failed = true

		return nil
	}

	p.pos++

	switch {
	case t.value == "all" && p.accept('p', "("):
		p.expect('p', ")")

		return calicoAll{}
	case t.value == "global" && p.accept('p', "("):
		p.expect('p', ")")

		return calicoGlobal{}
	case t.value == "has" && p.accept('p', "("):
		key := p.peek()
		p.expect('i', key.value)
		p.expect('p', ")")

		return calicoHas{key.value}
	}

	return p.parseComparison(t.value)
}

func (p *calicoParser) parseComparison(key string) calicoExpr {
	switch {
	case p.accept('p', "==") || p.accept('p', "!="):
		op := p.tokens[p.pos-1].value

		return calicoCmp{key: key, op: op, value: p.parseString()}
	case p.accept('i', "in"):
		return calicoIn{key: key, values: p.parseSet()}
	case p.accept('i', "not"):
		p.expect('i', "in")

		return calicoIn{key: key, values: p.parseSet(), not: true}
	case p.accept('i', "contains"):
		return calicoCmp{key: key, op: "contains", value: p.parseString()}
	case p.accept('i', "starts") || p.accept('i', "ends"):
		op := p.tokens[p.pos-1].value
		p.expect('i', "with")

		return calicoCmp{key: key, op: op, value: p.parseString()}
	}

	p.failed = true

	return nil
}

func (p *calicoParser) parseString() string {
	t := p.peek()
	if t.kind != 's' {
		p.failed = true

		return ""
	}

	p.pos++

	return t.value
}

func (p *calicoParser) parseSet() []string {
	p.expect('p', "{")

	var values []string

	for !p.failed && !p.accept('p', "}") {
		values = append(values, p.parseString())

		if !p.accept('p', ",") {
			p.expect('p', "}")

			break
		}
	}

	return values
}
