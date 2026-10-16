package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

// groupGarageOperator is the third-party Garage operator's API group
// (rajsinghtech/garage-operator), not Garage's own Helm chart. GarageCluster is served at
// v1beta2 (preferred) with v1beta1 deprecated-but-served; GarageBucket, GarageKey and
// GarageNode are v1beta1 only. The operator's own chart also installs a GarageNode kind in
// deuxfleurs.fr (peer discovery): never ask that group, match on this one.
const (
	groupGarageOperator = "garage.rajsingh.info"

	garageOpClusterV2 = "v1beta2"
	garageOpV1        = "v1beta1"
)

// garageOpCap caps each kind's listing: the section is one answer for the phone, and a
// runaway operator must not size it. A kind that reaches the cap sets truncated.
const garageOpCap = 200

// garageOperatorLayer is the operator's declarative layer: the custom resources of group
// garage.rajsingh.info and the operator Deployment's own health. It is the probe's answer
// and what readGarageSection merges into the garage section of KubeDataServices.
type garageOperatorLayer struct {
	Version   string                `json:"version,omitempty"` // the version garageclusters were read at
	Error     string                `json:"error"`
	Clusters  []garageOpCluster     `json:"clusters"`
	Buckets   []garageOpBucket      `json:"buckets"`
	Keys      []garageOpKey         `json:"keys"`
	Nodes     []garageOpNode        `json:"nodes"`
	Operator  *garageOperatorStatus `json:"operator,omitempty"`
	Truncated bool                  `json:"truncated,omitempty"`
}

// garageOpCondition is one of the operator's conditions the section keeps: a problem or a
// transition, with the operator's own reason and message.
type garageOpCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Time    int64  `json:"time"` // unix ms of lastTransitionTime, 0 when unknown
}

type garageOpCluster struct {
	Namespace       string              `json:"namespace"`
	Name            string              `json:"name"`
	Phase           string              `json:"phase"` // Pending..Failed, the operator's own word
	ClusterID       string              `json:"clusterId"`
	GarageVersion   string              `json:"garageVersion"`
	Replicas        int                 `json:"replicas"`
	ReadyReplicas   int                 `json:"readyReplicas"`
	StorageReplicas int                 `json:"storageReplicas"`
	StorageReady    int                 `json:"storageReadyReplicas"`
	GatewayReplicas int                 `json:"gatewayReplicas"`
	GatewayReady    int                 `json:"gatewayReadyReplicas"`
	LayoutVersion   int64               `json:"layoutVersion"`
	StagedLayout    int64               `json:"stagedLayoutVersion"` // 0 when nothing is staged
	Tombstones      []string            `json:"pendingGatewayTombstones"`
	Conditions      []garageOpCondition `json:"conditions"`
	Health          string              `json:"health"`
	Reasons         []string            `json:"reasons"`
}

type garageOpQuotaUsage struct {
	SizeBytes     int64 `json:"sizeBytes"`
	SizeLimit     int64 `json:"sizeLimit"` // 0 = unlimited
	SizePercent   int   `json:"sizePercent"`
	Objects       int64 `json:"objectCount"`
	ObjectLimit   int64 `json:"objectLimit"` // 0 = unlimited
	ObjectPercent int   `json:"objectPercent"`
}

type garageOpBucket struct {
	Namespace  string              `json:"namespace"`
	Name       string              `json:"name"`
	Cluster    string              `json:"cluster"` // spec.clusterRef.name
	Phase      string              `json:"phase"`
	BucketID   string              `json:"bucketId"`
	Size       string              `json:"size"` // the operator's own wording
	MaxSize    string              `json:"maxSizeQuota"`
	Quota      *garageOpQuotaUsage `json:"quotaUsage"`
	Website    bool                `json:"websiteEnabled"`
	WebsiteURL string              `json:"websiteUrl"`
	Keys       int                 `json:"keysWithAccess"`
	Conditions []garageOpCondition `json:"conditions"`
	Health     string              `json:"health"`
	Reasons    []string            `json:"reasons"`
}

