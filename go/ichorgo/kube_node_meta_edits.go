package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// The node taints and labels editor: the edits document, its local validation, and the
// diff of an edit against the node as the API server has it (kube_node_meta.go reads and
// patches the node).

// protectedNodeKeyPrefixes are the well-known label and taint prefixes the editor locks:
// Kubernetes, Talos and the cloud providers own them. Changing or removing one needs force.
var protectedNodeKeyPrefixes = []string{
	"kubernetes.io/", "k8s.io/", "node-role.kubernetes.io/", "node.kubernetes.io/",
	"topology.kubernetes.io/", "beta.kubernetes.io/", "talos.dev/", "eks.amazonaws.com/",
	"cloud.google.com/", "kubernetes.azure.com/", "karpenter.sh/",
}

var taintEffects = []string{"NoSchedule", "PreferNoSchedule", "NoExecute"}

const (
	taintNoExecute = "NoExecute"
	// nodeMetaChanged is the blocker, warning and error of an edit made on a stale read of
	// the node: the apps match it to offer a reload.
	nodeMetaChanged = "the node changed since it was read, reload"
)

var errNodeMetaChanged = errors.New(nodeMetaChanged)

// protectedNodeKey says whether a label or taint key is under a well-known prefix (or is
// the cordon taint, always protected).
func protectedNodeKey(key string) bool {
	if key == taintUnschedulable {
		return true
	}

	for _, p := range protectedNodeKeyPrefixes {
		// "kubernetes.io/" also covers "x.kubernetes.io/" sub-domains.
		if strings.HasPrefix(key, p) || strings.Contains(key, "."+p) {
			return true
		}
	}

	return false
}

// nodeMetaTaintEdit is one taint of the desired list.
type nodeMetaTaintEdit struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

func (t nodeMetaTaintEdit) id() string { return t.Key + "\x00" + t.Effect }

func (t nodeMetaTaintEdit) String() string {
	return strings.TrimSuffix(t.Key+"="+t.Value, "=") + ":" + t.Effect
}

// nodeMetaEdits is the edits document of KubeNodeMetaPlan and KubeNodeMetaApply. Labels are
// deltas (a stale copy never wipes cloud labels); Taints is the full desired list, nil when
// the edit leaves the taints alone. The cordon taint is never part of it.
type nodeMetaEdits struct {
	SetLabels    map[string]string    `json:"setLabels"`
	RemoveLabels []string             `json:"removeLabels"`
	Taints       *[]nodeMetaTaintEdit `json:"taints"`
	// ResourceVersion is the node's version the editor read (KubeNodeMeta): a node changed
	// since is refused. "" skips the check.
	ResourceVersion string `json:"resourceVersion"`
	// Force allows removing or changing protected labels and taints.
	Force bool `json:"force"`
}

var (
	dnsLabelRe   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	qualifiedRe  = regexp.MustCompile(`^[A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?$`)
	maxNameLen   = 63
	maxPrefixLen = 253
)

// parseNodeMetaEdits decodes and checks an edits document, before any request.
func parseNodeMetaEdits(editsJSON string) (nodeMetaEdits, error) {
	var e nodeMetaEdits

	if err := json.Unmarshal([]byte(editsJSON), &e); err != nil {
		return nodeMetaEdits{}, fmt.Errorf("bad edits: %w", err)
	}

	for key, value := range e.SetLabels {
		if err := checkQualifiedKey(key); err != nil {
			return nodeMetaEdits{}, fmt.Errorf("label %q: %w", key, err)
		}

		if err := checkLabelValue(value); err != nil {
			return nodeMetaEdits{}, fmt.Errorf("label %q: %w", key, err)
		}
	}

	for _, key := range e.RemoveLabels {
		if _, set := e.SetLabels[key]; set {
			return nodeMetaEdits{}, fmt.Errorf("label %q is both set and removed", key)
		}
	}

	if e.Taints != nil {
		if err := checkTaintEdits(*e.Taints); err != nil {
			return nodeMetaEdits{}, err
		}
	}

	return e, nil
}

