package ichorgo

import (
	"slices"
	"strings"
)

// The labels Cilium adds to every endpoint, which Cilium policies can select on.
const (
	ciliumNamespaceLabel       = "io.kubernetes.pod.namespace"
	ciliumNamespaceLabelPrefix = "io.cilium.k8s.namespace.labels."
	ciliumServiceAccountLabel  = "io.cilium.k8s.policy.serviceaccount"
)

// labelSelector is a Kubernetes LabelSelector (also Cilium's EndpointSelector). A nil
// selector selects nothing, an empty one everything.
type labelSelector struct {
	MatchLabels      map[string]string `json:"matchLabels"`
	MatchExpressions []struct {
		Key      string   `json:"key"`
		Operator string   `json:"operator"`
		Values   []string `json:"values"`
	} `json:"matchExpressions"`
}

// labelSet is an endpoint's labels without source prefixes ("k8s:app=db" → app: db).
type labelSet map[string]string

// ciliumLabelKey strips a Cilium source prefix: "k8s:app" → "app", "any:app" → "app".
func ciliumLabelKey(key string) string {
	for _, prefix := range []string{"k8s:", "any:", "container:"} {
		if rest, ok := strings.CutPrefix(key, prefix); ok {
			return rest
		}
	}

	return key
}

// flowLabels reads a Hubble endpoint's labels; reserved:, cidr: and other non-pod sources
// are left out.
func flowLabels(labels []string) labelSet {
	out := labelSet{}

	for _, l := range labels {
		if !strings.HasPrefix(l, "k8s:") {
			continue
		}

		key, value, _ := strings.Cut(strings.TrimPrefix(l, "k8s:"), "=")
		out[key] = value
	}

	return out
}

// podLabelSet is a pod's labels plus what Cilium adds: its namespace, its namespace's
// labels and its service account.
func podLabelSet(labels map[string]string, namespace, serviceAccount string, nsLabels map[string]string) labelSet {
	out := make(labelSet, len(labels)+len(nsLabels)+2)
	for k, v := range labels {
		out[k] = v
	}

	for k, v := range nsLabels {
		out[ciliumNamespaceLabelPrefix+k] = v
	}

	out[ciliumNamespaceLabel] = namespace

	if serviceAccount != "" {
		out[ciliumServiceAccountLabel] = serviceAccount
	}

	return out
}

// matches tells whether s selects labels, keys compared without Cilium source prefixes.
func (s *labelSelector) matches(labels labelSet) bool {
	if s == nil {
		return false
	}

	for k, v := range s.MatchLabels {
		got, ok := labels[ciliumLabelKey(k)]
		if !ok || got != v {
			return false
		}
	}

	for _, e := range s.MatchExpressions {
		got, ok := labels[ciliumLabelKey(e.Key)]

		switch e.Operator {
		case "In":
			if !ok || !slices.Contains(e.Values, got) {
				return false
			}
		case "NotIn":
			if ok && slices.Contains(e.Values, got) {
				return false
			}
		case "Exists":
			if !ok {
				return false
			}
		case "DoesNotExist":
			if ok {
				return false
			}
		default:
			return false
		}
	}

	return true
}

// namespaceOf is the namespace a Cilium selector pins ("" when it does not).
func (s *labelSelector) namespaceOf() string {
	if s == nil {
		return ""
	}

	for k, v := range s.MatchLabels {
		if ciliumLabelKey(k) == ciliumNamespaceLabel {
			return v
		}
	}

	return ""
}

// String prints s the way kubectl does ("app=db,tier in (a,b)"), without Cilium prefixes
// and without the namespace label, which the app shows apart; "" for an empty selector.
func (s *labelSelector) String() string {
	if s == nil {
		return ""
	}

	var parts []string

	for k, v := range s.MatchLabels {
		if key := ciliumLabelKey(k); key != ciliumNamespaceLabel {
			parts = append(parts, key+"="+v)
		}
	}

	slices.Sort(parts)

	for _, e := range s.MatchExpressions {
		key := ciliumLabelKey(e.Key)

		switch e.Operator {
		case "In":
			parts = append(parts, key+" in ("+strings.Join(e.Values, ",")+")")
		case "NotIn":
			parts = append(parts, key+" notin ("+strings.Join(e.Values, ",")+")")
		case "Exists":
			parts = append(parts, key)
		case "DoesNotExist":
			parts = append(parts, "!"+key)
		}
	}

	return strings.Join(parts, ",")
}