type garageOpKeyPermission struct {
	Bucket string `json:"bucket"` // bucketRef.name, or the bucket id / global alias
	Read   bool   `json:"read"`
	Write  bool   `json:"write"`
	Owner  bool   `json:"owner"`
}

type garageOpKey struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Cluster     string `json:"cluster"`
	Phase       string `json:"phase"`
	KeyID       string `json:"keyId"` // Garage's internal key id, not the S3 access key
	ClusterWide bool   `json:"clusterWide"`
	AllBuckets  *struct {
		Read  bool `json:"read"`
		Write bool `json:"write"`
		Owner bool `json:"owner"`
	} `json:"allBuckets"`
	Buckets      []garageOpKeyPermission `json:"bucketPermissions"` // capped, see garageOpKeyPermCap
	BucketsTotal int                     `json:"bucketPermissionsTotal"`
	Health       string                  `json:"health"`
	Reasons      []string                `json:"reasons"`
}

// garageOpKeyPermCap caps the per-bucket permissions listed; BucketsTotal tells the app
// how many more there are ("+n more").
const garageOpKeyPermCap = 20

type garageOpNode struct {
	Namespace        string   `json:"namespace"`
	Name             string   `json:"name"`
	Cluster          string   `json:"cluster"`
	clusterNamespace string   `json:"-"` // spec.clusterRef.namespace, "" when the node's own
	Phase            string   `json:"phase"`
	NodeID           string   `json:"nodeId"`
	Zone             string   `json:"zone"` // the effective one (spec.zone or zoneFrom)
	Capacity         string   `json:"capacity"`
	Gateway          bool     `json:"gateway"`
	Connected        bool     `json:"connected"`
	InLayout         bool     `json:"inLayout"`
	LastSeen         int64    `json:"lastSeenSecs"` // seconds ago, -1 when connected or unknown
	Health           string   `json:"health"`
	Reasons          []string `json:"reasons"`
}

// garageOperatorStatus is the operator Deployment's own health.
type garageOperatorStatus struct {
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	Ready     int      `json:"ready"`
	Desired   int      `json:"desired"`
	Health    string   `json:"health"`
	Reasons   []string `json:"reasons"`
}

// Reasons an operator item is not ok, for the app to word.
const (
	garageOpReasonClusterFailed    = "clusterFailed"       // phase Failed or the Error condition True
	garageOpReasonAdminUnreachable = "adminUnreachable"    // a connectTo handle cannot reach the external Admin API
	garageOpReasonOperatorDown     = "operatorDown"        // the operator Deployment is not ready
	garageOpReasonDegraded         = "degraded"            // phase Degraded
	garageOpReasonStorageReplicas  = "storageReplicas"     // fewer ready storage identities than desired
	garageOpReasonQuorumAtRisk     = "quorumAtRisk"        // partitions without write quorum: writes block
	garageOpReasonLayoutStaged     = "layoutStaged"        // a staged layout waits to be applied
	garageOpReasonTombstones       = "tombstones"          // stale gateway layout entries pending removal
	garageOpReasonNodeDisconnected = "nodeDisconnected"    // a node CR lost its Garage connection
	garageOpReasonNodeNotInLayout  = "nodeNotInLayout"     // a node CR holds no layout role
	garageOpReasonBucketFailed     = "bucketFailed"        // phase Failed or the Error condition True
	garageOpReasonLifecycle        = "lifecycleNotApplied" // the rules were dropped by a too-old Garage
	garageOpReasonLookupStuck      = "lookupStuck"         // the Admin API keeps timing out on this bucket
	garageOpReasonMetadataDegraded = "metadataDegraded"    // Garage cannot decode the bucket's metadata
	garageOpReasonDeletionBlocked  = "deletionBlocked"     // deletion is held to protect the content
	garageOpReasonWebsiteExposure  = "websiteExposure"     // the Ingress/HTTPRoute exposure is not reconciled
	garageOpReasonKeyFailed        = "keyFailed"           // phase Failed or the Error condition True
)

