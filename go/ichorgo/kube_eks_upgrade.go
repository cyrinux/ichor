package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Upgrading an EKS control plane one Kubernetes minor (UpdateClusterVersion): a plan that says
// what the upgrade will do and what stops it, the upgrade itself, and the update AWS runs for
// it (DescribeUpdate), with the credentials of the cluster's own sign-in. AWS runs the
// upgrade (20 to 40 minutes); it cannot be undone or cancelled, so only the plan's next minor
// is ever sent.

const (
	eksActionUpdateClusterVersion    = "eks:UpdateClusterVersion"
	eksActionDescribeClusterVersions = "eks:DescribeClusterVersions"
	eksActionListUpdates             = "eks:ListUpdates"

	eksClusterActive   = "ACTIVE"
	eksClusterUpdating = "UPDATING"

	// eksUpdateRunning is AWS' own status of an update still running.
	eksUpdateRunning = "InProgress"

	// eksUpdatesDescribed caps the DescribeUpdate calls made to find the update in progress.
	eksUpdatesDescribed = 20

	eksBlockerNotActive    = "the cluster is %s: only an active cluster can be upgraded"
	eksBlockerUpdating     = "an update is in progress on the cluster: wait for it to finish"
	eksBlockerGroupsBehind = "node groups are behind the current version: upgrade them first"
)

// eksUpgradePlan is what upgrading the control plane one minor would do. Next is the next
// minor ("" when AWS lists none: up to date); NextKnown says AWS listed it (false when
// DescribeClusterVersions was refused: availability not checked). SupportEnds is the end of
// standard support of Version, unix ms (0 when unknown). Updating is the update in progress,
// found again when the app is reopened. Blockers say why the upgrade may not start now.
// Groups are every managed node group: each is behind once the control plane moves.
type eksUpgradePlan struct {
	Installed       bool             `json:"installed"`
	Cluster         string           `json:"cluster"`
	Region          string           `json:"region"`
	Version         string           `json:"version"`
	Status          string           `json:"status"`
	PlatformVersion string           `json:"platformVersion"`
	Next            string           `json:"next"`
	NextKnown       bool             `json:"nextKnown"`
	SupportEnds     int64            `json:"supportEnds"`
	Updating        *nodeGroupUpdate `json:"updating"`
	GroupsBehind    []string         `json:"groupsBehind"`
	Groups          []string         `json:"groups"`
	Blockers        []string         `json:"blockers"`
	Error           string           `json:"error"`
	Forbidden       string           `json:"forbidden"`
}

var errNotEKSCluster = errors.New("this cluster does not sign in through EKS: its control plane cannot be upgraded from here")

func noEKSUpgradePlan() eksUpgradePlan {
	return eksUpgradePlan{GroupsBehind: []string{}, Groups: []string{}, Blockers: []string{}}
}

// KubeEKSUpgradePlan reads what upgrading the EKS control plane one minor would do (see
// eksUpgradePlan). A cluster that does not sign in through EKS answers installed false
// without any call to AWS; a refused read is the plan's error line. kubeServer: see KubePods.
func KubeEKSUpgradePlan(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	target := kubeTarget{configYAML, contextName, kubeServer}

	return kubeReadJSON(target, demoEKSUpgradePlanFor(target), func(ctx context.Context, k *kubeClient) (eksUpgradePlan, error) {
		if k.tokens == nil {
			return noEKSUpgradePlan(), nil
		}

		if _, ok := k.tokens.method.(*eksMethod); !ok {
			return noEKSUpgradePlan(), nil
		}

		m, creds, err := eksSigner(ctx, k)
		if err != nil {
			plan := noEKSUpgradePlan()
			plan.Installed, plan.Error = true, err.Error()

			return plan, nil
		}

		return readEKSUpgradePlan(ctx, m, creds), nil
	})
}

