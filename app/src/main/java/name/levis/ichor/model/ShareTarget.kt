package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * A screen a share link opens (the JSON of the Go core's BuildShareLink and ParseShareLink):
 * [cluster] is the cluster id of [ContextSummary.clusterId], so the link works on any phone
 * holding a talosconfig for the same cluster. A link only navigates, it never acts.
 */
@Serializable
data class ShareTarget(
    val cluster: String = "",
    val target: String,
    val host: String = "",
    val addr: String = "",
    val tab: String = "",
    val kind: String = "",
    @SerialName("ns") val namespace: String = "",
    val name: String = "",
) {
    companion object {
        const val CLUSTER = "cluster"
        const val ETCD = "etcd"
        const val HEALTH = "health"
        const val ARGO_CD = "argocd"
        const val FLUX = "flux"
        const val NODE = "node"
        const val WORKLOADS = "workloads"
        const val ARGO_APP = "argo-app"
        const val FLUX_APP = "flux-app"
        const val WORKLOAD = "workload"
        const val POD = "pod"
        const val CRONJOB = "cronjob"
        const val DATA = "data"
        const val CHECKUP = "checkup"
        const val ALERTS = "alerts"
        const val STORAGE = "storage"
        const val HARDWARE = "hardware"

        /** The node screen's tabs, by their index there. */
        val NODE_TABS = listOf("services", "resources", "live", "processes", "pods", "cgroups", "kube-pods")

        /** The Kubernetes screen's tabs, by their index there (the network test is not shared). */
        val KUBE_TABS = listOf("workloads", "pods", "cronjobs")

        fun screen(target: String) = ShareTarget(target = target)

        fun node(addr: String, host: String, tab: Int) =
            ShareTarget(target = NODE, addr = addr, host = host, tab = NODE_TABS.getOrNull(tab).orEmpty())

        /** A node's Storage screen (its volumes, mounts and disks). */
        fun storage(addr: String, host: String) = ShareTarget(target = STORAGE, addr = addr, host = host)

        /** A node's Hardware screen (its sensors). */
        fun hardware(addr: String, host: String) = ShareTarget(target = HARDWARE, addr = addr, host = host)

        fun kubernetes(tab: Int) = ShareTarget(target = WORKLOADS, tab = KUBE_TABS.getOrNull(tab).orEmpty())

        fun argoApp(namespace: String, name: String) = ShareTarget(target = ARGO_APP, namespace = namespace, name = name)

        fun fluxApp(kind: String, namespace: String, name: String) =
            ShareTarget(target = FLUX_APP, kind = kind, namespace = namespace, name = name)

        fun workload(kind: String, namespace: String, name: String) =
            ShareTarget(target = WORKLOAD, kind = kind, namespace = namespace, name = name)

        fun pod(namespace: String, name: String) = ShareTarget(target = POD, namespace = namespace, name = name)

        fun cronJob(namespace: String, name: String) = ShareTarget(target = CRONJOB, namespace = namespace, name = name)

        /** The data services screen on the tab of [catalogId] (see [DataServiceKind.catalogId]); "" for the first. */
        fun dataServices(catalogId: String = "") = ShareTarget(target = DATA, kind = catalogId)

        /** The prefix of a plugin's id in a data services link's kind (no plugin target in Go's links). */
        const val PLUGIN_KIND_PREFIX = "plugin-"

        /** Where CRD plugin [pluginId]'s tile leads (a plugin finding's alert), as iOS reads it too. */
        fun plugin(pluginId: String) = dataServices(PLUGIN_KIND_PREFIX + pluginId)
    }
}

/** The CRD plugin a data services link names ([ShareTarget.plugin]), null for a data service's tab. */
val ShareTarget.linkedPlugin: String?
    get() = kind.takeIf { target == ShareTarget.DATA && it.startsWith(ShareTarget.PLUGIN_KIND_PREFIX) }
        ?.removePrefix(ShareTarget.PLUGIN_KIND_PREFIX)?.takeIf { it.isNotEmpty() }

/** The tab of the node screen the link opens (0 when it names none). */
val ShareTarget.nodeTab: Int get() = ShareTarget.NODE_TABS.indexOf(tab).coerceAtLeast(0)

/**
 * The node of a cluster added from a kubeconfig a node or storage link names (a node or disk
 * alert), by its Kubernetes name, when it is one of [nodes]; null otherwise (its home opens).
 */
fun ShareTarget.kubeNodeName(nodes: Collection<String>): String? =
    addr.ifEmpty { host }.takeIf { (target == ShareTarget.NODE || target == ShareTarget.STORAGE) && it.isNotEmpty() && it in nodes }

/**
 * The node a Talos storage or hardware link names (a disk or temperature alert) when the
 * overview cannot tell (not read, or without it): its address, only when it is one of the
 * nodes [context]'s config targets (its nodes, else its endpoints), so a link still cannot
 * point the app's Talos calls at another address. Null for any other link.
 */
fun ShareTarget.configuredNode(context: ContextSummary?): String? {
    if (target != ShareTarget.STORAGE && target != ShareTarget.HARDWARE) return null
    val targets = context?.let { it.nodes.ifEmpty { it.endpoints } }.orEmpty()
    return addr.takeIf { it.isNotEmpty() && it in targets }
}

/**
 * What the Kubernetes screen opens for the link: its tab, and the item to show (a workload,
 * pod or CronJob), or null for a target that is not on that screen.
 */
val ShareTarget.kubeFocus: KubeFocus?
    get() = when (target) {
        ShareTarget.WORKLOADS -> KubeFocus(ShareTarget.KUBE_TABS.indexOf(tab).coerceAtLeast(0))
        ShareTarget.WORKLOAD -> KubeFocus(0, "$kind/$namespace/$name", namespace, name)
        ShareTarget.POD -> KubeFocus(1, "$namespace/$name", namespace, name)
        ShareTarget.CRONJOB -> KubeFocus(2, "$namespace/$name", namespace, name)
        else -> null
    }

/**
 * The Kubernetes screen opened on [tab], with the item of key [key] (as its row's) shown:
 * the list scoped to [namespace] and searched for [name].
 */
data class KubeFocus(val tab: Int, val key: String = "", val namespace: String = "", val name: String = "")

/**
 * The context to open a link of [clusterId] with: the active one when it is of that cluster
 * (its role is kept), else the first of it; null when the phone has no such cluster.
 */
fun ConfigSummary.contextFor(clusterId: String, active: String?): ContextSummary? {
    val ofCluster = contexts.filter { clusterId.isNotEmpty() && it.clusterId == clusterId }
    return ofCluster.firstOrNull { it.name == active } ?: ofCluster.firstOrNull()
}