// The conditions the section keeps: the watched types whose status is not True (a problem
// or a transition), plus the ones whose True status IS the problem.
var (
	garageOpClusterConds = []string{
		"ClusterHealthy", "LayoutApplied", "NodesConnected", "StatefulSetReady", "ServicesReady",
		"GatewayConnected", "PublicEndpointReady", "GatewayTombstones", "Error",
		"ManagementHandleReady", "QuorumAtRisk",
	}
	garageOpBucketConds = []string{
		"LifecycleConfigured", "WebsiteExposed", "Error",
		"BucketLookupStuck", "BucketMetadataDegraded", "DeletionBlocked",
	}
)

// garageOpCondTrue are the watched conditions whose True status is the problem; the rest
// are kept when they are not True.
var garageOpCondTrue = map[string]bool{
	"QuorumAtRisk": true, "BucketLookupStuck": true, "BucketMetadataDegraded": true, "DeletionBlocked": true,
}

// garageOpKeepConditions filters an object's conditions down to the watched types worth
// showing, with their reason, message and transition time.
func garageOpKeepConditions(conds kubeConditions, watched []string) []garageOpCondition {
	out := []garageOpCondition{}

	for _, typ := range watched {
		c := conds.get(typ)
		if c.Type == "" {
			continue
		}

		keep := garageOpCondTrue[typ] && c.Status == "True"
		if !garageOpCondTrue[typ] {
			keep = c.Status != "True"
		}

		if keep {
			out = append(out, garageOpCondition{
				Type: c.Type, Status: c.Status, Reason: c.Reason, Message: c.Message,
				Time: unixMilli(c.LastTransitionTime),
			})
		}
	}

	return out
}

// The custom resources, decoded down to what the section keeps.

type garageOpMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// garageOpClusterRef is spec.clusterRef: the namespace defaults to the referencing
// resource's own.
type garageOpClusterRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type garageOpClusterObject struct {
	Metadata garageOpMeta `json:"metadata"`
	Status   struct {
		Phase         string `json:"phase"`
		Replicas      int    `json:"replicas"`
		ReadyReplicas int    `json:"readyReplicas"`
		ClusterID     string `json:"clusterId"`
		BuildInfo     *struct {
			Version string `json:"version"`
		} `json:"buildInfo"`
		StorageReplicas          int            `json:"storageReplicas"`
		StorageReadyReplicas     int            `json:"storageReadyReplicas"`
		GatewayReplicas          int            `json:"gatewayReplicas"`
		GatewayReadyReplicas     int            `json:"gatewayReadyReplicas"`
		LayoutVersion            int64          `json:"layoutVersion"`
		StagedLayoutVersion      *int64         `json:"stagedLayoutVersion"`
		PendingGatewayTombstones []string       `json:"pendingGatewayTombstones"`
		Conditions               kubeConditions `json:"conditions"`
	} `json:"status"`
}

type garageOpBucketObject struct {
	Metadata garageOpMeta `json:"metadata"`
	Spec     struct {
		ClusterRef garageOpClusterRef `json:"clusterRef"`
		Quotas     *struct {
			MaxSize string `json:"maxSize"`
		} `json:"quotas"`
	} `json:"spec"`
	Status struct {
		Phase      string `json:"phase"`
		BucketID   string `json:"bucketId"`
		Size       string `json:"size"`
		QuotaUsage *struct {
			SizeBytes     int64 `json:"sizeBytes"`
			SizeLimit     int64 `json:"sizeLimit"`
			SizePercent   int   `json:"sizePercent"`
			ObjectCount   int64 `json:"objectCount"`
			ObjectLimit   int64 `json:"objectLimit"`
			ObjectPercent int   `json:"objectPercent"`
		} `json:"quotaUsage"`
		WebsiteEnabled bool           `json:"websiteEnabled"`
		WebsiteURL     string         `json:"websiteUrl"`
		Keys           []struct{}     `json:"keys"`
		Conditions     kubeConditions `json:"conditions"`
	} `json:"status"`
}

