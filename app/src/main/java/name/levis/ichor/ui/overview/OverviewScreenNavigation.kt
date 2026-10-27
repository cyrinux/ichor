package name.levis.ichor.ui.overview

import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.PluginDetectItem
import name.levis.ichor.model.ShareTarget

/** Where the overview (and the Kubernetes home it shows for a kubeconfig cluster) leads. */
class OverviewScreenNavigation(
    val onNode: (NodeOverview) -> Unit,
    val onNodeAction: (NodeOverview, NodeAction) -> Unit,
    val onEtcd: () -> Unit,
    val onKubeSpan: () -> Unit,
    val onWorkloads: () -> Unit,
    val onMetrics: () -> Unit,
    val onAlerts: () -> Unit,
    /** Data services on one system (null: the first); alone (solo) for a system the Kubernetes home files elsewhere. */
    val onDataServices: (DataServiceKind?, Boolean) -> Unit,
    /** A CRD plugin's tile (see [name.levis.ichor.ui.plugins.pluginRoute]). */
    val onPlugin: (PluginDetectItem) -> Unit,
    val onArgoCD: () -> Unit,
    val onFlux: () -> Unit,
    val onHealth: () -> Unit,
    val onEvents: () -> Unit,
    val onInsights: () -> Unit,
    val onApps: (attention: Boolean) -> Unit,
    val onSettings: () -> Unit,
    val onFunding: () -> Unit,
    val onIssueConfig: () -> Unit,
    val onUpgrade: (NodeOverview, String) -> Unit,
    /** Every node to the version given ("": the newest stable release). */
    val onUpgradeCluster: (String) -> Unit,
    val onUpgradeKubernetes: () -> Unit,
    /** "Add a node…": a machine in maintenance mode, booted from the Talos ISO. */
    val onAddNode: () -> Unit,
    /** "Boot a new machine…": the booter command, ISO and iPXE links that start a new node. */
    val onBootMedia: () -> Unit,
    val onDiagnose: () -> Unit,
    val onAddCluster: () -> Unit,
    val onClustersCleared: () -> Unit,
    val onChangelog: () -> Unit,
    val onAllNodes: (NodeFilter?) -> Unit,
    /** The Kubernetes nodes screen of a cluster added from a kubeconfig, on one filter (null: all). */
    val onKubeNodes: (NodeFilter?) -> Unit,
    val onCheckup: () -> Unit,
    /** The Capacity screen (from the Kubernetes home). */
    val onCapacity: () -> Unit,
    val onApiHealth: () -> Unit,
    val onNetworkPolicies: () -> Unit,
    /** Kyverno's admission policies (from the Kubernetes home). */
    val onPolicies: () -> Unit,
    /** What scales the cluster (from the Kubernetes home). */
    val onAutoscaling: () -> Unit,
    /** KubeVirt virtual machines (from the Kubernetes home). */
    val onVirtualMachines: () -> Unit,
    /** Argo Rollouts (from the Kubernetes home). */
    val onArgoRollouts: () -> Unit,
    val onResources: () -> Unit,
    val onHelm: () -> Unit,
    val onStorage: () -> Unit,
    val onServices: () -> Unit,
    /** The cluster's own Git repository (its Cluster repository setting), from its root. */
    val onRepository: () -> Unit,
    /** Settings → Git hosts. */
    val onGitHosts: () -> Unit,
    /** Crossplane's packages, definitions and resources (from the Kubernetes home). */
    val onCrossplane: () -> Unit,
    /** The Tailscale operator's connectors and proxies (from the Kubernetes home). */
    val onTailscale: () -> Unit,
    /** Cluster API and Sidero Metal (from the Kubernetes home). */
    val onClusterLifecycle: () -> Unit,
    val onLoadBalancing: () -> Unit,
    val onJobs: () -> Unit,
    /** The cluster's Kubernetes events, live (from the Kubernetes home). */
    val onKubeEvents: () -> Unit,
    val onDrain: (node: String) -> Unit,
    val onNodeDebug: (node: String) -> Unit,
    /** The taints and labels of a Kubernetes node, by its name (from the Kubernetes home). */
    val onNodeMeta: (node: String) -> Unit,
    /** The action audit log of a cluster, by its context name. */
    val onActivity: (cluster: String) -> Unit,
    /** The search across clusters. */
    val onSearch: () -> Unit,
    /** The cluster's runbooks. */
    val onRunbooks: () -> Unit,
    /** Opens a pinned object's screen, like its share link. */
    val onOpenTarget: (ShareTarget) -> Unit,
) {
    /** The part of it the Kubernetes home of a cluster added from a kubeconfig uses. */
    fun kubeHome() = KubeHomeNavigation(
        onWorkloads = onWorkloads,
        onMetrics = onMetrics,
        onAlerts = onAlerts,
        onCheckup = onCheckup,
        onCapacity = onCapacity,
        onApiHealth = onApiHealth,
        onNetworkPolicies = onNetworkPolicies,
        onPolicies = onPolicies,
        onAutoscaling = onAutoscaling,
        onVirtualMachines = onVirtualMachines,
        onArgoRollouts = onArgoRollouts,
        onArgoCD = onArgoCD,
        onFlux = onFlux,
        onDataServices = onDataServices,
        onPlugin = onPlugin,
        onSettings = onSettings,
        onFunding = onFunding,
        onAddCluster = onAddCluster,
        onClustersCleared = onClustersCleared,
        onChangelog = onChangelog,
        onResources = onResources,
        onHelm = onHelm,
        onStorage = onStorage,
        onServices = onServices,
        onCrossplane = onCrossplane,
        onTailscale = onTailscale,
        onClusterLifecycle = onClusterLifecycle,
        onLoadBalancing = onLoadBalancing,
        onRepository = onRepository,
        onGitHosts = onGitHosts,
        onJobs = onJobs,
        onEvents = onKubeEvents,
        onDrain = onDrain,
        onNodeDebug = onNodeDebug,
        onNodeMeta = onNodeMeta,
        onAllNodes = onKubeNodes,
        onActivity = onActivity,
        onSearch = onSearch,
        onRunbooks = onRunbooks,
        onApps = onApps,
        onOpenTarget = onOpenTarget,
    )
}
