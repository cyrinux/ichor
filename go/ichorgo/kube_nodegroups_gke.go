package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// GKE node pools, read-only, with the bearer of the cluster's own Google sign-in. The stored
// cluster keeps neither its project nor its location: the project comes from a node's
// providerID (gce://<project>/<zone>/<instance>), the cluster from the project's list, matched
// on the API server address the kubeconfig dials.

const (
	nodeGroupsProviderGKE = "gke"

	gkeActionListClusters = "container.clusters.list"

	gkeProviderIDPrefix = "gce://"

	gkeCapacitySpot     = "SPOT"
	gkeCapacityOnDemand = "ON_DEMAND"
)

// gkeCluster is the part of a Cluster resource the reader needs.
type gkeCluster struct {
	Name                 string `json:"name"`
	Location             string `json:"location"`
	Endpoint             string `json:"endpoint"`
	CurrentMasterVersion string `json:"currentMasterVersion"`
	PrivateClusterConfig struct {
		PrivateEndpoint string `json:"privateEndpoint"`
		PublicEndpoint  string `json:"publicEndpoint"`
	} `json:"privateClusterConfig"`
	ControlPlaneEndpointsConfig struct {
		DNSEndpointConfig struct {
			Endpoint string `json:"endpoint"`
		} `json:"dnsEndpointConfig"`
	} `json:"controlPlaneEndpointsConfig"`
	NodePools []gkeNodePool `json:"nodePools"`
}