type garageOpKeyObject struct {
	Metadata garageOpMeta `json:"metadata"`
	Spec     struct {
		ClusterRef garageOpClusterRef `json:"clusterRef"`
		AllBuckets *struct {
			Read  bool `json:"read"`
			Write bool `json:"write"`
			Owner bool `json:"owner"`
		} `json:"allBuckets"`
		BucketPermissions []struct {
			BucketRef *struct {
				Name string `json:"name"`
			} `json:"bucketRef"`
			BucketID    string `json:"bucketId"`
			GlobalAlias string `json:"globalAlias"`
			Read        bool   `json:"read"`
			Write       bool   `json:"write"`
			Owner       bool   `json:"owner"`
		} `json:"bucketPermissions"`
	} `json:"spec"`
	Status struct {
		KeyID       string         `json:"keyId"`
		Phase       string         `json:"phase"`
		ClusterWide bool           `json:"clusterWide"`
		Conditions  kubeConditions `json:"conditions"`
	} `json:"status"`
}

type garageOpNodeObject struct {
	Metadata garageOpMeta `json:"metadata"`
	Spec     struct {
		ClusterRef garageOpClusterRef `json:"clusterRef"`
		NodeID     string             `json:"nodeId"`
		Zone       string             `json:"zone"`
		Capacity   string             `json:"capacity"`
		Gateway    bool               `json:"gateway"`
	} `json:"spec"`
	Status struct {
		NodeID    string `json:"nodeId"`
		Zone      string `json:"zone"`
		Phase     string `json:"phase"`
		InLayout  bool   `json:"inLayout"`
		Connected bool   `json:"connected"`
		LastSeen  string `json:"lastSeen"`
	} `json:"status"`
}

