package ichorgo

// learnNamespaces remembers how the real namespaces names will read once masked, so that a
// namespace the app picks from a masked list (a namespace picker, a row) maps back to the
// real one: an extra word ("alice-apps" shown as "redacted-apps") cannot be revealed from the
// fake alone. Two namespaces masked alike keep the first one learned: there is no telling
// them apart on screen either.
func (m *privacyMask) learnNamespaces(names []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled {
		return
	}

	if m.namespacesBack == nil {
		m.namespacesBack = map[string]string{}
	}

	for _, real := range names {
		fake := m.maskLocked(real)
		if _, known := m.namespacesBack[fake]; fake != real && !known {
			m.namespacesBack[fake] = real
		}
	}
}

// revealNamespace is the real namespace for one the app sent: a masked one learned by
// learnNamespaces, else what reveal makes of it (hostnames, addresses).
func (m *privacyMask) revealNamespace(namespace string) string {
	m.mu.Lock()
	real, ok := m.namespacesBack[namespace]
	enabled := m.enabled
	m.mu.Unlock()

	if ok && enabled {
		return real
	}

	return m.reveal(namespace)
}

// namespacesOf are the namespaces of rows, for learnNamespaces.
func namespacesOf[T any](rows []T, of func(T) string) []string {
	seen := map[string]bool{}
	out := []string{}

	for _, r := range rows {
		if ns := of(r); !seen[ns] {
			seen[ns] = true
			out = append(out, ns)
		}
	}

	return out
}