// gkeNodePool is the part of a NodePool resource the reader needs.
type gkeNodePool struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	// StatusMessage is deprecated by Google in favour of conditions, still filled on errors.
	StatusMessage string `json:"statusMessage"`
	Version       string `json:"version"`
	Config        struct {
		MachineType string `json:"machineType"`
		DiskSizeGb  int    `json:"diskSizeGb"`
		ImageType   string `json:"imageType"`
		Spot        bool   `json:"spot"`
		Preemptible bool   `json:"preemptible"`
	} `json:"config"`
	Autoscaling struct {
		Enabled bool `json:"enabled"`
		// MinNodeCount and MaxNodeCount are per zone; the totals are for the whole pool.
		MinNodeCount      int `json:"minNodeCount"`
		MaxNodeCount      int `json:"maxNodeCount"`
		TotalMinNodeCount int `json:"totalMinNodeCount"`
		TotalMaxNodeCount int `json:"totalMaxNodeCount"`
	} `json:"autoscaling"`
	Locations  []string `json:"locations"`
	Conditions []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"conditions"`
}

// gkeNode is the part of a node the reader needs: its pool label and providerID.
type gkeNode struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		ProviderID string `json:"providerID"`
	} `json:"spec"`
}

// readGKENodePools reads the node pools of the GKE cluster k talks to. Every failure is a
// line in the report, never an error: the rest of the Autoscaling screen still shows.
func readGKENodePools(ctx context.Context, k *kubeClient) nodeGroupsReport {
	report := nodeGroupsReport{Provider: nodeGroupsProviderGKE, Installed: true, Groups: []managedNodeGroup{}}

	// The nodes name the project: an account that may not list them cannot be placed.
	nodes, err := listObjects[gkeNode](ctx, k, "/api/v1/nodes")
	if err != nil {
		report.Error = nodeGroupsCredentialsError(err)

		return report
	}

	project := gkeProjectOf(nodes)
	if project == "" {
		report.Error = "no node says which Google Cloud project this cluster is in"

		return report
	}

	token, err := k.tokens.get(ctx)
	if err != nil {
		report.Error = nodeGroupsCredentialsError(err)

		return report
	}

	account := gkeAccount{token: token, quotaProject: gkeQuotaProject(k.tokens.key)}

	var list struct {
		Clusters []gkeCluster `json:"clusters"`
	}

	if err := account.get(ctx, gkeEndpoint+"/v1/projects/"+url.PathEscape(project)+"/locations/-/clusters", &list); err != nil {
		var e *cloudHTTPError
		if errors.As(err, &e) && e.status == http.StatusForbidden {
			report.Forbidden = gkeActionListClusters
			report.Error = gkeActionListClusters + " is not allowed for this account"

			return report
		}

		report.Error = err.Error()

		return report
	}

	// Only the matched cluster goes in the report: the project's other clusters stay out.
	i := slices.IndexFunc(list.Clusters, func(c gkeCluster) bool { return gkeClusterServes(c, k.base) })
	if i < 0 {
		report.Error = "this cluster was not found in project " + project

		return report
	}

	cluster := list.Clusters[i]
	report.Region, report.Cluster = cluster.Location, cluster.Name
	report.Groups = gkeNodePoolRows(cluster.NodePools, gkePoolCounts(nodes))

	return withClusterVersion(report, cluster.CurrentMasterVersion)
}

// gkeProjectOf is the project of the first node whose providerID is a Compute Engine one.
func gkeProjectOf(nodes []gkeNode) string {
	for _, n := range nodes {
		rest, ok := strings.CutPrefix(n.Spec.ProviderID, gkeProviderIDPrefix)
		if !ok {
			continue
		}

		if project, _, _ := strings.Cut(rest, "/"); project != "" {
			return project
		}
	}

	return ""
}

// gkeQuotaProject is the quota project of a Google account's user credentials (billed with
// X-Goog-User-Project, as at import), "" for any other sign-in.
func gkeQuotaProject(key string) string {
	raw := kubeAuth.load(key).secret(gcpFieldUserCredentials)
	if raw == "" {
		return ""
	}

	creds, err := parseUserCredentials(raw)
	if err != nil {
		return ""
	}

	return creds.QuotaProjectID
}

// gkeClusterServes reports whether c answers at base: its public endpoint, private endpoint,
// public endpoint of a private cluster or DNS endpoint is base's host. A cluster reached
// through a proxy or another address matches none, and no guess is made by name.
func gkeClusterServes(c gkeCluster, base *url.URL) bool {
	if base == nil {
		return false
	}

	host := strings.ToLower(base.Hostname())
	if host == "" {
		return false
	}

	for _, endpoint := range []string{
		c.Endpoint, c.PrivateClusterConfig.PrivateEndpoint, c.PrivateClusterConfig.PublicEndpoint,
		c.ControlPlaneEndpointsConfig.DNSEndpointConfig.Endpoint,
	} {
		if endpoint != "" && gkeEndpointHost(endpoint) == host {
			return true
		}
	}

	return false
}

// gkeEndpointHost is an endpoint field as a bare lowercase host: Google writes an address
// ("203.0.113.7") or a name, without a scheme or port, but a scheme or port is tolerated.
func gkeEndpointHost(endpoint string) string {
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	endpoint = strings.TrimSuffix(endpoint, "/")

	if host, _, err := net.SplitHostPort(endpoint); err == nil {
		endpoint = host
	}

	return strings.ToLower(strings.Trim(endpoint, "[]"))
}

// gkePoolCounts is how many nodes carry each pool's label.
func gkePoolCounts(nodes []gkeNode) map[string]int {
	counts := map[string]int{}

	for _, n := range nodes {
		if pool := n.Metadata.Labels[gkePoolLabel]; pool != "" {
			counts[pool]++
		}
	}

	return counts
}

// gkeNodePoolRows maps the pools, sorted by name.
func gkeNodePoolRows(pools []gkeNodePool, counts map[string]int) []managedNodeGroup {
	rows := make([]managedNodeGroup, 0, len(pools))
	for _, p := range pools {
		rows = append(rows, gkeNodePoolRow(p, counts[p.Name]))
	}

	slices.SortFunc(rows, func(a, b managedNodeGroup) int { return cmp.Compare(a.Name, b.Name) })

	return rows
}

func gkeNodePoolRow(p gkeNodePool, nodes int) managedNodeGroup {
	row := managedNodeGroup{
		Name: p.Name, Status: p.Status, Version: p.Version, AMIType: p.Config.ImageType,
		CapacityType: gkeCapacityOnDemand, InstanceTypes: []string{}, DiskSize: p.Config.DiskSizeGb,
		// The pool resource does not hold its current size (its instance groups do, in the
		// Compute API, which needs another permission): the nodes the cluster sees stand in.
		Nodes: nodes, Desired: nodes,
		Updating: p.Status == "RECONCILING" || p.Status == "PROVISIONING",
		Issues:   gkePoolIssues(p),
	}

	if p.Config.MachineType != "" {
		row.InstanceTypes = []string{p.Config.MachineType}
	}

	if p.Config.Spot || p.Config.Preemptible {
		row.CapacityType = gkeCapacitySpot
	}

	if a := p.Autoscaling; a.Enabled {
		if a.TotalMaxNodeCount > 0 {
			row.Min, row.Max = a.TotalMinNodeCount, a.TotalMaxNodeCount
		} else {
			// Per-zone bounds: a regional pool runs them in every zone.
			zones := max(len(p.Locations), 1)
			row.Min, row.Max = a.MinNodeCount*zones, a.MaxNodeCount*zones
		}

		row.AtMax = row.Max > 0 && nodes >= row.Max
	}

	return row
}

// gkePoolIssues are the pool's conditions, and its status message when no condition says it.
func gkePoolIssues(p gkeNodePool) []nodeGroupIssue {
	issues := []nodeGroupIssue{}

	for _, c := range p.Conditions {
		issues = append(issues, nodeGroupIssue{Code: c.Code, Message: c.Message, ResourceIDs: []string{}})
	}

	said := slices.ContainsFunc(issues, func(i nodeGroupIssue) bool { return i.Message == p.StatusMessage })
	if p.StatusMessage != "" && !said {
		issues = append([]nodeGroupIssue{{Code: p.Status, Message: p.StatusMessage, ResourceIDs: []string{}}}, issues...)
	}

	return issues
}
