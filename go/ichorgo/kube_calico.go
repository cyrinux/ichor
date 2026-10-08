package ichorgo

import (
	"context"
	"net/url"
	"strings"
	"sync"
)

// Calico as a flow source. Calico 3.30+ records flows with Goldmane (every calico-node sends
// them over gRPC) and serves them through Whisker: a Deployment in calico-system whose
// whisker-backend container streams the flows the nginx of its whisker container exposes.
// The app reads that stream through the API server's Service proxy.
const (
	calicoNodeSelector  = "k8s-app=calico-node"
	calicoNodeContainer = "calico-node"
	whiskerSelector     = "k8s-app=whisker"
	whiskerBackend      = "whisker-backend"
	whiskerService      = "whisker"
	// whiskerTLSPort is the port newer operators serve Whisker on, with TLS; 3.30 used 8081, plain.
	whiskerTLSPort = 8443
)

// The CNIs the app follows flows from (ciliumStatus.CNI).
const (
	flowCNICilium = "cilium"
	flowCNICalico = "calico"
)

// whiskerInfo is the whisker Service the flows are read through.
type whiskerInfo struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`  // a whisker pod: the view's one "agent"
	Port      int    `json:"port"` // the Service port
	TLS       bool   `json:"tls"`  // addressed as https:whisker:<port>
}

// readFlowSource is the status of whichever CNI records flows: Cilium when it runs, else
// Calico. Both are looked for at once: a phone pays each round trip.
func readFlowSource(ctx context.Context, k *kubeClient) (ciliumStatus, error) {
	var (
		cilium, calico       ciliumStatus
		ciliumErr, calicoErr error
		wg                   sync.WaitGroup
	)

	wg.Go(func() { cilium, ciliumErr = readCiliumStatus(ctx, k) })
	wg.Go(func() { calico, calicoErr = readCalicoStatus(ctx, k) })
	wg.Wait()

	switch {
	case ciliumErr != nil:
		return ciliumStatus{}, ciliumErr
	case cilium.Installed:
		return cilium, nil
	default:
		return calico, calicoErr
	}
}

// readCalicoStatus finds the calico-node pods (Calico runs; their image tag is its version)
// and, with them, the whisker pod and Service the flows come from (Hubble: flows are recorded).
func readCalicoStatus(ctx context.Context, k *kubeClient) (ciliumStatus, error) {
	out, err := readAgentPods(ctx, k, calicoNodeSelector, calicoNodeContainer, flowCNICalico)
	if err != nil || !out.Installed {
		return out, err
	}

	whisker, err := readWhisker(ctx, k)
	if err != nil {
		return out, err
	}

	if whisker != nil {
		out.Hubble, out.Namespace, out.Whisker = true, whisker.Namespace, whisker
	}

	return out, nil
}

// readWhisker finds a whisker pod (one with its backend ready first) and the port of its
// Service; nil when Whisker is not installed.
func readWhisker(ctx context.Context, k *kubeClient) (*whiskerInfo, error) {
	pods, err := listDSPods(ctx, k, whiskerSelector)
	if err != nil {
		return nil, err
	}

	// The pods that carry the backend, a ready one first.
	var pod *dsPod

	for i := range pods {
		if p := &pods[i]; p.hasContainer(whiskerBackend) && (pod == nil || p.containerReady(whiskerBackend)) {
			pod = p
		}
	}

	if pod == nil {
		return nil, nil //nolint:nilnil // not installed
	}

	var svc struct {
		Spec struct {
			Ports []whiskerServicePort `json:"ports"`
		} `json:"spec"`
	}

	namespace := pod.Metadata.Namespace
	if err := k.get(ctx, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/services/"+whiskerService, &svc); err != nil {
		return nil, ignoreNotFound(err)
	}

	port, tls, ok := whiskerPort(svc.Spec.Ports)
	if !ok {
		return nil, nil //nolint:nilnil // a Service without ports: nothing to reach
	}

	return &whiskerInfo{Namespace: namespace, Pod: pod.Metadata.Name, Port: port, TLS: tls}, nil
}

type whiskerServicePort struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

// whiskerPort picks the Service port to proxy to: the TLS one (8443, or named after it)
// when there is one, else the first; ok is false without any.
func whiskerPort(ports []whiskerServicePort) (port int, tls, ok bool) {
	for _, p := range ports {
		name := strings.ToLower(p.Name)
		if p.Port == whiskerTLSPort || strings.Contains(name, "https") || strings.Contains(name, "tls") {
			return p.Port, true, true
		}
	}

	if len(ports) > 0 {
		return ports[0].Port, false, true
	}

	return 0, false, false
}
