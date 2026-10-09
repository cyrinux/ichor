package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/client"
)

// podPhases are the phases a pod list can be narrowed to (status.phase).
var podPhases = []string{"Pending", "Running", "Succeeded", "Failed", "Unknown"}

// KubeNodeName is the Kubernetes name of the Talos node node (its address), as its kubelet
// registered it (Talos' NodeStatus resource): what KubeNodePodsPage takes, the mapping
// KubeCordon and the maintenance plan use too. Reads Talos only.
func KubeNodeName(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isTalosDemoContext(configYAML, contextName) {
		return demoKubeNodeName(node)
	}

	return withNodeSession(configYAML, contextName, node, callTimeout, func(nodeCtx context.Context, s *session) (string, error) {
		return kubeNodeNameOf(nodeCtx, s.client)
	})
}

// kubeNodeNameOf is the name the node's kubelet registered with, from the Talos node ctx
// targets.
func kubeNodeNameOf(ctx context.Context, c *client.Client) (string, error) {
	state := fetchKubeNodeState(ctx, c)
	if state == nil || state.Name == "" {
		return "", errKubeNodeUnknown
	}

	return state.Name, nil
}

var errKubeNodeUnknown = errors.New("the node's Kubernetes name is unknown: is the kubelet running?")

// demoKubeNodeName is a demo node's hostname, the name its kubelet registered.
func demoKubeNodeName(node string) (string, error) {
	for _, n := range demoNodes() {
		if n.Node == node {
			return n.Hostname, nil
		}
	}

	return "", fmt.Errorf("unknown demo node %q", node)
}

// KubeNodePodsPage lists one page of the pods scheduled on the Kubernetes node nodeName, in every
// namespace (os:admin), with fieldSelector=spec.nodeName: the drill-down from a node, read in
// the API server's order. phase narrows them to a status.phase ("Running"), or to every phase
// but one ("!Succeeded"), "" for all. Answer, continueToken, limit, table: see KubePodsPage
// (remaining is always -1: the API server does not count with a selector).
func KubeNodePodsPage(configYAML, contextName, kubeServer, nodeName, phase, continueToken string, limit int, table bool) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	nodeName = privacy.reveal(strings.TrimSpace(nodeName))

	if !kubeNamePattern.MatchString(nodeName) || strings.Contains(nodeName, "..") {
		return "", fmt.Errorf("invalid Kubernetes node name %q", nodeName)
	}

	args, err := newPageArgs("", continueToken, limit)
	if err != nil {
		return "", err
	}

	fields, err := podFieldSelector("spec.nodeName="+nodeName, phase)
	if err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() kubePodPage {
			return demoSelectedPods(phase, func(p kubePod) bool { return p.Node == nodeName })
		},
		func(ctx context.Context, k *kubeClient) (kubePodPage, error) {
			page, err := listPodsPage(ctx, k, "", pageQuery{limit: args.limit, continueToken: args.continueToken, fieldSelector: fields, table: table})
			learnPodNames(page.Pods)

			return page, err
		})
}

// KubeWorkloadPodsPage lists one page of the pods of a Deployment, StatefulSet or DaemonSet
// (kind) (os:admin): those of its namespace its spec.selector matches, asked of the API server
// as a labelSelector (matchLabels and matchExpressions), in the server's order. A workload
// whose selector is empty, or cannot be written as a query, has none. phase, answer,
// continueToken, limit, table: see KubeNodePodsPage.
func KubeWorkloadPodsPage(configYAML, contextName, kubeServer, kind, namespace, name, phase, continueToken string, limit int, table bool) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.revealName(strings.TrimSpace(name))

	wk, err := findWorkloadKind(kind)
	if err != nil {
		return "", err
	}

	if err := validateKubeName("workload", namespace, name); err != nil {
		return "", err
	}

	args, err := newPageArgs(namespace, continueToken, limit)
	if err != nil {
		return "", err
	}

	fields, err := podFieldSelector("", phase)
	if err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() kubePodPage {
			return demoSelectedPods(phase, func(p kubePod) bool { return p.Namespace == namespace && ownedByWorkload(p.Owner, wk.kind, name) })
		},
		func(ctx context.Context, k *kubeClient) (kubePodPage, error) {
			q := pageQuery{limit: args.limit, continueToken: args.continueToken, fieldSelector: fields, table: table}

			page, err := listWorkloadPodsPage(ctx, k, wk, namespace, name, q)
			learnPodNames(page.Pods)

			return page, err
		})
}

