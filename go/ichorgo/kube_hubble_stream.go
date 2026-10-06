package ichorgo

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	// hubbleEmitInterval is the most often a live flow view sends a snapshot.
	hubbleEmitInterval = time.Second
	// hubbleAgentRefresh is how often the agents are listed again: a stream that ended is
	// started again then, on the node's current agent pod (Cilium rollouts replace them).
	hubbleAgentRefresh = 10 * time.Second
	// hubbleBackfill is how many stored flows each agent sends before following.
	hubbleBackfill = "200"
)

var (
	errCiliumMissing = errors.New("Cilium is not running in this cluster: no cilium-agent pods (label k8s-app=cilium)")
	errHubbleOff     = errors.New("Hubble is disabled in Cilium: set hubble.enabled=true in its Helm values (enable-hubble in the cilium-config ConfigMap)")
)

// HubbleListener follows a live flow view (implemented in Kotlin/Swift).
type HubbleListener interface {
	// OnUpdate gets the view as JSON (see hubbleSnapshot), at most once a second while it
	// changes: recent flows, grouped drops with the policies behind them, agent states.
	OnUpdate(json string)
	// OnDone is called exactly once; errMessage is empty when cancelled.
	OnDone(errMessage string)
}

// HubbleRun is a handle on a live flow view.
type HubbleRun struct {
	cancel context.CancelFunc
}

// Cancel stops following the agents; OnDone follows.
func (r *HubbleRun) Cancel() { r.cancel() }

// hubbleFilter narrows what the agents send.
type hubbleFilter struct {
	namespace, pod string
	dropsOnly      bool
}

// args are the `hubble observe` filter flags of f.
func (f hubbleFilter) args() []string {
	var out []string

	switch {
	case f.pod != "":
		out = append(out, "--pod", f.namespace+"/"+f.pod)
	case f.namespace != "":
		out = append(out, "--namespace", f.namespace)
	}

	if f.dropsOnly {
		out = append(out, "--verdict", "DROPPED", "--verdict", "AUDIT")
	}

	return out
}

func (f hubbleFilter) validate() error {
	switch {
	case f.pod != "":
		return validateKubeName("pod", f.namespace, f.pod)
	case f.namespace != "":
		return validateKubeName("namespace", f.namespace, f.namespace)
	default:
		return nil
	}
}

// hubbleCommand follows the agent's flows like `hubble observe --follow`, on its local
// Hubble socket: no Relay, no TLS, nothing to install. It starts with the last
// hubbleBackfill stored flows, or, following again, with those after since (unix ms).
func hubbleCommand(f hubbleFilter, since int64) []string {
	argv := []string{"hubble", "observe", "--follow", "-o", "jsonpb"}
	if since > 0 {
		argv = append(argv, "--since", time.UnixMilli(since+1).UTC().Format(time.RFC3339Nano))
	} else {
		argv = append(argv, "--last", hubbleBackfill)
	}

	return append(argv, f.args()...)
}

// StartHubbleFlows follows the cluster's network flows live, like Hubble UI, when Cilium
// runs with Hubble (os:admin). It runs `hubble observe --follow` in every cilium-agent pod
// (read-only, the CLI ships in the agent image) and merges their flows. namespace and pod
// narrow it (pod needs namespace); dropsOnly keeps dropped and audited flows only. Each
// drop group names the policies that denied it, or that put its pod in default-deny.
// kubeServer: see KubePods.
func StartHubbleFlows(configYAML, contextName, kubeServer, namespace, pod string, dropsOnly bool, listener HubbleListener) *HubbleRun {
	contextName = unmaskContext(configYAML, contextName)

	listener = maskedHubbleListener{listener}

	filter := hubbleFilter{
		namespace: privacy.reveal(strings.TrimSpace(namespace)),
		pod:       privacy.reveal(strings.TrimSpace(pod)),
		dropsOnly: dropsOnly,
	}

	ctx, cancel := context.WithCancel(context.Background())
	emit := func(s hubbleSnapshot) { emitJSON(s, listener.OnUpdate) }

	go func() {
		defer cancel()

		var err error

		switch {
		case filter.validate() != nil:
			err = filter.validate()
		case isDemoContext(configYAML, contextName):
			err = runDemoHubble(ctx, filter, emit)
		default:
			err = startHubble(ctx, kubeTarget{configYAML, contextName, kubeServer}, filter, emit)
		}

		if errors.Is(err, context.Canceled) {
			err = nil
		}

		msg := ""
		if err != nil {
			msg = err.Error()
		}

		listener.OnDone(msg)
	}()

	return &HubbleRun{cancel: cancel}
}

// startHubble checks that Cilium records flows, then follows them until ctx ends. The
// checks answer outside withKube: a cluster without Hubble keeps its working client.
func startHubble(ctx context.Context, target kubeTarget, filter hubbleFilter, emit func(hubbleSnapshot)) error {
	status, err := withKube(target, readCiliumStatus)

	switch {
	case err != nil:
		return err
	case !status.Installed:
		return errCiliumMissing
	case !status.Hubble:
		return errHubbleOff
	}

	_, err = withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (struct{}, error) {
		return struct{}{}, followHubble(ctx, k, status, filter, emit)
	})

	return err
}

// followHubble streams every agent until ctx ends.
func followHubble(ctx context.Context, k *kubeClient, status ciliumStatus, filter hubbleFilter, emit func(hubbleSnapshot)) error {
	agg := newHubbleAgg(status)

	var wg sync.WaitGroup

	wg.Go(func() { refreshPolicies(ctx, k, agg) })
	wg.Go(func() { superviseAgents(ctx, k, status, filter, agg) })

	emitSnapshots(ctx, agg, emit)
	wg.Wait()

	return ctx.Err()
}

// refreshPolicies rereads the policies now and every policyRefreshInterval.
func refreshPolicies(ctx context.Context, k *kubeClient, agg *hubbleAgg) {
	for {
		readCtx, cancel := context.WithTimeout(ctx, callTimeout)
		policies, _, err := readNetPolicies(readCtx, k)
		cancel()

		if ctx.Err() != nil {
			return
		}

		agg.setPolicies(policies, err)

		select {
		case <-ctx.Done():
			return
		case <-time.After(policyRefreshInterval):
		}
	}
}

// emitSnapshots sends what changed every hubbleEmitInterval until ctx ends.
func emitSnapshots(ctx context.Context, agg *hubbleAgg, emit func(hubbleSnapshot)) {
	if s, ok := agg.snapshot(true); ok {
		emit(s)
	}

	ticker := time.NewTicker(hubbleEmitInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s, ok := agg.snapshot(false); ok {
				emit(s)
			}
		}
	}
}