func checkTaintEdits(taints []nodeMetaTaintEdit) error {
	seen := map[string]bool{}

	for _, t := range taints {
		if t.Key == taintUnschedulable {
			return fmt.Errorf("the %s taint is the cordon's: use Cordon / Uncordon", taintUnschedulable)
		}

		if err := checkQualifiedKey(t.Key); err != nil {
			return fmt.Errorf("taint %q: %w", t.Key, err)
		}

		if err := checkLabelValue(t.Value); err != nil {
			return fmt.Errorf("taint %q: %w", t.Key, err)
		}

		if !slices.Contains(taintEffects, t.Effect) {
			return fmt.Errorf("taint %q: effect %q is not one of %s", t.Key, t.Effect, strings.Join(taintEffects, ", "))
		}

		if seen[t.id()] {
			return fmt.Errorf("taint %q with effect %s is listed twice", t.Key, t.Effect)
		}

		seen[t.id()] = true
	}

	return nil
}

// checkQualifiedKey checks a label or taint key: an optional DNS subdomain prefix and "/",
// then a name of at most 63 characters.
func checkQualifiedKey(key string) error {
	prefix, name, hasPrefix := strings.Cut(key, "/")
	if !hasPrefix {
		name, prefix = prefix, ""
	}

	if hasPrefix {
		if prefix == "" || len(prefix) > maxPrefixLen {
			return fmt.Errorf("the prefix must be a DNS name of 1 to %d characters", maxPrefixLen)
		}

		for _, part := range strings.Split(prefix, ".") {
			if !dnsLabelRe.MatchString(part) {
				return errors.New("the prefix must be a lowercase DNS name")
			}
		}
	}

	if name == "" || len(name) > maxNameLen || !qualifiedRe.MatchString(name) {
		return fmt.Errorf("the name must be 1 to %d characters of letters, digits, '-', '_' or '.', starting and ending with a letter or digit", maxNameLen)
	}

	return nil
}

// checkLabelValue checks a label or taint value: empty, or at most 63 characters.
func checkLabelValue(value string) error {
	if value == "" {
		return nil
	}

	if len(value) > maxNameLen || !qualifiedRe.MatchString(value) {
		return fmt.Errorf("the value must be at most %d characters of letters, digits, '-', '_' or '.', starting and ending with a letter or digit", maxNameLen)
	}

	return nil
}

// nodeMetaObject is the part of a Node the editor reads and patches.
type nodeMetaObject struct {
	Metadata struct {
		Name            string            `json:"name"`
		ResourceVersion string            `json:"resourceVersion"`
		Labels          map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		Taints []nodeTaintObject `json:"taints"`
	} `json:"spec"`
}

// nodeTaintObject is a taint as the API server has it; TimeAdded is kept as sent back.
type nodeTaintObject struct {
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"`
	Effect    string `json:"effect"`
	TimeAdded string `json:"timeAdded,omitempty"`
}

func (t nodeTaintObject) edit() nodeMetaTaintEdit {
	return nodeMetaTaintEdit{Key: t.Key, Value: t.Value, Effect: t.Effect}
}

func (t nodeTaintObject) unixAdded() int64 {
	at, err := time.Parse(time.RFC3339, t.TimeAdded)
	if err != nil {
		return 0
	}

	return at.Unix()
}

type nodeMetaKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type nodeMetaLabelChange struct {
	Key  string `json:"key"`
	From string `json:"from"`
	To   string `json:"to"`
}

type nodeMetaTaintChange struct {
	Key    string `json:"key"`
	Effect string `json:"effect"`
	From   string `json:"from"`
	To     string `json:"to"`
}

type nodeMetaLabelDiff struct {
	Added   []nodeMetaKV          `json:"added"`
	Removed []nodeMetaKV          `json:"removed"`
	Changed []nodeMetaLabelChange `json:"changed"`
}

type nodeMetaTaintDiff struct {
	Added   []nodeMetaTaintEdit   `json:"added"`
	Removed []nodeMetaTaintEdit   `json:"removed"`
	Changed []nodeMetaTaintChange `json:"changed"`
}

// nodeMetaDiff is what an edit changes on a node, and the patch that does it.
type nodeMetaDiff struct {
	labels nodeMetaLabelDiff
	taints nodeMetaTaintDiff
	// patchLabels: the labels to set, nil values for the removed ones.
	patchLabels map[string]any
	// patchTaints is the node's full new taint list (the cordon taint kept), nil when the
	// taints do not change.
	patchTaints []nodeTaintObject
}

