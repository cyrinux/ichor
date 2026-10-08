package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
)

// findEvent is a Warning event of the last hour, grouped: Extra is the object's kind,
// Reason and Message the event's own, Count how many times, Since when it was last seen.
const findEvent = "event"

const (
	// checkupEventWindow is how far back the checkup's events go.
	checkupEventWindow = time.Hour
	checkupMaxEvents   = 30
	// kubeEventsMax caps what KubeEvents returns, newest first.
	kubeEventsMax     = 100
	eventMessageLimit = 400
)

// kubeEvent is one Kubernetes event, as `kubectl describe` lists them.
type kubeEvent struct {
	Type      string `json:"type"` // Normal or Warning
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Count     int    `json:"count"`
	First     int64  `json:"first"`
	Last      int64  `json:"last"`
	Source    string `json:"source"`
}

type kubeEventList struct {
	Events []kubeEvent `json:"events"`
	// Forbidden: the credentials may not list events cluster-wide (KubeClusterEvents).
	Forbidden bool `json:"forbidden,omitempty"`
}

type eventObject struct {
	Metadata struct {
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	InvolvedObject struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"involvedObject"`
	Type           string     `json:"type"`
	Reason         string     `json:"reason"`
	Message        string     `json:"message"`
	Count          int        `json:"count"`
	FirstTimestamp *time.Time `json:"firstTimestamp"`
	LastTimestamp  *time.Time `json:"lastTimestamp"`
	EventTime      *time.Time `json:"eventTime"`
	Series         *struct {
		Count            int        `json:"count"`
		LastObservedTime *time.Time `json:"lastObservedTime"`
	} `json:"series"`
	Source struct {
		Component string `json:"component"`
	} `json:"source"`
	ReportingComponent string `json:"reportingComponent"`
}

func mapEvent(e eventObject) kubeEvent {
	first, last := e.Metadata.CreationTimestamp, e.Metadata.CreationTimestamp
	count := max(e.Count, 1)

	if e.EventTime != nil && !e.EventTime.IsZero() {
		first, last = *e.EventTime, *e.EventTime
	}

	if e.FirstTimestamp != nil && !e.FirstTimestamp.IsZero() {
		first = *e.FirstTimestamp
	}

	if e.LastTimestamp != nil && e.LastTimestamp.After(last) {
		last = *e.LastTimestamp
	}

	if e.Series != nil {
		count = max(count, e.Series.Count)

		if e.Series.LastObservedTime != nil && e.Series.LastObservedTime.After(last) {
			last = *e.Series.LastObservedTime
		}
	}

	message := strings.TrimSpace(e.Message)
	if len(message) > eventMessageLimit {
		message = strings.ToValidUTF8(message[:eventMessageLimit], "") + "…"
	}

	return kubeEvent{
		Type: e.Type, Reason: e.Reason, Message: message,
		Kind: e.InvolvedObject.Kind, Namespace: e.InvolvedObject.Namespace, Name: e.InvolvedObject.Name,
		Count: count, First: milli(first), Last: milli(last),
		Source: cmp.Or(e.Source.Component, e.ReportingComponent),
	}
}

// KubeEvents lists the events of one object, newest first, like the end of `kubectl
// describe` (os:admin): {"events":[{type,reason,message,kind,namespace,name,count,first,
// last,source}]}. With a kind ("Pod", "Deployment") it is that object's; with kind "" it is
// name's and those of what it owns by name (a Deployment's ReplicaSets and pods), and with
// name "" too every event of the namespace. A cluster-scoped object (kind "Node") takes
// namespace "" with its kind and name. kubeServer: see KubePods.
func KubeEvents(configYAML, contextName, kubeServer, namespace, kind, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	kind = strings.TrimSpace(kind)

	if err := validateEventTarget(namespace, kind, name); err != nil {
		return "", err
	}

	demo := func() kubeEventList { return demoEvents(namespace, kind, name, time.Now()) }

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demo, func(ctx context.Context, k *kubeClient) (kubeEventList, error) {
		return readEvents(ctx, k, namespace, kind, name)
	})
}

// validateEventTarget refuses what cannot name a namespace, a kind or an object: they go
// into a path and a field selector. No namespace is only for a cluster-scoped object, named
// by its kind and name.
func validateEventTarget(namespace, kind, name string) error {
	if namespace == "" && (kind == "" || name == "") {
		return errors.New("events need a namespace, or the kind and name of a cluster-scoped object")
	}

	if namespace != "" {
		if err := validateNamespace(namespace); err != nil {
			return err
		}
	}

	if strings.ContainsFunc(kind, func(r rune) bool { return !unicode.IsLetter(r) }) {
		return fmt.Errorf("invalid kind %q", kind)
	}

	if name != "" && !kubeNamePattern.MatchString(name) {
		return fmt.Errorf("invalid name %q", name)
	}

	return nil
}

func readEvents(ctx context.Context, k *kubeClient, namespace, kind, name string) (kubeEventList, error) {
	path := scopedPath("/api/v1", namespace, "events")

	if kind != "" && name != "" {
		path += "?fieldSelector=" + url.QueryEscape("involvedObject.kind="+kind+",involvedObject.name="+name)
	}

	objs, err := listObjects[eventObject](ctx, k, path)
	if err != nil {
		return kubeEventList{}, err
	}

	events := []kubeEvent{}

	for _, obj := range objs {
		e := mapEvent(obj)
		if kind == "" && name != "" && e.Name != name && !strings.HasPrefix(e.Name, name+"-") {
			continue
		}

		events = append(events, e)
	}

	return kubeEventList{Events: newestEvents(events, kubeEventsMax)}, nil
}

// KubeClusterEvents lists the events of every namespace and of the cluster-scoped objects
// (nodes), newest first, as KubeEvents' JSON (os:admin): the Kubernetes counterpart of the
// Talos events. warningsOnly leaves out the Normal ones; limit caps the list (0, or more than
// kubeEventsMax, is kubeEventsMax). Credentials that may not list events cluster-wide get
// forbidden instead of an error. kubeServer: see KubePods.
func KubeClusterEvents(configYAML, contextName, kubeServer string, warningsOnly bool, limit int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	limit = clampEventLimit(limit)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() kubeEventList { return demoClusterEvents(warningsOnly, limit, time.Now()) },
		func(ctx context.Context, k *kubeClient) (kubeEventList, error) {
			list, err := readClusterEvents(ctx, k, warningsOnly, limit)
			learnEventNames(list.Events)

			return list, err
		})
}

