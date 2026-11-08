package ichorgo

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// mapGarageOperator is the pure half of readGarageOperator, for the tests: the CRs mapped,
// matched to each other and sorted worst health first.
func mapGarageOperator(clusters []garageOpClusterObject, buckets []garageOpBucketObject, keys []garageOpKeyObject, nodes []garageOpNodeObject, deployments []garageOpDeploymentObject, now time.Time) garageOperatorLayer {
	out := newGarageOperatorLayer()

	for _, obj := range nodes {
		out.Nodes = append(out.Nodes, mapGarageOpNode(obj, now))
	}

	for _, obj := range clusters {
		out.Clusters = append(out.Clusters, mapGarageOpCluster(obj, out.Nodes))
	}

	for _, obj := range buckets {
		out.Buckets = append(out.Buckets, mapGarageOpBucket(obj))
	}

	for _, obj := range keys {
		out.Keys = append(out.Keys, mapGarageOpKey(obj))
	}

	out.Operator = mapGarageOperatorDeployment(deployments)

	slices.SortFunc(out.Clusters, byHealthThenKey(func(x garageOpCluster) (string, string) { return x.Health, x.Namespace + "/" + x.Name }))
	slices.SortFunc(out.Buckets, byHealthThenKey(func(x garageOpBucket) (string, string) { return x.Health, x.Namespace + "/" + x.Name }))
	slices.SortFunc(out.Keys, byHealthThenKey(func(x garageOpKey) (string, string) { return x.Health, x.Namespace + "/" + x.Name }))
	slices.SortFunc(out.Nodes, byHealthThenKey(func(x garageOpNode) (string, string) { return x.Health, x.Namespace + "/" + x.Name }))

	return *out
}

func mapGarageOpCluster(obj garageOpClusterObject, nodes []garageOpNode) garageOpCluster {
	st := obj.Status

	c := garageOpCluster{
		Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name,
		Phase: st.Phase, ClusterID: st.ClusterID,
		Replicas: st.Replicas, ReadyReplicas: st.ReadyReplicas,
		StorageReplicas: st.StorageReplicas, StorageReady: st.StorageReadyReplicas,
		GatewayReplicas: st.GatewayReplicas, GatewayReady: st.GatewayReadyReplicas,
		LayoutVersion: st.LayoutVersion, Reasons: []string{},
		Tombstones: []string{}, Conditions: []garageOpCondition{},
	}

	if st.BuildInfo != nil {
		c.GarageVersion = st.BuildInfo.Version
	}

	if st.StagedLayoutVersion != nil {
		c.StagedLayout = *st.StagedLayoutVersion
	}

	c.Tombstones = st.PendingGatewayTombstones
	if c.Tombstones == nil {
		c.Tombstones = []string{}
	}
	c.Conditions = garageOpKeepConditions(st.Conditions, garageOpClusterConds)

	// The node CRs of this cluster: a disconnected or roleless one is the cluster's problem.
	disconnected, notInLayout := false, false
	for _, n := range nodes {
		if cmp.Or(n.clusterNamespace, n.Namespace) != c.Namespace || n.Cluster != c.Name {
			continue
		}

		if !n.Connected {
			disconnected = true
		}

		if !n.InLayout {
			notInLayout = true
		}
	}

	reasons := []string{}
	critical := false

	if c.Phase == "Failed" || c.condition("Error").Status == "True" {
		reasons, critical = append(reasons, garageOpReasonClusterFailed), true
	}

	if c.condition("ManagementHandleReady").Status == "False" {
		reasons, critical = append(reasons, garageOpReasonAdminUnreachable), true
	}

	if c.condition("QuorumAtRisk").Status == "True" {
		reasons = append(reasons, garageOpReasonQuorumAtRisk)
	}

	if c.Phase == "Degraded" {
		reasons = append(reasons, garageOpReasonDegraded)
	}

	if c.StorageReplicas > 0 && c.StorageReady < c.StorageReplicas {
		reasons = append(reasons, garageOpReasonStorageReplicas)
	}

	if c.StagedLayout > 0 && c.StagedLayout != c.LayoutVersion {
		reasons = append(reasons, garageOpReasonLayoutStaged)
	}

	if len(c.Tombstones) > 0 || c.condition("GatewayTombstones").Status == "True" {
		reasons = append(reasons, garageOpReasonTombstones)
	}

	if disconnected {
		reasons = append(reasons, garageOpReasonNodeDisconnected)
	}

	if notInLayout {
		reasons = append(reasons, garageOpReasonNodeNotInLayout)
	}

	c.Reasons, c.Health = reasons, healthOK
	if len(reasons) > 0 {
		c.Health = healthWarning
	}

	if critical {
		c.Health = healthCritical
	}

	return c
}

