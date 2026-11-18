package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"
)

// The cloud node pools of a cluster, read from the provider's API with the credentials of
// the cluster's own sign-in. EKS managed node groups first; other providers fill the same
// report with their own provider name.

const (
	nodeGroupsProviderEKS = "eks"

	// eksNodeGroupsParallel is how many DescribeNodegroup calls run at once.
	eksNodeGroupsParallel = 4

	eksNodeGroupUpdating = "UPDATING"

	eksActionListNodegroups    = "eks:ListNodegroups"
	eksActionDescribeNodegroup = "eks:DescribeNodegroup"
)

// nodeGroupsReport is a cluster's cloud node pools. Installed false (and provider "") when
// the cluster does not sign in through a provider the reader knows: the apps hide the group.
type nodeGroupsReport struct {
	Provider  string `json:"provider"`
	Installed bool   `json:"installed"`
	Region    string `json:"region"`
	Cluster   string `json:"cluster"`
	// Error is why the groups could not be read (or only some of them); Forbidden names the
	// provider action the credentials may not call, when that is the reason.
	Error     string             `json:"error"`
	Forbidden string             `json:"forbidden"`
	Groups    []managedNodeGroup `json:"groups"`
}

// managedNodeGroup is one pool as the provider describes it. Nodes is how many of the
// cluster's nodes carry the pool's label: it differs from Desired while the pool scales.
type managedNodeGroup struct {
	Name           string           `json:"name"`
	Status         string           `json:"status"`
	Desired        int              `json:"desired"`
	Min            int              `json:"min"`
	Max            int              `json:"max"`
	Version        string           `json:"version"`
	ReleaseVersion string           `json:"releaseVersion"`
	AMIType        string           `json:"amiType"`
	CapacityType   string           `json:"capacityType"`
	InstanceTypes  []string         `json:"instanceTypes"`
	DiskSize       int              `json:"diskSize"`
	Nodes          int              `json:"nodes"`
	AtMax          bool             `json:"atMax"`
	Issues         []nodeGroupIssue `json:"issues"`
	Updating       bool             `json:"updating"`
	// Created is unix ms, 0 when unknown.
	Created int64 `json:"created"`
}

type nodeGroupIssue struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	ResourceIDs []string `json:"resourceIds"`
}

// eksNodegroup is DescribeNodegroup's answer.
type eksNodegroup struct {
	Name          string `json:"nodegroupName"`
	Status        string `json:"status"`
	ScalingConfig struct {
		Min     int `json:"minSize"`
		Max     int `json:"maxSize"`
		Desired int `json:"desiredSize"`
	} `json:"scalingConfig"`
	Version        string   `json:"version"`
	ReleaseVersion string   `json:"releaseVersion"`
	AMIType        string   `json:"amiType"`
	CapacityType   string   `json:"capacityType"`
	InstanceTypes  []string `json:"instanceTypes"`
	DiskSize       int      `json:"diskSize"`
	Health         struct {
		Issues []struct {
			Code        string   `json:"code"`
			Message     string   `json:"message"`
			ResourceIDs []string `json:"resourceIds"`
		} `json:"issues"`
	} `json:"health"`
	// CreatedAt is epoch seconds (AWS REST JSON timestamps).
	CreatedAt float64 `json:"createdAt"`
}

// KubeNodeGroups reads the cluster's cloud node pools (EKS managed node groups) with the
// credentials of the cluster's own sign-in: {"provider","installed","region","cluster",
// "error","forbidden","groups"} (see nodeGroupsReport). A cluster that does not sign in
// through EKS answers installed false without any call to AWS. kubeServer: see KubePods.
func KubeNodeGroups(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	target := kubeTarget{configYAML, contextName, kubeServer}

	return kubeReadJSON(target, demoNodeGroupsFor(target), readNodeGroups)
}

func noNodeGroups() nodeGroupsReport { return nodeGroupsReport{Groups: []managedNodeGroup{}} }

