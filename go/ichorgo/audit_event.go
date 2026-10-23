package ichorgo

import (
	"context"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Optionally, each Kubernetes object the app changes also gets a Kubernetes Event (reason
// IchorAction) telling what the phone did, so the cluster's own history shows it: `kubectl
// get events`, the Argo CD or Lens timelines. Per cluster: only on the clusters
// SetAuditEventClusters names, off everywhere else (and until called). Best effort:
// an Event that cannot be written (RBAC on events, namespace gone) never fails the action and
// is not retried. Only actions on a namespaced Kubernetes object get one: node reboots and
// Talos changes have no object to attach it to.

const (
	auditEventReason    = "IchorAction"
	auditEventComponent = "ichor"
	auditEventTimeout   = 10 * time.Second
	// auditEventMessageMax bounds the message: the API server accepts 1 KiB.
	auditEventMessageMax = 1000
)

var (
	// auditEventClusters holds the fingerprints of the clusters that record actions. The set
	// is never changed in place: SetAuditEventClusters swaps in a new one.
	auditEventClusters atomic.Pointer[map[string]struct{}]
	// auditEventPosted is called once an Event was sent or given up (tests wait on it).
	auditEventPosted func()
)

// SetAuditEventClusters names the clusters whose changed objects get a Kubernetes Event: their
// context fingerprints (ContextSummary.fingerprint, every context of the cluster), one per
// line; "" turns the Events off everywhere. The apps call it at start with the saved choice
// and after each change, with the whole set.
func SetAuditEventClusters(fingerprints string) {
	set := map[string]struct{}{}

	for _, f := range strings.Split(fingerprints, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			set[f] = struct{}{}
		}
	}

	auditEventClusters.Store(&set)
}

// auditEventsSomewhere says whether at least one cluster records actions.
func auditEventsSomewhere() bool {
	set := auditEventClusters.Load()

	return set != nil && len(*set) > 0
}

// auditEventWanted says whether entry e, on the cluster with that context fingerprint, gets a
// Kubernetes Event: the cluster records actions, it is real, and the action touched a
// namespaced object ("Kind/name").
func auditEventWanted(e auditEntry, fingerprint string) bool {
	kind, name, ok := strings.Cut(e.Object, "/")
	if e.Demo || e.Namespace == "" || !ok || kind == "" || name == "" || fingerprint == "" {
		return false
	}

	set := auditEventClusters.Load()
	if set == nil {
		return false
	}

	_, on := (*set)[fingerprint]

	return on
}

// auditClusterFingerprint is the fingerprint of the context an action ran on, as the apps key
// their per-cluster settings: a Talos context's (contextFingerprint) or a kubeconfig context's
// (kubeFingerprint), told apart by the config, never by the name alone. contextName is the
// unmasked name; "" is the config's current context. "" when the context is unknown.
func auditClusterFingerprint(configYAML, contextName string) string {
	if configYAML == "" {
		return ""
	}

	if isKubeconfig(configYAML) {
		doc, err := loadKubeconfigDoc(configYAML)
		if err != nil {
			return ""
		}

		if contextName == "" {
			contextName = doc.currentName()
		}

		ctx, ok := doc.context(contextName)
		if !ok {
			return ""
		}

		cluster, _ := doc.cluster(ctx.Context.Cluster)

		return kubeFingerprint(contextName, cluster)
	}

	name, ctx, err := resolveContext(configYAML, contextName)
	if err != nil {
		return ""
	}

	return contextFingerprint(name, ctx)
}

// postAuditEvent writes the Event for e in the background, on the cluster the action ran on.
func postAuditEvent(target kubeTarget, e auditEntry) {
	go func() {
		defer func() {
			_ = recover() // best effort: a failure here must not reach the app

			if auditEventPosted != nil {
				auditEventPosted()
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), auditEventTimeout)
		defer cancel()

		path := "/api/v1/namespaces/" + url.PathEscape(e.Namespace) + "/events"
		body := auditEventBody(e, auditNow())

		_ = kubeDo(ctx, target, func(ctx context.Context, k *kubeClient) error {
			return k.post(ctx, path, body, nil)
		})
	}()
}

// auditEventBody is the core/v1 Event for e: Normal, from component ichor, on the object the
// action targeted. The message carries the action, its parameters (already redacted) and the
// outcome; nothing names the cluster, the phone or the user.
func auditEventBody(e auditEntry, now time.Time) map[string]any {
	kind, name, _ := strings.Cut(e.Object, "/")
	stamp := now.UTC().Format(time.RFC3339)

	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Event",
		"metadata": map[string]any{
			"generateName": "ichor-",
			"namespace":    e.Namespace,
		},
		"involvedObject": map[string]any{
			"kind":      kind,
			"namespace": e.Namespace,
			"name":      name,
		},
		"reason":             auditEventReason,
		"action":             e.Action,
		"message":            auditEventMessage(e),
		"type":               "Normal",
		"source":             map[string]any{"component": auditEventComponent},
		"reportingComponent": auditEventComponent,
		"firstTimestamp":     stamp,
		"lastTimestamp":      stamp,
		"count":              1,
	}
}

// auditEventMessage: "Ichor: scale replicas=3 (ok)", or "... failed: <error>" (or partial).
func auditEventMessage(e auditEntry) string {
	var b strings.Builder

	b.WriteString("Ichor: ")
	b.WriteString(e.Action)

	if e.Params != "" {
		b.WriteString(" ")
		b.WriteString(e.Params)
	}

	if e.Outcome == auditFailed || e.Outcome == auditPartial {
		b.WriteString(" " + e.Outcome)

		if e.Error != "" {
			b.WriteString(": ")
			b.WriteString(e.Error)
		}
	} else {
		b.WriteString(" (ok)")
	}

	return clipUTF8(b.String(), auditEventMessageMax)
}
