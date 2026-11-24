package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Upgrading an EKS managed node group (UpdateNodegroupVersion): to the control plane's
// Kubernetes version, or to the latest node image (AMI release) of the version it runs. The
// update it starts is followed with KubeNodeGroupUpdate, like a size change. AWS replaces
// every node of the group, evicting its pods; its own version rules are not re-implemented
// here, its refusals are passed through.

const (
	eksActionUpdateNodegroupVersion = "eks:UpdateNodegroupVersion"
	eksActionDescribeCluster        = "eks:DescribeCluster"
)

// KubeNodeGroupUpgrade starts the upgrade of the EKS managed node group: version "" keeps
// its Kubernetes version and takes the latest node image for it; otherwise version must be
// the control plane's. force replaces nodes even when a pod disruption budget refuses the
// eviction. Answers {"id","status","type","errors"} (see nodeGroupUpdate). Refused before
// any change for no group, another version, a group already updating, a cluster that does
// not sign in through EKS, and demo contexts. The POST is never retried. kubeServer: see
// KubePods.
func KubeNodeGroupUpgrade(configYAML, contextName, kubeServer, group, version string, force bool) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	group = privacy.reveal(strings.TrimSpace(group))
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")

	defer recordAction(&err, configYAML, contextName, auditAction{
		Server: kubeServer, Action: "upgrade-nodegroup", Object: "NodeGroup/" + group,
		Params: fmt.Sprintf("version=%s,force=%t", cmpOr(version, "latest"), force),
	})

	if group == "" {
		return "", errors.New("no node group named")
	}

	target := kubeTarget{configYAML, contextName, kubeServer}
	if isDemoContext(target.config, target.context) {
		return "", errDemoUnavailable
	}

	update, err := withKubeFrom(context.Background(), target, func(ctx context.Context, k *kubeClient) (nodeGroupUpdate, error) {
		return upgradeEKSNodeGroup(ctx, k, group, version, force)
	})
	if err != nil {
		return "", err
	}

	return toJSON(update)
}

func upgradeEKSNodeGroup(ctx context.Context, k *kubeClient, group, version string, force bool) (nodeGroupUpdate, error) {
	m, creds, err := eksSigner(ctx, k)
	if err != nil {
		return nodeGroupUpdate{}, err
	}

	// Only the control plane's version is offered: AWS words the other skews itself, but a
	// version the apps never offer is a mistake to stop before it replaces nodes.
	if version != "" {
		clusterVersion, err := eksClusterVersion(ctx, m, creds)
		if err != nil {
			return nodeGroupUpdate{}, eksReadError(err, eksActionDescribeCluster)
		}

		if version != clusterVersion {
			return nodeGroupUpdate{}, fmt.Errorf("the group can only be upgraded to the control plane's version (%s), not %s", clusterVersion, version)
		}
	}

	path := eksNodeGroupPath(m, group)

	if _, err := describeUpdatableNodeGroup(ctx, m, creds, path, "be upgraded"); err != nil {
		return nodeGroupUpdate{}, err
	}

	request := map[string]any{"force": force, "clientRequestToken": randomToken()}
	if version != "" {
		request["version"] = version
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nodeGroupUpdate{}, err
	}

	var answer struct {
		Update eksUpdate `json:"update"`
	}

	if err := eksCall(ctx, m, creds, http.MethodPost, path+"/update-version", nil, body, &answer); err != nil {
		return nodeGroupUpdate{}, eksUpdateError(err, eksActionUpdateNodegroupVersion)
	}

	return answer.Update.row(), nil
}

// eksClusterVersion is the Kubernetes version of m's control plane (DescribeCluster), "1.34".
func eksClusterVersion(ctx context.Context, m *eksMethod, creds awsCredentials) (string, error) {
	var answer struct {
		Cluster struct {
			Version string `json:"version"`
		} `json:"cluster"`
	}

	if err := eksCall(ctx, m, creds, http.MethodGet, "/clusters/"+url.PathEscape(m.cluster), nil, nil, &answer); err != nil {
		return "", err
	}

	return answer.Cluster.Version, nil
}

// withClusterVersion sets the report's control plane version and marks the groups whose
// Kubernetes minor is below it. The version is a bonus: a role that may not describe the
// cluster still sees its groups, none marked behind.
func withClusterVersion(report nodeGroupsReport, clusterVersion string) nodeGroupsReport {
	out := report
	out.ClusterVersion = clusterVersion
	out.Groups = make([]managedNodeGroup, len(report.Groups))

	for i, g := range report.Groups {
		g.Behind = kubeMinorBelow(g.Version, clusterVersion)
		out.Groups[i] = g
	}

	return out
}

// kubeMinorBelow reports whether version's major.minor is below of's; false when either
// cannot be read.
func kubeMinorBelow(version, of string) bool {
	major, minor, ok := kubeMinor(version)
	ofMajor, ofMinor, ofOK := kubeMinor(of)

	if !ok || !ofOK {
		return false
	}

	return major < ofMajor || (major == ofMajor && minor < ofMinor)
}
