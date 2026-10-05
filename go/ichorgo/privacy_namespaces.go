package ichorgo

// privacyNamesMax bounds the pod and workload names learnNames keeps: past it, new ones are
// not learned (they still reveal through hostnames and addresses).
const privacyNamesMax = 50000

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

	m.learnLocked(m.namespacesBack, names, 0)
}

// learnNames is learnNamespaces for the names of pods and workloads handed to the app (an
// app's pods in the inventory, a row of a list), which it sends back to act on one.
func (m *privacyMask) learnNames(names []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled {
		return
	}

	if m.namesBack == nil {
		m.namesBack = map[string]string{}
	}

	m.learnLocked(m.namesBack, names, privacyNamesMax)
}

// learnLocked adds the masked form of each real name to back (fake -> real), up to limit
// entries (0: no bound).
func (m *privacyMask) learnLocked(back map[string]string, names []string, limit int) {
	for _, real := range names {
		if limit > 0 && len(back) >= limit {
			return
		}

		fake := m.maskLocked(real)
		if _, known := back[fake]; fake != real && !known {
			back[fake] = real
		}
	}
}

// revealNamespace is the real namespace for one the app sent: a masked one learned by
// learnNamespaces, else what reveal makes of it (hostnames, addresses).
func (m *privacyMask) revealNamespace(namespace string) string {
	return m.revealLearned(namespace, func(s *maskState) map[string]string { return s.namespacesBack })
}

// revealName is revealNamespace for a pod or workload name learned by learnNames.
func (m *privacyMask) revealName(name string) string {
	return m.revealLearned(name, func(s *maskState) map[string]string { return s.namesBack })
}

func (m *privacyMask) revealLearned(s string, back func(*maskState) map[string]string) string {
	m.mu.Lock()
	real, ok := back(&m.maskState)[s]
	enabled := m.enabled
	m.mu.Unlock()

	if ok && enabled {
		return real
	}

	return m.reveal(s)
}

// learnPodNames learns the namespaces and names of pods handed to the app.
func learnPodNames(pods []kubePod) {
	privacy.learnNamespaces(namespacesOf(pods, func(p kubePod) string { return p.Namespace }))
	privacy.learnNames(namespacesOf(pods, func(p kubePod) string { return p.Name }))
}

// learnWorkloadNames learns the namespaces and names of workloads handed to the app.
func learnWorkloadNames(workloads []kubeWorkload) {
	privacy.learnNamespaces(namespacesOf(workloads, func(w kubeWorkload) string { return w.Namespace }))
	privacy.learnNames(namespacesOf(workloads, func(w kubeWorkload) string { return w.Name }))
}

// namespacesOf are the distinct values of rows (namespaces, names), for learnNamespaces and learnNames.
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
