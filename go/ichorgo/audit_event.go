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
// get events`, the Argo CD or Lens timelines. Off unless SetAuditEvents(true). Best effort:
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
	auditEvents atomic.Bool
	// auditEventPosted is called once an Event was sent or given up (tests wait on it).
	auditEventPosted func()
)

// SetAuditEvents turns the Kubernetes Events on the objects the app changes on or off (off
// until called). The apps call it at start with the saved setting and when it changes.
func SetAuditEvents(enabled bool) { auditEvents.Store(enabled) }

// auditEventWanted says whether entry e gets a Kubernetes Event: the setting is on, the
// cluster is real, and the action touched a namespaced object ("Kind/name").
func auditEventWanted(e auditEntry) bool {
	kind, name, ok := strings.Cut(e.Object, "/")

	return auditEvents.Load() && !e.Demo && e.Namespace != "" && ok && kind != "" && name != ""
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

// auditEventMessage: "Ichor: scale replicas=3 (ok)", or "... failed: <error>".
func auditEventMessage(e auditEntry) string {
	var b strings.Builder

	b.WriteString("Ichor: ")
	b.WriteString(e.Action)

	if e.Params != "" {
		b.WriteString(" ")
		b.WriteString(e.Params)
	}

	if e.Outcome == auditFailed {
		b.WriteString(" failed")

		if e.Error != "" {
			b.WriteString(": ")
			b.WriteString(e.Error)
		}
	} else {
		b.WriteString(" (ok)")
	}

	return clipUTF8(b.String(), auditEventMessageMax)
}
