package ichorgo

// reveal is the inverse of mask for text written about masked data (an AI answer about a
// masked report): fake hostnames and addresses are swapped back for the real ones, and the
// fake domain too when it stands for a single real one. Context names, extra words and
// several domains sharing the fake stay masked: there is no telling which was meant.
//
// It only reads right when the fakes cannot be mistaken for real text (see setAvoid).
func (m *privacyMask) reveal(s string) string {
	return m.unmaskFakes(s, true)
}

// unmaskFakes swaps fake hostnames and addresses back for the real ones and, withDomain,
// the fake domain when it stands for a single real one.
func (m *privacyMask) unmaskFakes(s string, withDomain bool) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled || s == "" {
		return s
	}

	terms := make([]maskTerm, 0, len(m.hostsBack)+1)
	for fake, real := range m.hostsBack {
		terms = append(terms, maskTerm{match: fake, repl: real})
	}

	if withDomain && len(m.domains) == 1 {
		for domain := range m.domains {
			terms = append(terms, maskTerm{match: maskedDomain, repl: domain})
		}
	}

	sortTerms(terms)

	back := func(fake string) (string, bool) {
		real, ok := m.ipsBack[fake]

		return real, ok
	}

	return replaceIPv6(replaceIPv4(replaceTerms(s, terms), back), back)
}