type garageOpDeploymentObject struct {
	Metadata garageOpMeta `json:"metadata"`
	Spec     struct {
		Template struct {
			Spec struct {
				Containers []struct {
					Image string `json:"image"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		Replicas      int `json:"replicas"`
		ReadyReplicas int `json:"readyReplicas"`
	} `json:"status"`
}

// KubeGarageOperator reads the garage-operator's declarative layer alone: its GarageCluster,
// GarageBucket, GarageKey and GarageNode resources and the operator Deployment's health
// (os:admin). The answer is a garageOperatorLayer, with empty lists when the operator's CRDs
// are not served. kubeServer: see KubePods.
func KubeGarageOperator(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() garageOperatorLayer { return *demoGarageOperatorLayer(time.Now()) }, func(ctx context.Context, k *kubeClient) (garageOperatorLayer, error) {
		groups, err := readAPIGroups(ctx, k)
		if err != nil {
			return garageOperatorLayer{}, err
		}

		version, ok := groups[groupGarageOperator]
		if !ok {
			return *newGarageOperatorLayer(), nil
		}

		return *readGarageOperator(ctx, k, version, time.Now()), nil
	})
}

func newGarageOperatorLayer() *garageOperatorLayer {
	return &garageOperatorLayer{
		Clusters: []garageOpCluster{}, Buckets: []garageOpBucket{}, Keys: []garageOpKey{}, Nodes: []garageOpNode{},
	}
}

// readGarageOperator lists the four CR kinds and the operator's Deployments, maps them and
// sorts each list worst health first. version is the group's preferred version; the cluster
// kind is read at v1beta2 when that is what is preferred, else at v1beta1 (still served).
func readGarageOperator(ctx context.Context, k *kubeClient, version string, now time.Time) *garageOperatorLayer {
	out := newGarageOperatorLayer()
	out.Version = garageOpClusterVersion(version)

	var (
		clusters    []garageOpClusterObject
		buckets     []garageOpBucketObject
		keys        []garageOpKeyObject
		nodes       []garageOpNodeObject
		deployments []garageOpDeploymentObject
		truncated   [5]bool // one flag per list: the lists run in parallel
		errs        = make([]error, 5)
		wg          sync.WaitGroup
	)

	wg.Go(func() { errs[0] = garageOpList(ctx, k, out.clustersPath(), &clusters, &truncated[0], nil) })
	wg.Go(func() { errs[1] = garageOpList(ctx, k, out.bucketsPath(), &buckets, &truncated[1], nil) })
	wg.Go(func() { errs[2] = garageOpList(ctx, k, out.keysPath(), &keys, &truncated[2], nil) })
	wg.Go(func() { errs[3] = garageOpList(ctx, k, out.nodesPath(), &nodes, &truncated[3], nil) })
	// Only the operator's own Deployments are kept, page by page: the cluster's other
	// Deployments neither count toward the cap nor push the operator out of it.
	wg.Go(func() {
		errs[4] = garageOpList(ctx, k, "/apis/apps/v1/deployments", &deployments, &truncated[4], isGarageOperatorDeployment(loadAppCatalog()))
	})
	wg.Wait()

	mapped := mapGarageOperator(clusters, buckets, keys, nodes, deployments, now)
	out.Clusters, out.Buckets, out.Keys, out.Nodes, out.Operator = mapped.Clusters, mapped.Buckets, mapped.Keys, mapped.Nodes, mapped.Operator
	out.Truncated = slices.Contains(truncated[:], true)
	out.Error = sectionError(errs...)

	return out
}

// garageOpClusterVersion picks the version garageclusters are read at: the group's preferred
// one when the reader knows it, else v1beta1, which every operator keeps served.
func garageOpClusterVersion(preferred string) string {
	if preferred == garageOpClusterV2 || preferred == garageOpV1 {
		return preferred
	}

	return garageOpV1
}

func (l *garageOperatorLayer) clustersPath() string {
	return "/apis/" + groupGarageOperator + "/" + l.Version + "/garageclusters"
}

func (l *garageOperatorLayer) bucketsPath() string {
	return "/apis/" + groupGarageOperator + "/" + garageOpV1 + "/garagebuckets"
}

func (l *garageOperatorLayer) keysPath() string {
	return "/apis/" + groupGarageOperator + "/" + garageOpV1 + "/garagekeys"
}

func (l *garageOperatorLayer) nodesPath() string {
	return "/apis/" + groupGarageOperator + "/" + garageOpV1 + "/garagenodes"
}

// garageOpList lists path into out, page by page, stopping at garageOpCap items: more
// items set truncated instead of growing the answer. keep, when set, drops the items it
// refuses before they count toward the cap.
func garageOpList[T any](ctx context.Context, k *kubeClient, path string, out *[]T, truncated *bool, keep func(T) bool) error {
	q := pageQuery{}

	for {
		page, err := k.getPage(ctx, path, q)
		if err != nil {
			return err
		}

		items, err := decodeItems[T](page)
		if err != nil {
			return err
		}

		for _, item := range items {
			if keep != nil && !keep(item) {
				continue
			}

			if len(*out) >= garageOpCap {
				*truncated = true

				return nil
			}

			*out = append(*out, item)
		}

		if page.continueToken == "" {
			return nil
		}

		q.continueToken = page.continueToken
	}
}

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

// readGarageSection is the garage section of KubeDataServices: the instances read through
// the Garage CLI in their pods, plus the garage-operator's declarative layer when its CRDs
// are served (version set). nil when neither found anything: no pod group and no group.
func readGarageSection(ctx context.Context, k *kubeClient, run execFunc, pods []dsPod, podsErr error, hinted bool, opVersion string) *garageStatus {
	inst := readGarage(ctx, k, run, pods, podsErr, hinted)
	if opVersion == "" {
		return inst
	}

	layer := readGarageOperator(ctx, k, opVersion, time.Now())

	out := &garageStatus{
		Error: layer.Error, Version: layer.Version, Truncated: layer.Truncated,
		Clusters: layer.Clusters, Buckets: layer.Buckets, Keys: layer.Keys, Nodes: layer.Nodes,
		Operator: layer.Operator, Instances: []garageInstance{},
	}

	if inst != nil {
		out.Instances = inst.Instances

		switch {
		case inst.Error == "":
		case layer.Error == "":
			out.Error = inst.Error
		default:
			out.Error = inst.Error + "; " + layer.Error
		}
	}

	return out
}