func readNodeGroups(ctx context.Context, k *kubeClient) (nodeGroupsReport, error) {
	if k.tokens == nil {
		return noNodeGroups(), nil
	}

	m, ok := k.tokens.method.(*eksMethod)
	if !ok {
		return noNodeGroups(), nil
	}

	report := nodeGroupsReport{Provider: nodeGroupsProviderEKS, Installed: true, Region: m.region, Cluster: m.cluster, Groups: []managedNodeGroup{}}

	creds, err := eksSourceCredentials(ctx, k.tokens, m)
	if err != nil {
		report.Error = nodeGroupsCredentialsError(err)

		return report, nil
	}

	groups, forbidden, err := listEKSNodeGroups(ctx, m, creds)
	report.Groups, report.Forbidden = groups, forbidden

	switch {
	case forbidden != "":
		report.Error = forbidden + " is not allowed for this role"
	case err != nil:
		report.Error = err.Error()
	}

	// The node counts are a bonus: a role that may not list nodes still sees the groups.
	nodes, _ := listObjects[struct { //nolint:errcheck
		Metadata checkMeta `json:"metadata"`
	}](ctx, k, "/api/v1/nodes")

	counts := map[string]int{}
	for _, n := range nodes {
		if g := n.Metadata.Labels[eksNodeGroupLabel]; g != "" {
			counts[g]++
		}
	}

	for i := range report.Groups {
		report.Groups[i].Nodes = counts[report.Groups[i].Name]
	}

	return report, nil
}

// nodeGroupsCredentialsError is why no credentials could be had. The reader never opens the
// sign-in sheet (that is the API server's token source's job): it says so in a line.
func nodeGroupsCredentialsError(err error) string {
	var signIn *errSignInRequired
	if errors.As(err, &signIn) {
		return "sign in again to read the node groups"
	}

	return err.Error()
}

// eksSourceCredentials are the credentials m signs with, from the sign-in stored for the
// cluster; the renewed state (role credentials cache) is saved back. The token source's lock
// keeps a token renewal and this read from refreshing the session at the same time.
func eksSourceCredentials(ctx context.Context, s *authTokenSource, m *eksMethod) (awsCredentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := kubeAuth.load(s.key)
	if state.Method != "" && state.Method != m.name() {
		return awsCredentials{}, signInRequired(m.name(), "the stored sign-in is for another method")
	}

	state.Method = m.name()

	creds, updated, err := m.signingCredentials(ctx, state)
	if err != nil {
		return awsCredentials{}, err
	}

	updated.Method = m.name()
	kubeAuth.save(s.key, updated)

	return creds, nil
}

// listEKSNodeGroups lists m's cluster's managed node groups, every page, each described,
// sorted by name. forbidden names the action AWS refused; the groups described before a
// refusal or a failure are kept.
func listEKSNodeGroups(ctx context.Context, m *eksMethod, creds awsCredentials) (groups []managedNodeGroup, forbidden string, err error) {
	base := "/clusters/" + url.PathEscape(m.cluster) + "/node-groups"

	get := func(path string, query url.Values, out any) error {
		u := eksEndpoint(m.region) + path
		if len(query) > 0 {
			u += "?" + query.Encode()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}

		sigV4Sign(req, emptySHA256, creds, m.region, "eks", time.Now())

		return cloudJSON(req, out)
	}

	var names []string

	next := ""

	for range eksMaxPages {
		q := url.Values{"maxResults": {"100"}}
		if next != "" {
			q.Set("nextToken", next)
		}

		var page struct {
			Nodegroups []string `json:"nodegroups"`
			NextToken  string   `json:"nextToken"`
		}

		if err := get(base, q, &page); err != nil {
			if unauthorized(err) {
				return []managedNodeGroup{}, eksActionListNodegroups, err
			}

			return []managedNodeGroup{}, "", err
		}

		names = append(names, page.Nodegroups...)

		if next = page.NextToken; next == "" {
			break
		}
	}

	return describeEKSNodeGroups(names, func(name string, out *eksNodegroup) error {
		var d struct {
			Nodegroup eksNodegroup `json:"nodegroup"`
		}

		err := get(base+"/"+url.PathEscape(name), nil, &d)
		*out = d.Nodegroup

		return err
	})
}