// KubeEKSUpgrade starts the upgrade of the EKS control plane to version, which must be the
// plan's next minor: {"id","status","type","errors"} of the update AWS starts (see
// nodeGroupUpdate). Refused before any change for another version, any blocker of the plan,
// a cluster that does not sign in through EKS, and demo contexts. It cannot be undone or
// cancelled; the POST is never retried. kubeServer: see KubePods.
func KubeEKSUpgrade(configYAML, contextName, kubeServer, version string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")

	defer recordAction(&err, configYAML, contextName, auditAction{
		Server: kubeServer, Action: "upgrade-eks-control-plane", Object: "Cluster/" + eksClusterName(configYAML, contextName),
		Params: "version=" + version,
	})

	if version == "" {
		return "", errors.New("no version named")
	}

	target := kubeTarget{configYAML, contextName, kubeServer}
	if isDemoContext(target.config, target.context) {
		return "", errDemoUnavailable
	}

	update, err := withKubeFrom(context.Background(), target, func(ctx context.Context, k *kubeClient) (nodeGroupUpdate, error) {
		return upgradeEKSControlPlane(ctx, k, version)
	})
	if err != nil {
		return "", err
	}

	return toJSON(update)
}

// KubeEKSUpdate reads the cluster-level update updateID (a control plane upgrade):
// {"id","status","type","errors"} (see nodeGroupUpdate). The demo answers a finished update.
// kubeServer: see KubePods.
func KubeEKSUpdate(configYAML, contextName, kubeServer, updateID string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	updateID = strings.TrimSpace(updateID)
	target := kubeTarget{configYAML, contextName, kubeServer}

	demo := func() nodeGroupUpdate {
		return nodeGroupUpdate{ID: updateID, Status: eksUpdateSuccessful, Type: "VersionUpdate", Errors: []nodeGroupIssue{}}
	}

	return kubeReadJSON(target, demo, func(ctx context.Context, k *kubeClient) (nodeGroupUpdate, error) {
		if updateID == "" {
			return nodeGroupUpdate{}, errors.New("no update named")
		}

		m, creds, err := eksSigner(ctx, k)
		if err != nil {
			return nodeGroupUpdate{}, err
		}

		return describeEKSUpdate(ctx, m, creds, updateID, "")
	})
}

func upgradeEKSControlPlane(ctx context.Context, k *kubeClient, version string) (nodeGroupUpdate, error) {
	m, creds, err := eksSigner(ctx, k)
	if errors.Is(err, errNotEKSNodeGroups) {
		return nodeGroupUpdate{}, errNotEKSCluster
	} else if err != nil {
		return nodeGroupUpdate{}, err
	}

	plan := readEKSUpgradePlan(ctx, m, creds)

	switch {
	case plan.Error != "":
		return nodeGroupUpdate{}, errors.New(plan.Error)
	case plan.Next == "":
		return nodeGroupUpdate{}, fmt.Errorf("no newer Kubernetes version is available for this cluster (%s)", plan.Version)
	case version != plan.Next:
		return nodeGroupUpdate{}, fmt.Errorf("the control plane can only be upgraded one minor at a time, to %s, not %s", plan.Next, version)
	case len(plan.Blockers) > 0:
		return nodeGroupUpdate{}, errors.New(plan.Blockers[0])
	}

	body, err := json.Marshal(map[string]string{"version": version, "clientRequestToken": randomToken()})
	if err != nil {
		return nodeGroupUpdate{}, err
	}

	var answer struct {
		Update eksUpdate `json:"update"`
	}

	path := "/clusters/" + url.PathEscape(m.cluster) + "/updates"
	if err := eksCall(ctx, m, creds, http.MethodPost, path, nil, body, &answer); err != nil {
		return nodeGroupUpdate{}, eksClusterUpdateError(err)
	}

	return answer.Update.row(), nil
}

// eksClusterUpdateError words AWS' answer to UpdateClusterVersion, as eksUpdateError does
// for a node group, but a conflict is the cluster's.
func eksClusterUpdateError(err error) error {
	var e *cloudHTTPError
	if errors.As(err, &e) && e.status == http.StatusConflict {
		return errors.New("another update is running on this cluster")
	}

	return eksUpdateError(err, eksActionUpdateClusterVersion)
}