func clampEventLimit(limit int) int {
	if limit <= 0 || limit > kubeEventsMax {
		return kubeEventsMax
	}

	return limit
}

func readClusterEvents(ctx context.Context, k *kubeClient, warningsOnly bool, limit int) (kubeEventList, error) {
	path := "/api/v1/events"
	if warningsOnly {
		path += "?fieldSelector=" + url.QueryEscape("type!=Normal")
	}

	objs, err := listObjects[eventObject](ctx, k, path)
	if isForbidden(err) {
		return kubeEventList{Events: []kubeEvent{}, Forbidden: true}, nil
	}

	if err != nil {
		return kubeEventList{}, err
	}

	events := make([]kubeEvent, 0, len(objs))
	for _, obj := range objs {
		events = append(events, mapEvent(obj))
	}

	return kubeEventList{Events: newestEvents(events, limit)}, nil
}

// newestEvents sorts events newest first and keeps limit of them.
func newestEvents(events []kubeEvent, limit int) []kubeEvent {
	slices.SortStableFunc(events, func(a, b kubeEvent) int { return cmp.Compare(b.Last, a.Last) })

	if len(events) > limit {
		events = events[:limit]
	}

	return events
}

// eventsSince keeps the events last seen at since (Unix ms) or after.
func eventsSince(events []kubeEvent, since int64) []kubeEvent {
	return slices.DeleteFunc(slices.Clone(events), func(e kubeEvent) bool { return e.Last < since })
}

// learnEventNames teaches the mask the namespaces and object names events talk about, which
// the app may not have seen yet.
func learnEventNames(events []kubeEvent) {
	notEmpty := func(names []string) []string { return slices.DeleteFunc(names, func(s string) bool { return s == "" }) }

	privacy.learnNamespaces(notEmpty(namespacesOf(events, func(e kubeEvent) string { return e.Namespace })))
	privacy.learnNames(notEmpty(namespacesOf(events, func(e kubeEvent) string { return e.Name })))
}

// checkupEvents groups the Warning events of the last hour by object and reason, the most
// recent first. They never make the verdict worse: an event is a symptom the other
// sections name.
func checkupEvents(ctx context.Context, k *kubeClient, in checkupInput) checkupSection {
	objs, err := listObjects[eventObject](ctx, k, "/api/v1/events?fieldSelector="+url.QueryEscape("type!=Normal"))
	if err != nil {
		return newSection(checkEvents, 0, nil, err)
	}

	return newSection(checkEvents, len(objs), eventFindings(objs, in.now))
}

func eventFindings(objs []eventObject, now time.Time) []checkupFinding {
	since := now.Add(-checkupEventWindow).UnixMilli()
	groups := map[string]*checkupFinding{}
	order := []*checkupFinding{}

	for _, obj := range objs {
		e := mapEvent(obj)
		if e.Last < since {
			continue
		}

		key := promKey(e.Kind, e.Namespace, e.Name, e.Reason)

		f := groups[key]
		if f == nil {
			f = &checkupFinding{Kind: findEvent, Severity: sevInfo, Namespace: e.Namespace, Name: e.Name, Extra: e.Kind, Reason: e.Reason}
			groups[key] = f
			order = append(order, f)
		}

		f.Count += e.Count

		if e.Last >= f.Since {
			f.Since, f.Message = e.Last, e.Message
		}
	}

	slices.SortStableFunc(order, func(a, b *checkupFinding) int { return cmp.Compare(b.Since, a.Since) })

	findings := make([]checkupFinding, 0, min(len(order), checkupMaxEvents))

	for _, f := range order {
		if len(findings) == checkupMaxEvents {
			break
		}

		findings = append(findings, *f)
	}

	return findings
}