func (d nodeMetaDiff) empty() bool {
	return len(d.patchLabels) == 0 && d.patchTaints == nil
}

// diffNodeMeta compares an edit with the node.
func diffNodeMeta(node nodeMetaObject, e nodeMetaEdits) nodeMetaDiff {
	d := nodeMetaDiff{
		labels:      nodeMetaLabelDiff{Added: []nodeMetaKV{}, Removed: []nodeMetaKV{}, Changed: []nodeMetaLabelChange{}},
		taints:      nodeMetaTaintDiff{Added: []nodeMetaTaintEdit{}, Removed: []nodeMetaTaintEdit{}, Changed: []nodeMetaTaintChange{}},
		patchLabels: map[string]any{},
	}

	current := node.Metadata.Labels

	for _, key := range sortedStringKeys(e.SetLabels) {
		value := e.SetLabels[key]

		old, ok := current[key]

		switch {
		case !ok:
			d.labels.Added = append(d.labels.Added, nodeMetaKV{key, value})
		case old != value:
			d.labels.Changed = append(d.labels.Changed, nodeMetaLabelChange{key, old, value})
		default:
			continue
		}

		d.patchLabels[key] = value
	}

	removed := append([]string(nil), e.RemoveLabels...)
	sort.Strings(removed)
	removed = slices.Compact(removed)

	for _, key := range removed {
		if old, ok := current[key]; ok {
			d.labels.Removed = append(d.labels.Removed, nodeMetaKV{key, old})
			d.patchLabels[key] = nil
		}
	}

	if e.Taints != nil {
		diffTaints(&d, node.Spec.Taints, *e.Taints)
	}

	return d
}

// diffTaints compares the desired taints with the node's: same key and effect is the same
// taint (its value may change). The cordon taint stays as the server has it.
func diffTaints(d *nodeMetaDiff, current []nodeTaintObject, desired []nodeMetaTaintEdit) {
	byID := map[string]nodeTaintObject{}

	var cordon []nodeTaintObject

	for _, t := range current {
		if t.Key == taintUnschedulable {
			cordon = append(cordon, t)

			continue
		}

		byID[t.edit().id()] = t
	}

	wanted := map[string]bool{}
	next := append([]nodeTaintObject(nil), cordon...)
	changed := false

	for _, t := range desired {
		wanted[t.id()] = true
		old, ok := byID[t.id()]

		switch {
		case !ok:
			d.taints.Added = append(d.taints.Added, t)
			changed = true
		case old.Value != t.Value:
			d.taints.Changed = append(d.taints.Changed, nodeMetaTaintChange{t.Key, t.Effect, old.Value, t.Value})
			changed = true
		default:
			// Unchanged: sent back as it was, timeAdded included.
			next = append(next, old)

			continue
		}

		next = append(next, nodeTaintObject{Key: t.Key, Value: t.Value, Effect: t.Effect})
	}

	for _, t := range current {
		if t.Key != taintUnschedulable && !wanted[t.edit().id()] {
			d.taints.Removed = append(d.taints.Removed, t.edit())
			changed = true
		}
	}

	if changed {
		d.patchTaints = next
	}

	sortTaintEdits(d.taints.Added)
	sortTaintEdits(d.taints.Removed)
	sort.Slice(d.taints.Changed, func(i, j int) bool {
		return d.taints.Changed[i].Key+"\x00"+d.taints.Changed[i].Effect < d.taints.Changed[j].Key+"\x00"+d.taints.Changed[j].Effect
	})
}

func sortTaintEdits(list []nodeMetaTaintEdit) {
	sort.Slice(list, func(i, j int) bool { return list[i].id() < list[j].id() })
}

func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

// protectedChanges lists the protected labels and taints the diff removes or changes.
func (d nodeMetaDiff) protectedChanges() []string {
	var out []string

	for _, l := range d.labels.Removed {
		if protectedNodeKey(l.Key) {
			out = append(out, "removes the protected label "+l.Key)
		}
	}

	for _, l := range d.labels.Changed {
		if protectedNodeKey(l.Key) {
			out = append(out, "changes the protected label "+l.Key)
		}
	}

	for _, t := range d.taints.Removed {
		if protectedNodeKey(t.Key) {
			out = append(out, "removes the protected taint "+t.String())
		}
	}

	for _, t := range d.taints.Changed {
		if protectedNodeKey(t.Key) {
			out = append(out, "changes the protected taint "+t.Key+":"+t.Effect)
		}
	}

	return out
}

