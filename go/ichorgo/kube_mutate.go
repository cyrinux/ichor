package ichorgo

import (
	"context"
	"strings"
)

// namedMutation is an action on one named object: what the audit log records and what
// validateKubeName calls the object when the name is missing.
type namedMutation struct {
	action string // audit action, a contract with the apps' Activity screen: never rename
	kind   string // audit object kind, "Schedule" in "Schedule/<name>"
	label  string // the object in validateKubeName's message
	// learnedNamespace reveals the namespace from the names learned by the privacy mask
	// (privacy.revealNamespace) instead of the fake hostnames and addresses.
	learnedNamespace bool
}

// kubeNamedMutation is the shared prologue of an action on namespace/name: the result masked,
// the context and names revealed, the action audited and the names validated, then fn run
// through kubeMutate with the revealed names. Returns what fn returns.
func kubeNamedMutation(target kubeTarget, m namedMutation, namespace, name string, fn func(ctx context.Context, k *kubeClient, namespace, name string) (string, error)) (out string, err error) {
	defer maskResult(&out, &err)

	target.context = unmaskContext(target.config, target.context)

	namespace, name = strings.TrimSpace(namespace), privacy.reveal(strings.TrimSpace(name))
	if m.learnedNamespace {
		namespace = privacy.revealNamespace(namespace)
	} else {
		namespace = privacy.reveal(namespace)
	}

	defer recordAction(&err, target.config, target.context, auditAction{Server: target.server, Action: m.action, Namespace: namespace, Object: m.kind + "/" + name})

	if err := validateKubeName(m.label, namespace, name); err != nil {
		return "", err
	}

	err = kubeMutate(target, func(ctx context.Context, k *kubeClient) error {
		out, err = fn(ctx, k, namespace, name)

		return err
	})

	return out, err
}
