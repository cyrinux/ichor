package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// The wait and assert steps of a runbook (docs/runbooks.md): wait polls a condition until it
// holds or its timeout passes, assert checks it once. Both only read, and a false condition
// fails the step, which stops the run unless continueOnError.

const (
	clusterKind = "Cluster"

	conditionAvailable       = "available"
	conditionRolloutDone     = "rolloutDone"
	conditionPodsRunning     = "podsRunning"
	conditionNodeReady       = "nodeReady"
	conditionNodeSchedulable = "nodeSchedulable"
	conditionEtcdHealthy     = "etcdHealthy"

	waitDefaultTimeout  = 300
	waitMaxTimeout      = 1800
	waitDefaultInterval = 5
	waitMinInterval     = 2
)

// runbookSecond is a second of a wait step's timeout and interval; tests shorten it.
var runbookSecond = time.Second

// runbookCondition is what one params.until reads.
type runbookCondition struct {
	target int
	kinds  []string
	roles  []string
}

var (
	workloadKindNames = []string{"Deployment", "StatefulSet", "DaemonSet"}
	// Reading etcd's status through the Talos API.
	talosReaders = []string{"os:reader", "os:operator", adminRole}
)

// runbookConditions: the Kubernetes reads go through the kubeconfig Talos issues, which
// needs os:admin on a talosconfig cluster; a kubeconfig cluster's RBAC alone decides.
var runbookConditions = map[string]runbookCondition{
	conditionAvailable:       {target: targetWorkload, kinds: workloadKindNames, roles: kubeAdmin},
	conditionRolloutDone:     {target: targetWorkload, kinds: workloadKindNames, roles: kubeAdmin},
	conditionPodsRunning:     {target: targetWorkload, kinds: workloadKindNames, roles: kubeAdmin},
	conditionNodeReady:       {target: targetNode, roles: kubeAdmin},
	conditionNodeSchedulable: {target: targetNode, roles: kubeAdmin},
	conditionEtcdHealthy:     {target: targetCluster, roles: talosReaders},
}