// describeEKSNodeGroups describes names, eksNodeGroupsParallel at a time.
func describeEKSNodeGroups(names []string, describe func(string, *eksNodegroup) error) ([]managedNodeGroup, string, error) {
	answers := make([]eksNodegroup, len(names))
	errs := make([]error, len(names))
	slots := make(chan struct{}, eksNodeGroupsParallel)

	var wg sync.WaitGroup

	for i, name := range names {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()

			errs[i] = describe(name, &answers[i])
		})
	}

	wg.Wait()

	groups := []managedNodeGroup{}

	var (
		forbidden string
		firstErr  error
	)

	for i, err := range errs {
		switch {
		case unauthorized(err):
			forbidden = eksActionDescribeNodegroup
		case err != nil:
			firstErr = cmp.Or(firstErr, err)
		default:
			groups = append(groups, nodeGroupRow(answers[i], names[i]))
		}
	}

	slices.SortFunc(groups, func(a, b managedNodeGroup) int { return cmp.Compare(a.Name, b.Name) })

	return groups, forbidden, firstErr
}

func nodeGroupRow(g eksNodegroup, name string) managedNodeGroup {
	row := managedNodeGroup{
		Name: cmpOr(g.Name, name), Status: g.Status,
		Desired: g.ScalingConfig.Desired, Min: g.ScalingConfig.Min, Max: g.ScalingConfig.Max,
		Version: g.Version, ReleaseVersion: g.ReleaseVersion, AMIType: g.AMIType, CapacityType: g.CapacityType,
		InstanceTypes: g.InstanceTypes, DiskSize: g.DiskSize, Issues: []nodeGroupIssue{},
		Updating: g.Status == eksNodeGroupUpdating,
	}

	if row.InstanceTypes == nil {
		row.InstanceTypes = []string{}
	}

	row.AtMax = row.Max > 0 && row.Desired >= row.Max

	if g.CreatedAt > 0 {
		row.Created = int64(g.CreatedAt * 1000)
	}

	for _, issue := range g.Health.Issues {
		ids := issue.ResourceIDs
		if ids == nil {
			ids = []string{}
		}

		row.Issues = append(row.Issues, nodeGroupIssue{Code: issue.Code, Message: issue.Message, ResourceIDs: ids})
	}

	return row
}

// demoNodeGroupsFor is the demo's node groups: on the Kubernetes demo (an EKS cluster) the
// "system" group its first nodes carry and a "gpu" group stuck at its maximum because its
// instances do not launch; none on the Talos demo.
func demoNodeGroupsFor(target kubeTarget) func() nodeGroupsReport {
	return func() nodeGroupsReport {
		if !isKubeconfig(target.config) {
			return noNodeGroups()
		}

		return demoNodeGroups()
	}
}

func demoNodeGroups() nodeGroupsReport {
	created := time.Now().Add(-60 * 24 * time.Hour).UnixMilli()

	system := 0

	for _, n := range demoEKSNodes().Nodes {
		if n.PoolKind == nodeGroupsProviderEKS && n.Pool == "system" {
			system++
		}
	}

	return nodeGroupsReport{
		Provider: nodeGroupsProviderEKS, Installed: true, Region: "eu-west-1", Cluster: "ichor-demo",
		Groups: []managedNodeGroup{
			{
				Name: "gpu", Status: "DEGRADED", Desired: 2, Min: 0, Max: 2, Version: "1.34",
				ReleaseVersion: "1.34.1-20251016", AMIType: "AL2023_x86_64_NVIDIA", CapacityType: "ON_DEMAND",
				InstanceTypes: []string{"g6.xlarge"}, DiskSize: 100, Nodes: 0, AtMax: true,
				Issues: []nodeGroupIssue{{
					Code:        "AsgInstanceLaunchFailures",
					Message:     "Could not launch On-Demand Instances. InsufficientInstanceCapacity - We currently do not have sufficient g6.xlarge capacity in the Availability Zone you requested.",
					ResourceIDs: []string{"eks-gpu-a2c9d1e4-asg"},
				}},
				Created: created,
			},
			{
				Name: "system", Status: "ACTIVE", Desired: system, Min: 2, Max: 6, Version: "1.34",
				ReleaseVersion: "1.34.1-20251016", AMIType: "AL2023_x86_64_STANDARD", CapacityType: "ON_DEMAND",
				InstanceTypes: []string{"m7i.xlarge"}, DiskSize: 50, Nodes: system,
				Issues: []nodeGroupIssue{}, Created: created,
			},
		},
	}
}