// condition is one of the cluster's kept conditions, by type; zero when absent.
func (c *garageOpCluster) condition(typ string) garageOpCondition {
	for _, cond := range c.Conditions {
		if cond.Type == typ {
			return cond
		}
	}

	return garageOpCondition{}
}

func mapGarageOpBucket(obj garageOpBucketObject) garageOpBucket {
	st := obj.Status

	b := garageOpBucket{
		Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name,
		Cluster: obj.Spec.ClusterRef.Name,
		Phase:   st.Phase, BucketID: st.BucketID,
		Size: st.Size, Website: st.WebsiteEnabled, WebsiteURL: st.WebsiteURL,
		Keys: len(st.Keys), Reasons: []string{}, Conditions: []garageOpCondition{},
	}

	if obj.Spec.Quotas != nil {
		b.MaxSize = obj.Spec.Quotas.MaxSize
	}

	if st.QuotaUsage != nil {
		b.Quota = &garageOpQuotaUsage{
			SizeBytes: st.QuotaUsage.SizeBytes, SizeLimit: st.QuotaUsage.SizeLimit, SizePercent: st.QuotaUsage.SizePercent,
			Objects: st.QuotaUsage.ObjectCount, ObjectLimit: st.QuotaUsage.ObjectLimit, ObjectPercent: st.QuotaUsage.ObjectPercent,
		}
	}

	b.Conditions = garageOpKeepConditions(st.Conditions, garageOpBucketConds)

	reasons := []string{}
	critical := false

	if b.Phase == "Failed" || bucketCondition(st.Conditions, "Error").Status == "True" {
		reasons, critical = append(reasons, garageOpReasonBucketFailed), true
	}

	if bucketCondition(st.Conditions, "LifecycleConfigured").Status == "False" {
		reasons = append(reasons, garageOpReasonLifecycle)
	}

	if bucketCondition(st.Conditions, "BucketLookupStuck").Status == "True" {
		reasons = append(reasons, garageOpReasonLookupStuck)
	}

	if bucketCondition(st.Conditions, "BucketMetadataDegraded").Status == "True" {
		reasons = append(reasons, garageOpReasonMetadataDegraded)
	}

	if bucketCondition(st.Conditions, "DeletionBlocked").Status == "True" {
		reasons = append(reasons, garageOpReasonDeletionBlocked)
	}

	if bucketCondition(st.Conditions, "WebsiteExposed").Status == "False" {
		reasons = append(reasons, garageOpReasonWebsiteExposure)
	}

	b.Reasons, b.Health = reasons, healthOK
	if len(reasons) > 0 {
		b.Health = healthWarning
	}

	if critical {
		b.Health = healthCritical
	}

	return b
}

// bucketCondition is one of the bucket object's raw conditions, by type; zero when absent.
func bucketCondition(conds kubeConditions, typ string) kubeCondition {
	return conds.get(typ)
}