func listWorkloadPodsPage(ctx context.Context, k *kubeClient, wk workloadKind, namespace, name string, q pageQuery) (kubePodPage, error) {
	var obj appsObject
	if err := k.get(ctx, appsPath(wk, namespace, name), &obj); err != nil {
		return kubePodPage{}, err
	}

	selector, ok := selectorQuery(obj.Spec.Selector)
	if !ok {
		return kubePodPage{Pods: []kubePod{}, pageCursor: completeCursor}, nil
	}

	q.labelSelector = selector

	return listPodsPage(ctx, k, namespace, q)
}

// podFieldSelector joins base (a field selector, "" for none) and the status.phase test of
// phase ("Running", "!Succeeded", "" for none).
func podFieldSelector(base, phase string) (string, error) {
	phase = strings.TrimSpace(phase)
	if phase == "" {
		return base, nil
	}

	op, value := "=", phase
	if rest, ok := strings.CutPrefix(phase, "!"); ok {
		op, value = "!=", rest
	}

	if !slices.Contains(podPhases, value) {
		return "", fmt.Errorf("unsupported pod phase %q (%s, or one of them after !)", phase, strings.Join(podPhases, ", "))
	}

	parts := []string{"status.phase" + op + value}
	if base != "" {
		parts = append([]string{base}, parts...)
	}

	return strings.Join(parts, ","), nil
}

// selectorQuery writes a workload's spec.selector as the labelSelector query that lists its
// pods: "app=web,tier in (a,b),!legacy". ok is false when it selects every pod (empty: never
// asked for) or holds what the query syntax cannot carry.
func selectorQuery(s labelSelector) (query string, ok bool) {
	parts := make([]string, 0, len(s.MatchLabels)+len(s.MatchExpressions))

	for key, value := range s.MatchLabels {
		if !selectorToken(key) || (value != "" && !selectorToken(value)) {
			return "", false
		}

		parts = append(parts, key+"="+value)
	}

	slices.Sort(parts)

	for _, e := range s.MatchExpressions {
		part, err := selectorExpression(e.Key, e.Operator, e.Values)
		if err != nil {
			return "", false
		}

		parts = append(parts, part)
	}

	if len(parts) == 0 {
		return "", false
	}

	return strings.Join(parts, ","), true
}

func selectorExpression(key, operator string, values []string) (string, error) {
	if !selectorToken(key) {
		return "", errors.New("bad key")
	}

	for _, v := range values {
		if v != "" && !selectorToken(v) {
			return "", errors.New("bad value")
		}
	}

	switch operator {
	case "In", "NotIn":
		if len(values) == 0 {
			return "", errors.New("no values")
		}

		return key + " " + strings.ToLower(operator) + " (" + strings.Join(values, ",") + ")", nil
	case "Exists":
		return key, nil
	case "DoesNotExist":
		return "!" + key, nil
	default:
		return "", fmt.Errorf("unknown operator %q", operator)
	}
}

// selectorToken tells a label key or value the query can carry as is: no separator, space,
// operator nor parenthesis.
func selectorToken(s string) bool {
	if s == "" || len(s) > 316 {
		return false
	}

	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./", r)
		if !ok {
			return false
		}
	}

	return true
}

// ownedByWorkload tells a pod's owner ("ReplicaSet/web-5d8f") stands for the workload kind/name.
func ownedByWorkload(owner, kind, name string) bool {
	ref, ok := ownerWorkload("", owner)

	return ok && ref.kind.kind == kind && ref.name == name
}

// demoSelectedPods are the demo pods keep takes, narrowed to phase.
func demoSelectedPods(phase string, keep func(kubePod) bool) kubePodPage {
	pods := []kubePod{}

	for _, p := range demoPods() {
		if keep(p) && demoPhaseMatches(p, phase) {
			pods = append(pods, p)
		}
	}

	return kubePodPage{Pods: pods, pageCursor: completeCursor}
}

// demoPhaseMatches approximates a phase test on a demo pod's status.
func demoPhaseMatches(p kubePod, phase string) bool {
	if phase == "" {
		return true
	}

	actual := "Running"

	switch p.Status {
	case "Completed":
		actual = "Succeeded"
	case "Pending", "ContainerCreating":
		actual = "Pending"
	case "Failed", "Error":
		actual = "Failed"
	}

	if want, ok := strings.CutPrefix(phase, "!"); ok {
		return actual != want
	}

	return actual == phase
}
