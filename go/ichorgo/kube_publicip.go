package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// When Talos does not know a node's public IP (no discovery service, no KubeSpan), it is
// asked from the internet: one short-lived pod per node runs curl against "what is my IP"
// services, whose answer is the address the node's traffic leaves through. The pods use
// the pod network (on most CNIs, pod traffic leaves masqueraded to its node's address)
// and pass the "restricted" pod security level, in a namespace of their own deleted at
// the end, like a network test.

const (
	// publicIPImage is curl's official image, pinned (multi-arch index).
	publicIPImage = "docker.io/curlimages/curl:8.22.0@sha256:58adaa4e8dca9c988bae2aba4ab3434a0bb2da16bbe3f92dec39ec7785166777"
	// publicIPName labels the run namespaces, and prefixes their names.
	publicIPName      = "ichor-publicip"
	publicIPContainer = "curl"
	// publicIPTimeout bounds a whole run; publicIPPodTimeout one pod, image pull included.
	publicIPTimeout    = 5 * time.Minute
	publicIPPodTimeout = 3 * time.Minute
)

// publicIPServices answer a request with the caller's address alone, tried in turn.
var publicIPServices = []string{"https://ifconfig.co/ip", "https://api.ipify.org", "https://icanhazip.com"}

type publicIPNode struct {
	Name     string `json:"name"`               // Kubernetes node name
	Address  string `json:"address"`            // its InternalIP, to match it to a Talos node
	PublicIP string `json:"publicIP,omitempty"` // empty on error
	Error    string `json:"error,omitempty"`
}

type publicIPReport struct {
	Nodes []publicIPNode `json:"nodes"`
	At    int64          `json:"at"` // epoch ms
}

// DetectPublicIPs asks every ready Kubernetes node's public IP from the internet, through
// the Kubernetes API (os:admin): {"nodes":[{name,address,publicIP,error}],"at"}. It creates
// a namespace, runs one curl pod per node there and deletes it, also on failure. It blocks
// until every node answered or publicIPTimeout. kubeServer: see KubePods.
func DetectPublicIPs(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		return toJSON(demoPublicIPs())
	}

	ctx, cancel := context.WithTimeout(context.Background(), publicIPTimeout)
	defer cancel()

	report, err := withKubeContext(ctx, kubeTarget{configYAML, contextName, kubeServer}, detectPublicIPs)
	if err != nil {
		return "", err
	}

	return toJSON(report)
}

func detectPublicIPs(ctx context.Context, k *kubeClient) (publicIPReport, error) {
	list, err := listNetPerfNodes(ctx, k)
	if err != nil {
		return publicIPReport{}, err
	}

	sweepRunNamespaces(ctx, k, publicIPName, time.Now())

	ns, err := createRunNamespace(ctx, k, publicIPName, false)
	if err != nil {
		return publicIPReport{}, fmt.Errorf("create the namespace: %w", err)
	}

	defer func() { _ = deleteNetPerfNamespace(ctx, k, ns) }() //nolint:errcheck // swept by the next run

	nodes := make([]publicIPNode, len(list.Nodes))

	forEachNode(list.Nodes, func(i int, n netPerfNode) {
		nodes[i] = publicIPNode{Name: n.Name, Address: n.Address}
		if !n.Ready {
			nodes[i].Error = "node not ready"

			return
		}

		ip, probeErr := probePublicIPPod(ctx, k, ns, fmt.Sprintf("probe-%d", i), n.Name)
		if probeErr != nil {
			nodes[i].Error = probeErr.Error()
		}

		nodes[i].PublicIP = ip
	})

	return publicIPReport{Nodes: nodes, At: time.Now().UnixMilli()}, nil
}

// probePublicIPPod runs one curl pod on node and returns the address it was seen from.
func probePublicIPPod(ctx context.Context, k *kubeClient, ns, name, node string) (string, error) {
	img := runPodImage{app: publicIPName, image: publicIPImage, container: publicIPContainer}
	spec := runPodSpec(img, name, node, false, publicIPPodTimeout, publicIPCommand()...)

	if err := k.post(ctx, netPerfNamespacePath(ns)+"/pods", spec, nil); err != nil {
		return "", kubeError(err)
	}

	pod, err := waitRunPod(ctx, k, "public IP", ns, name, node, publicIPPodTimeout, nil, func(pod netPerfPod) bool {
		return pod.Status.Phase == "Succeeded" || pod.Status.Phase == "Failed"
	})
	if err != nil {
		return "", err
	}

	log, err := k.getText(ctx, netPerfNamespacePath(ns)+"/pods/"+url.PathEscape(name)+"/log?container="+publicIPContainer)
	if err != nil {
		return "", fmt.Errorf("read the answer: %w", kubeError(err))
	}

	if pod.Status.Phase == "Failed" {
		return "", fmt.Errorf("no answer from %s: %s", strings.Join(publicIPServices, ", "), lastLine(log))
	}

	return parsePublicIP(log)
}

// publicIPCommand asks each service in turn over IPv4, then over any family.
func publicIPCommand() []string {
	script := `for f in -4 ""; do for u in "$@"; do curl -fsS $f --max-time 8 "$u" && exit 0; done; done; exit 1`

	return append([]string{"sh", "-c", script, "sh"}, publicIPServices...)
}

// parsePublicIP reads the address a service answered: the last line, after the errors curl
// wrote for the services that did not answer (the log holds stderr too).
func parsePublicIP(log string) (string, error) {
	answer := lastLine(log)

	a, err := netip.ParseAddr(answer)
	if err != nil {
		return "", fmt.Errorf("unexpected answer %q", answer)
	}

	if !isPublicAddr(a) {
		return "", errors.New("the answer is not a public address: " + a.String())
	}

	return a.Unmap().String(), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")

	return strings.TrimSpace(lines[len(lines)-1])
}

func demoPublicIPs() publicIPReport {
	nodes := demoNetPerfNodes()
	out := publicIPReport{Nodes: make([]publicIPNode, len(nodes)), At: time.Now().UnixMilli()}

	for i, n := range nodes {
		out.Nodes[i] = publicIPNode{Name: n.Name, Address: n.Address, PublicIP: fmt.Sprintf("203.0.113.%d", 40+i)}
	}

	return out
}