func mapGarageOpKey(obj garageOpKeyObject) garageOpKey {
	st := obj.Status

	k := garageOpKey{
		Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name,
		Cluster: obj.Spec.ClusterRef.Name,
		Phase:   st.Phase, KeyID: st.KeyID,
		ClusterWide: st.ClusterWide, Buckets: []garageOpKeyPermission{}, Reasons: []string{},
	}

	if obj.Spec.AllBuckets != nil {
		k.AllBuckets = &struct {
			Read  bool `json:"read"`
			Write bool `json:"write"`
			Owner bool `json:"owner"`
		}{Read: obj.Spec.AllBuckets.Read, Write: obj.Spec.AllBuckets.Write, Owner: obj.Spec.AllBuckets.Owner}
	}

	for _, p := range obj.Spec.BucketPermissions {
		// A permission names its bucket by bucketRef, bucketId or globalAlias: any may be absent.
		name := cmp.Or(p.BucketID, p.GlobalAlias)
		if p.BucketRef != nil && p.BucketRef.Name != "" {
			name = p.BucketRef.Name
		}

		k.Buckets = append(k.Buckets, garageOpKeyPermission{Bucket: name, Read: p.Read, Write: p.Write, Owner: p.Owner})
	}

	k.BucketsTotal = len(k.Buckets)
	if len(k.Buckets) > garageOpKeyPermCap {
		k.Buckets = k.Buckets[:garageOpKeyPermCap]
	}

	k.Reasons, k.Health = []string{}, healthOK
	if k.Phase == "Failed" || st.Conditions.get("Error").Status == "True" {
		k.Reasons = append(k.Reasons, garageOpReasonKeyFailed)
		k.Health = healthCritical
	}

	return k
}

func mapGarageOpNode(obj garageOpNodeObject, now time.Time) garageOpNode {
	st := obj.Status

	n := garageOpNode{
		Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name,
		Cluster: obj.Spec.ClusterRef.Name, clusterNamespace: obj.Spec.ClusterRef.Namespace,
		Phase:  st.Phase,
		NodeID: cmp.Or(st.NodeID, obj.Spec.NodeID), Zone: cmp.Or(st.Zone, obj.Spec.Zone),
		Capacity: obj.Spec.Capacity, Gateway: obj.Spec.Gateway,
		Connected: st.Connected, InLayout: st.InLayout, LastSeen: -1, Reasons: []string{},
	}

	if !n.Connected {
		if ms := unixMilli(st.LastSeen); ms > 0 {
			n.LastSeen = max(0, (now.UnixMilli()-ms)/1000)
		}
	}

	reasons := []string{}
	if !n.Connected {
		reasons = append(reasons, garageOpReasonNodeDisconnected)
	}

	if !n.InLayout {
		reasons = append(reasons, garageOpReasonNodeNotInLayout)
	}

	n.Reasons, n.Health = reasons, healthOK
	if len(reasons) > 0 {
		n.Health = healthWarning
	}

	return n
}

// isGarageOperatorDeployment reports a Deployment running an image the catalog knows as
// garage-operator.
func isGarageOperatorDeployment(catalog *appCatalog) func(garageOpDeploymentObject) bool {
	return func(d garageOpDeploymentObject) bool {
		for _, c := range d.Spec.Template.Spec.Containers {
			if id := catalog.identify(parseImageRef(c.Image)); id.app != nil && id.app.ID == "garage-operator" {
				return true
			}
		}

		return false
	}
}

// mapGarageOperatorDeployment reads the operator's own health from the Deployments whose
// image the catalog knows as garage-operator. nil when none is running (the CRs may still
// be served by an operator running outside Kubernetes).
func mapGarageOperatorDeployment(deployments []garageOpDeploymentObject) *garageOperatorStatus {
	isOperator := isGarageOperatorDeployment(loadAppCatalog())

	var matched []garageOpDeploymentObject

	for _, d := range deployments {
		if isOperator(d) {
			matched = append(matched, d)
		}
	}

	if len(matched) == 0 {
		return nil
	}

	slices.SortFunc(matched, func(a, b garageOpDeploymentObject) int {
		return strings.Compare(a.Metadata.Namespace+"/"+a.Metadata.Name, b.Metadata.Namespace+"/"+b.Metadata.Name)
	})

	d := matched[0]
	op := &garageOperatorStatus{
		Namespace: d.Metadata.Namespace, Name: d.Metadata.Name,
		Ready: d.Status.ReadyReplicas, Desired: d.Status.Replicas, Reasons: []string{},
	}

	op.Health = healthOK
	if op.Ready < op.Desired {
		op.Health = healthCritical
		op.Reasons = append(op.Reasons, garageOpReasonOperatorDown)
	}

	return op
}