// noExecuteTaints are the new or changed NoExecute taints: they evict the pods that do not
// tolerate them, at once.
func (d nodeMetaDiff) noExecuteTaints() []nodeMetaTaintEdit {
	var out []nodeMetaTaintEdit

	for _, t := range d.taints.Added {
		if t.Effect == taintNoExecute {
			out = append(out, t)
		}
	}

	for _, c := range d.taints.Changed {
		if c.Effect == taintNoExecute {
			out = append(out, nodeMetaTaintEdit{Key: c.Key, Value: c.To, Effect: c.Effect})
		}
	}

	return out
}

// summary is the diff in one line, for the audit log.
func (d nodeMetaDiff) summary() string {
	var parts []string

	add := func(sign string, n int, noun, what string) {
		if n == 0 {
			return
		}

		if n > 1 {
			noun += "s"
		}

		parts = append(parts, fmt.Sprintf("%s%d %s (%s)", sign, n, noun, what))
	}

	label := func(list []nodeMetaKV) string {
		out := make([]string, len(list))
		for i, l := range list {
			out[i] = strings.TrimSuffix(l.Key+"="+l.Value, "=")
		}

		return strings.Join(out, ", ")
	}

	taint := func(list []nodeMetaTaintEdit) string {
		out := make([]string, len(list))
		for i, t := range list {
			out[i] = t.String()
		}

		return strings.Join(out, ", ")
	}

	changedLabels := make([]string, len(d.labels.Changed))
	for i, c := range d.labels.Changed {
		changedLabels[i] = c.Key + "=" + c.To
	}

	changedTaints := make([]string, len(d.taints.Changed))
	for i, c := range d.taints.Changed {
		changedTaints[i] = strings.TrimSuffix(c.Key+"="+c.To, "=") + ":" + c.Effect
	}

	add("+", len(d.labels.Added), "label", label(d.labels.Added))
	add("-", len(d.labels.Removed), "label", label(d.labels.Removed))
	add("~", len(d.labels.Changed), "label", strings.Join(changedLabels, ", "))
	add("+", len(d.taints.Added), "taint", taint(d.taints.Added))
	add("-", len(d.taints.Removed), "taint", taint(d.taints.Removed))
	add("~", len(d.taints.Changed), "taint", strings.Join(changedTaints, ", "))

	if len(parts) == 0 {
		return "no change"
	}

	return strings.Join(parts, ", ")
}

// podToleration is a pod's toleration of node taints.
type podToleration struct {
	Key      string `json:"key"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
	Effect   string `json:"effect"`
}

// tolerates follows the scheduler's rule: an empty effect matches every effect, an empty
// key with Exists every key, Exists every value, Equal (the default) the same value.
func (t podToleration) tolerates(taint nodeMetaTaintEdit) bool {
	if t.Effect != "" && t.Effect != taint.Effect {
		return false
	}

	if t.Key != "" && t.Key != taint.Key {
		return false
	}

	if t.Operator == "Exists" {
		return true
	}

	return t.Key != "" && t.Value == taint.Value
}

// evictedBy lists the pods the NoExecute taints evict: those without a matching toleration,
// but DaemonSet and static pods (they tolerate them, and come back).
func evictedBy(pods []drainPod, taints []nodeMetaTaintEdit) []nodeMetaEvict {
	out := []nodeMetaEvict{}

	if len(taints) == 0 {
		return out
	}

	for _, p := range pods {
		if p.Kind == drainDaemonSet || p.Kind == drainStatic {
			continue
		}

		for _, taint := range taints {
			if !tolerated(p.tolerations, taint) {
				out = append(out, nodeMetaEvict{Namespace: p.Namespace, Name: p.Name, Owner: p.Owner})

				break
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}

		return out[i].Name < out[j].Name
	})

	return out
}

func tolerated(tolerations []podToleration, taint nodeMetaTaintEdit) bool {
	for _, t := range tolerations {
		if t.tolerates(taint) {
			return true
		}
	}

	return false
}

type nodeMetaEvict struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Owner     string `json:"owner"`
}
