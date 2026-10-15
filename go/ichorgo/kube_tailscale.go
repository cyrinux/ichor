package ichorgo

import "context"

// groupTailscale is the Tailscale Kubernetes operator's group: Connectors (subnet routers, exit
// nodes, app connectors), ProxyGroups, ProxyClasses and the DNSConfig. Every kind is
// cluster-scoped.
const groupTailscale = "tailscale.com"

// What a Tailscale row's condition says, problems first.
const (
	tsHealthCritical = "critical"
	tsHealthWarning  = "warning"
	tsHealthOK       = "ok"
)

const (
	// tsListMax is how many objects of one kind the report keeps (problems first).
	tsListMax = 200
	// tsOperatorSelector finds the operator Deployment the Helm chart installs.
	tsOperatorSelector = "app.kubernetes.io/name=tailscale-operator"
	// The reasons the reader gives when the operator's own condition cannot be trusted.
	tsReasonNotReconciled     = "NotReconciled"
	tsReasonReconciling       = "Reconciling"
	tsReasonProxyClassMissing = "ProxyClassMissing"
)

// tsReport is what the Tailscale operator runs in the cluster. Installed is false (and every
// list empty) when the cluster does not serve tailscale.com.
type tsReport struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"` // the served tailscale.com version, "" when not installed
	// Error is a list that failed; what could be read still shows.
	Error        string         `json:"error,omitempty"`
	Truncated    bool           `json:"truncated"` // a kind had more than tsListMax objects
	Operator     *tsOperator    `json:"operator"`  // nil when no operator Deployment was found
	Connectors   []tsConnector  `json:"connectors"`
	ProxyGroups  []tsProxyGroup `json:"proxyGroups"`
	ProxyClasses []tsProxyClass `json:"proxyClasses"`
	DNSConfig    *tsDNSConfig   `json:"dnsConfig"`
	Counts       tsCounts       `json:"counts"`
}

// tsOperator is the operator's Deployment: when it is down, nothing below is reconciled.
type tsOperator struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Ready     int    `json:"ready"`
	Desired   int    `json:"desired"`
	Image     string `json:"image"`
	Health    string `json:"health"`
}

// tsState is a row's readiness from its operator condition, and our grouping of it.
type tsState struct {
	Ready   bool   `json:"ready"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Health  string `json:"health"`
}

// tsDevice is one tailnet device a Connector or ProxyGroup runs.
type tsDevice struct {
	Hostname        string   `json:"hostname"`
	TailnetIPs      []string `json:"tailnetIPs"`
	StaticEndpoints []string `json:"staticEndpoints"`
}

// tsConnector is a Connector: a subnet router, an exit node or an app connector.
type tsConnector struct {
	Name       string   `json:"name"`
	Hostname   string   `json:"hostname"`
	TailnetIPs []string `json:"tailnetIPs"`
	// SubnetRoutes is status.subnetRoutes as the operator writes it: comma-separated.
	SubnetRoutes       string     `json:"subnetRoutes"`
	AdvertisedRoutes   []string   `json:"advertisedRoutes"`
	ExitNode           bool       `json:"exitNode"`
	AppConnector       bool       `json:"appConnector"`
	AppConnectorRoutes []string   `json:"appConnectorRoutes"`
	Tags               []string   `json:"tags"`
	ProxyClass         string     `json:"proxyClass"`
	Replicas           int        `json:"replicas"`
	Devices            []tsDevice `json:"devices"`
	tsState
}

// tsProxyGroup is a ProxyGroup: a pool of egress, ingress or kube-apiserver proxies.
type tsProxyGroup struct {
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Replicas   int        `json:"replicas"`
	ProxyClass string     `json:"proxyClass"`
	Tags       []string   `json:"tags"`
	URL        string     `json:"url"` // set for kube-apiserver groups
	Devices    []tsDevice `json:"devices"`
	// Available is ProxyGroupAvailable: at least one proxy runs (Ready: all of them).
	Available bool `json:"available"`
	tsState
}

// tsProxyClass is a ProxyClass, and how many Connectors and ProxyGroups name it.
type tsProxyClass struct {
	Name   string `json:"name"`
	UsedBy int    `json:"usedBy"`
	tsState
}

// tsDNSConfig is the DNSConfig: the nameserver that resolves tailnet names in the cluster.
type tsDNSConfig struct {
	Name         string `json:"name"`
	NameserverIP string `json:"nameserverIP"`
	tsState
}

type tsCounts struct {
	OK       int `json:"ok"`
	Warning  int `json:"warning"`
	Critical int `json:"critical"`
}

// KubeTailscale reads the Tailscale Kubernetes operator (os:admin): {"installed","version",
// "error","truncated","operator","connectors","proxyGroups","proxyClasses","dnsConfig","counts"}
// (see tsReport), problems first in every list. A cluster without the operator answers
// {"installed":false} and no error. kubeServer: see KubePods.
func KubeTailscale(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoTailscale,
		func(ctx context.Context, k *kubeClient) (tsReport, error) {
			return readTailscale(ctx, k)
		})
}
