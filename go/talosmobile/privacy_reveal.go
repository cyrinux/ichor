package talosmobile

import (
	"cmp"
	"slices"
)

// reveal is the inverse of mask for text written about masked data (an AI answer about a
// masked report): fake hostnames and addresses are swapped back for the real ones, and the
// fake domain too when it stands for a single real one. Context names, extra words and
// several domains sharing the fake stay masked: there is no telling which was meant.
//
// It only reads right when the fakes cannot be mistaken for real text (see setAvoid).
func (m *privacyMask) reveal(s string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled || s == "" {
		return s
	}

	terms := make([]maskTerm, 0, len(m.hostsBack)+1)
	for fake, real := range m.hostsBack {
		terms = append(terms, maskTerm{match: fake, repl: real})
	}

	if len(m.domains) == 1 {
		for domain := range m.domains {
			terms = append(terms, maskTerm{match: maskedDomain, repl: domain})
		}
	}

	slices.SortFunc(terms, func(a, b maskTerm) int {
		return cmp.Or(cmp.Compare(len(b.match), len(a.match)), cmp.Compare(a.match, b.match))
	})

	back := func(fake string) (string, bool) {
		real, ok := m.ipsBack[fake]

		return real, ok
	}

	return replaceIPv6(replaceIPv4(replaceTerms(s, terms), back), back)
}