// readEKSUpgradePlan builds the plan of m's cluster. Only DescribeCluster is needed: the
// supported versions, the node groups and the update in progress are each a bonus whose
// refusal leaves its part unknown.
func readEKSUpgradePlan(ctx context.Context, m *eksMethod, creds awsCredentials) eksUpgradePlan {
	plan := noEKSUpgradePlan()
	plan.Installed, plan.Cluster, plan.Region = true, m.cluster, m.region

	cluster, err := eksDescribeCluster(ctx, m, creds)
	if err != nil {
		if unauthorized(err) {
			plan.Forbidden = eksActionDescribeCluster
		}

		plan.Error = eksReadError(err, eksActionDescribeCluster).Error()

		return plan
	}

	plan.Version, plan.Status, plan.PlatformVersion = cluster.Version, cluster.Status, cluster.PlatformVersion
	plan.Next = nextKubeMinor(cluster.Version)

	if versions, err := eksSupportedVersions(ctx, m, creds); err == nil {
		plan = withSupportedVersions(plan, versions)
	}

	if groups, _, _ := listEKSNodeGroups(ctx, m, creds); len(groups) > 0 { //nolint:errcheck // a bonus, see above
		for _, g := range groups {
			plan.Groups = append(plan.Groups, g.Name)
			if kubeMinorBelow(g.Version, cluster.Version) {
				plan.GroupsBehind = append(plan.GroupsBehind, g.Name)
			}
		}
	}

	if cluster.Status == eksClusterUpdating {
		plan.Updating = findEKSUpdateInProgress(ctx, m, creds)
	}

	plan.Blockers = eksUpgradeBlockers(plan)

	return plan
}

// eksUpgradeBlockers are the reasons the plan's upgrade may not start now.
func eksUpgradeBlockers(plan eksUpgradePlan) []string {
	blockers := []string{}

	switch {
	case plan.Updating != nil || plan.Status == eksClusterUpdating:
		blockers = append(blockers, eksBlockerUpdating)
	case plan.Status != eksClusterActive:
		blockers = append(blockers, fmt.Sprintf(eksBlockerNotActive, strings.ToLower(plan.Status)))
	}

	if len(plan.GroupsBehind) > 0 {
		blockers = append(blockers, eksBlockerGroupsBehind)
	}

	return blockers
}

type eksClusterInfo struct {
	Version         string `json:"version"`
	Status          string `json:"status"`
	PlatformVersion string `json:"platformVersion"`
}

// eksDescribeCluster is m's cluster as DescribeCluster has it.
func eksDescribeCluster(ctx context.Context, m *eksMethod, creds awsCredentials) (eksClusterInfo, error) {
	var answer struct {
		Cluster eksClusterInfo `json:"cluster"`
	}

	err := eksCall(ctx, m, creds, http.MethodGet, "/clusters/"+url.PathEscape(m.cluster), nil, nil, &answer)

	return answer.Cluster, err
}

// eksClusterVersionInfo is one Kubernetes version EKS supports (DescribeClusterVersions); the
// dates are epoch seconds (AWS REST JSON timestamps).
type eksClusterVersionInfo struct {
	Version           string  `json:"clusterVersion"`
	EndOfStandardDate float64 `json:"endOfStandardSupportDate"`
}

// eksSupportedVersions lists the Kubernetes versions EKS offers in m's region, every page.
func eksSupportedVersions(ctx context.Context, m *eksMethod, creds awsCredentials) ([]eksClusterVersionInfo, error) {
	var versions []eksClusterVersionInfo

	next := ""

	for range eksMaxPages {
		q := url.Values{"clusterType": {"eks"}, "maxResults": {"100"}}
		if next != "" {
			q.Set("nextToken", next)
		}

		var page struct {
			ClusterVersions []eksClusterVersionInfo `json:"clusterVersions"`
			NextToken       string                  `json:"nextToken"`
		}

		if err := eksCall(ctx, m, creds, http.MethodGet, "/cluster-versions", q, nil, &page); err != nil {
			return nil, err
		}

		versions = append(versions, page.ClusterVersions...)

		if next = page.NextToken; next == "" {
			break
		}
	}

	return versions, nil
}

// withSupportedVersions sets what AWS says of the plan's versions: whether the next minor is
// offered (none listed: up to date) and when standard support of the current one ends.
func withSupportedVersions(plan eksUpgradePlan, versions []eksClusterVersionInfo) eksUpgradePlan {
	out := plan
	out.NextKnown = false

	offered := false

	for _, v := range versions {
		switch {
		case sameKubeMinor(v.Version, plan.Next):
			offered = true
		case sameKubeMinor(v.Version, plan.Version) && v.EndOfStandardDate > 0:
			out.SupportEnds = int64(v.EndOfStandardDate * 1000)
		}
	}

	if offered {
		out.NextKnown = true
	} else {
		out.Next = ""
	}

	return out
}