func runbookConditionNames() []string {
	names := make([]string, 0, len(runbookConditions))
	for name := range runbookConditions {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// validateConditionParams checks and defaults the params of a wait or assert step.
func validateConditionParams(action string, p *runbookParams) error {
	if p.MinReplicas != nil {
		if p.Until != conditionPodsRunning {
			return errors.New("params.minReplicas goes with until: podsRunning")
		}

		if *p.MinReplicas < 1 {
			return errors.New("params.minReplicas must be at least 1")
		}
	}

	if action != "wait" {
		return nil
	}

	if p.TimeoutSeconds == nil {
		p.TimeoutSeconds = new(waitDefaultTimeout)
	}

	if p.IntervalSeconds == nil {
		p.IntervalSeconds = new(waitDefaultInterval)
	}

	switch timeout, interval := *p.TimeoutSeconds, *p.IntervalSeconds; {
	case timeout < 1 || timeout > waitMaxTimeout:
		return fmt.Errorf("params.timeoutSeconds must be between 1 and %d", waitMaxTimeout)
	case interval < waitMinInterval:
		return fmt.Errorf("params.intervalSeconds must be at least %d", waitMinInterval)
	}

	return nil
}

// conditionDetail is what the plan says of a wait or assert step, and the wait's timeout.
func (s runbookStep) conditionDetail() (string, int) {
	if runbookActions[s.Action].target != targetCondition {
		return "", 0
	}

	t, p := s.Target, s.Params

	var waitFor, holds string

	switch p.Until {
	case conditionAvailable:
		waitFor, holds = t.Name+" to be available", t.Name+" is available"
	case conditionRolloutDone:
		waitFor, holds = "the rollout of "+t.Name+" to finish", "the rollout of "+t.Name+" is done"
	case conditionPodsRunning:
		pods := "every pod of " + t.Name
		if p.MinReplicas != nil {
			pods = fmt.Sprintf("at least %d pods of %s", *p.MinReplicas, t.Name)
		}

		waitFor, holds = pods+" to run", pods+" run"
	case conditionNodeReady:
		waitFor, holds = "node "+t.Node+" to be Ready", "node "+t.Node+" is Ready"
	case conditionNodeSchedulable:
		waitFor, holds = "node "+t.Node+" to be Ready and schedulable", "node "+t.Node+" is Ready and schedulable"
	case conditionEtcdHealthy:
		waitFor, holds = "every etcd member to be healthy", "every etcd member is healthy"
	}

	if s.Action == "assert" {
		return "check that " + holds, 0
	}

	return "wait up to " + waitDuration(*p.TimeoutSeconds) + " for " + waitFor, *p.TimeoutSeconds
}

// waitDuration is seconds as "5 min", "90 s" or "1 min 30 s".
func waitDuration(seconds int) string {
	minutes, rest := seconds/60, seconds%60

	switch {
	case minutes == 0:
		return fmt.Sprintf("%d s", rest)
	case rest == 0:
		return fmt.Sprintf("%d min", minutes)
	}

	return fmt.Sprintf("%d min %d s", minutes, rest)
}

// conditionReads are the permissions a wait or assert step reads with, nil for the other
// actions and for etcd (the Talos role decides).
func (s runbookStep) conditionReads() []ssarAttributes {
	if runbookActions[s.Action].target != targetCondition {
		return nil
	}

	t := s.Target

	switch s.Params.Until {
	case conditionNodeReady, conditionNodeSchedulable:
		return []ssarAttributes{{Verb: "list", Resource: "nodes"}}
	case conditionEtcdHealthy:
		return nil
	}

	wk, err := findWorkloadKind(t.Kind)
	if err != nil {
		return nil
	}

	reads := []ssarAttributes{{Verb: "get", Group: "apps", Resource: wk.resource, Namespace: t.Namespace}}
	if s.Params.Until != conditionAvailable {
		reads = append(reads, ssarAttributes{Verb: "list", Resource: "pods", Namespace: t.Namespace})
	}

	return reads
}

// deniedReads asks the cluster's RBAC for each read; "" when all are allowed or unknown.
func (g *runbookGate) deniedReads(reads []ssarAttributes) string {
	for _, r := range reads {
		out, err := KubeCan(g.target.config, g.target.context, g.target.server, r.Verb, r.Group, r.Resource, r.Namespace, "")
		if err != nil {
			continue
		}

		var a kubeAccess
		if json.Unmarshal([]byte(out), &a) != nil || a.Allowed || a.Unknown {
			continue
		}

		a.Verb, a.Group, a.Resource, a.Namespace = r.Verb, r.Group, r.Resource, r.Namespace

		return accessReason(a)
	}

	return ""
}

// conditionFailure: the condition can no longer hold (a rollout past its deadline, an
// object that does not exist), so a wait stops at once.
type conditionFailure string

func (f conditionFailure) Error() string { return string(f) }

// conditionStep runs a wait or assert step; progress gets the last observed value while
// waiting.
func (r runbookExec) conditionStep(ctx context.Context, s runbookStep, progress func(string)) (string, error) {
	if s.Action == "assert" {
		ok, observed, err := r.checkCondition(ctx, s)

		switch {
		case err != nil:
			return "", err
		case !ok:
			return "", errors.New("check failed: " + observed)
		}

		return observed, nil
	}

	p := s.Params
	timeout, interval := time.Duration(*p.TimeoutSeconds)*runbookSecond, time.Duration(*p.IntervalSeconds)*runbookSecond
	deadline := time.Now().Add(timeout)

	for {
		ok, observed, err := r.checkCondition(ctx, s)

		var failure conditionFailure

		switch {
		case errors.As(err, &failure):
			return "", err
		case err == nil && ok:
			return observed, nil
		case err != nil:
			// The API server may be away (a node rebooting): the wait goes on.
			observed = err.Error()
		}

		progress(observed)

		if time.Now().Add(interval).After(deadline) {
			return "", fmt.Errorf("timed out after %s: %s", waitDuration(*p.TimeoutSeconds), observed)
		}

		select {
		case <-ctx.Done():
			return "", errors.New("stopped while waiting: " + observed)
		case <-time.After(interval):
		}
	}
}

// checkCondition evaluates the step's condition once: whether it holds, and what was seen.
func (r runbookExec) checkCondition(ctx context.Context, s runbookStep) (bool, string, error) {
	type seen struct {
		ok       bool
		observed string
	}

	t, p := s.Target, s.Params

	if p.Until == conditionEtcdHealthy {
		v, err := withSessionFrom(ctx, r.target.config, r.target.context, callTimeout, func(ctx context.Context, sess *session) (seen, error) {
			ov, err := gatherEtcdOverview(ctx, sess)
			if err != nil {
				return seen{}, err
			}

			ok, observed := etcdAllHealthy(ov)

			return seen{ok, observed}, nil
		})

		return v.ok, v.observed, err
	}

	v, err := withKubeFrom(ctx, r.target, func(ctx context.Context, k *kubeClient) (seen, error) {
		var (
			ok       bool
			observed string
			err      error
		)

		switch p.Until {
		case conditionNodeReady, conditionNodeSchedulable:
			ok, observed, err = nodeCondition(ctx, k, t.Node, p.Until == conditionNodeSchedulable)
		default:
			ok, observed, err = workloadCondition(ctx, k, t, p)
		}

		return seen{ok, observed}, err
	})

	return v.ok, v.observed, err
}

// workloadCondition evaluates available, rolloutDone or podsRunning on a workload.
func workloadCondition(ctx context.Context, k *kubeClient, t runbookTarget, p runbookParams) (bool, string, error) {
	wk, err := findWorkloadKind(t.Kind)
	if err != nil {
		return false, "", conditionFailure(err.Error())
	}

	if p.Until == conditionRolloutDone {
		st, err := rolloutStatus(ctx, k, wk, t.Namespace, t.Name)

		switch {
		case isNotFound(err):
			return false, "", conditionFailure(fmt.Sprintf("%s %s/%s does not exist", wk.kind, t.Namespace, t.Name))
		case err != nil:
			return false, "", err
		case st.Failed && !st.Stale:
			return false, "", conditionFailure("the rollout failed: progress deadline exceeded")
		}

		w := st.Workload
		observed := fmt.Sprintf("%d/%d updated, %d ready", w.Updated, w.Desired, w.Ready)

		return st.Done && !st.Stale, observed, nil
	}

	var obj appsObject

	err = k.get(ctx, "/apis/apps/v1/namespaces/"+url.PathEscape(t.Namespace)+"/"+wk.resource+"/"+url.PathEscape(t.Name), &obj)

	switch {
	case isNotFound(err):
		return false, "", conditionFailure(fmt.Sprintf("%s %s/%s does not exist", wk.kind, t.Namespace, t.Name))
	case err != nil:
		return false, "", err
	}

	desired, available := workloadCounts(wk.kind, obj)
	// Until the controller sees the latest spec, its counters describe the previous one.
	current := obj.Status.ObservedGeneration >= obj.Metadata.Generation

	if p.Until == conditionAvailable {
		return current && available >= desired, fmt.Sprintf("%d/%d available", available, desired), nil
	}

	want := desired
	if p.MinReplicas != nil {
		want = int32(*p.MinReplicas)
	}

	running, err := workloadRunningPods(ctx, k, t.Namespace, obj)
	if err != nil {
		return false, "", err
	}

	return running >= want, fmt.Sprintf("%d running, want %d", running, want), nil
}

// workloadCounts is the replicas a workload wants and those available.
func workloadCounts(kind string, obj appsObject) (desired, available int32) {
	if kind == "DaemonSet" {
		return obj.Status.DesiredNumberScheduled, obj.Status.NumberAvailable
	}

	desired = 1
	if obj.Spec.Replicas != nil {
		desired = *obj.Spec.Replicas
	}

	return desired, obj.Status.AvailableReplicas
}

// workloadRunningPods counts the workload's pods in phase Running and not being deleted.
func workloadRunningPods(ctx context.Context, k *kubeClient, namespace string, obj appsObject) (int32, error) {
	selector, ok := selectorQuery(obj.Spec.Selector)
	if !ok {
		return 0, nil
	}

	var pods kubeList[podObject]
	if err := getList(ctx, k, scopedPath("/api/v1", namespace, "pods")+"?labelSelector="+url.QueryEscape(selector), &pods); err != nil {
		return 0, err
	}

	var n int32

	for _, pod := range pods.Items {
		if pod.Status.Phase == "Running" && pod.Metadata.DeletionTimestamp == nil {
			n++
		}
	}

	return n, nil
}

// nodeCondition evaluates nodeReady or nodeSchedulable; node is the Kubernetes node's name
// or one of its addresses (the address a node step of the talosconfig names).
func nodeCondition(ctx context.Context, k *kubeClient, node string, schedulable bool) (bool, string, error) {
	nodes, err := listObjects[kubeNodeObject](ctx, k, "/api/v1/nodes")
	if err != nil {
		return false, "", err
	}

	i := slices.IndexFunc(nodes, func(n kubeNodeObject) bool { return nodeNamed(n, node) })
	if i < 0 {
		// A node that left may register again: a wait goes on.
		return false, "node " + node + " is not registered", nil
	}

	n := nodes[i]
	ready := n.Status.Conditions.is("Ready")

	observed := "NotReady"
	if ready {
		observed = "Ready"
	}

	if n.Spec.Unschedulable {
		observed += ", cordoned"
	}

	if schedulable {
		return ready && !n.Spec.Unschedulable, observed, nil
	}

	return ready, observed, nil
}

// nodeNamed: node is n's name or one of its addresses.
func nodeNamed(n kubeNodeObject, node string) bool {
	if n.Metadata.Name == node {
		return true
	}

	for _, a := range n.Status.Addresses {
		if a.Address == node {
			return true
		}
	}

	return false
}

// etcdAllHealthy: every control-plane node answered with no error, and no alarm is raised.
func etcdAllHealthy(ov etcdOverview) (bool, string) {
	healthy := 0

	for _, st := range ov.Statuses {
		if st.Error == "" && len(st.Errors) == 0 {
			healthy++
		}
	}

	observed := fmt.Sprintf("%d/%d members healthy", healthy, len(ov.Statuses))

	alarms := make([]string, 0, len(ov.Alarms))
	for _, a := range ov.Alarms {
		alarms = append(alarms, a.Alarm)
	}

	if len(alarms) > 0 {
		observed += ", alarm " + strings.Join(alarms, ", ")
	}

	if ov.Error != "" {
		observed += ": " + ov.Error
	}

	return ov.Error == "" && len(ov.Statuses) > 0 && healthy == len(ov.Statuses) && len(alarms) == 0, observed
}