// findEKSUpdateInProgress finds the cluster-level update in progress (ListUpdates, then
// DescribeUpdate from the newest id), nil when none is found or the calls are refused.
func findEKSUpdateInProgress(ctx context.Context, m *eksMethod, creds awsCredentials) *nodeGroupUpdate {
	var ids []string

	next := ""

	for range eksMaxPages {
		q := url.Values{"maxResults": {"100"}}
		if next != "" {
			q.Set("nextToken", next)
		}

		var page struct {
			UpdateIDs []string `json:"updateIds"`
			NextToken string   `json:"nextToken"`
		}

		if err := eksCall(ctx, m, creds, http.MethodGet, "/clusters/"+url.PathEscape(m.cluster)+"/updates", q, nil, &page); err != nil {
			return nil
		}

		ids = append(ids, page.UpdateIDs...)

		if next = page.NextToken; next == "" {
			break
		}
	}

	for i, described := len(ids)-1, 0; i >= 0 && described < eksUpdatesDescribed; i, described = i-1, described+1 {
		u, err := describeEKSUpdate(ctx, m, creds, ids[i], "")
		if err == nil && u.Status == eksUpdateRunning {
			return &u
		}
	}

	return nil
}

// describeEKSUpdate reads the update id of m's cluster: of the node group group, or of the
// cluster itself when group is "".
func describeEKSUpdate(ctx context.Context, m *eksMethod, creds awsCredentials, id, group string) (nodeGroupUpdate, error) {
	path := "/clusters/" + url.PathEscape(m.cluster) + "/updates/" + url.PathEscape(id)

	var query url.Values
	if group != "" {
		query = url.Values{"nodegroupName": {group}}
	}

	var answer struct {
		Update eksUpdate `json:"update"`
	}

	if err := eksCall(ctx, m, creds, http.MethodGet, path, query, nil, &answer); err != nil {
		return nodeGroupUpdate{}, eksReadError(err, eksActionDescribeUpdate)
	}

	return answer.Update.row(), nil
}

// eksClusterName is the EKS cluster the stored context signs in to, "" when it does not sign
// in through EKS (the audit entry's object, known before any call).
func eksClusterName(configYAML, contextName string) string {
	c, err := signInContext(configYAML, contextName)
	if err != nil {
		return ""
	}

	if m, ok := c.method.(*eksMethod); ok {
		return m.cluster
	}

	return ""
}

// nextKubeMinor is the minor after version ("1.31" → "1.32"), "" when it cannot be read.
func nextKubeMinor(version string) string {
	major, minor, ok := kubeMinor(version)
	if !ok {
		return ""
	}

	return strconv.Itoa(major) + "." + strconv.Itoa(minor+1)
}

// sameKubeMinor reports whether a and b name the same major.minor.
func sameKubeMinor(a, b string) bool {
	aMajor, aMinor, aOK := kubeMinor(a)
	bMajor, bMinor, bOK := kubeMinor(b)

	return aOK && bOK && aMajor == bMajor && aMinor == bMinor
}

// demoEKSUpgradePlanFor is the demo's plan: on the Kubernetes demo (an EKS cluster) one minor
// available and its "gpu" group behind as a blocker; none on the Talos demo.
func demoEKSUpgradePlanFor(target kubeTarget) func() eksUpgradePlan {
	return func() eksUpgradePlan {
		if !isKubeconfig(target.config) {
			return noEKSUpgradePlan()
		}

		return demoEKSUpgradePlan(time.Now())
	}
}

func demoEKSUpgradePlan(now time.Time) eksUpgradePlan {
	groups := demoNodeGroups(now)

	plan := noEKSUpgradePlan()
	plan.Installed, plan.Cluster, plan.Region = true, groups.Cluster, groups.Region
	plan.Version, plan.Status, plan.PlatformVersion = groups.ClusterVersion, eksClusterActive, "eks.9"
	plan.Next, plan.NextKnown = nextKubeMinor(groups.ClusterVersion), true
	plan.SupportEnds = now.Add(200 * 24 * time.Hour).UnixMilli()

	for _, g := range groups.Groups {
		plan.Groups = append(plan.Groups, g.Name)
		if g.Behind {
			plan.GroupsBehind = append(plan.GroupsBehind, g.Name)
		}
	}

	plan.Blockers = eksUpgradeBlockers(plan)

	return plan
}
