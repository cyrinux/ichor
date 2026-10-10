package name.levis.ichor.ui.nav

import android.net.Uri
import androidx.navigation.NavHostController
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.KubeFocus
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.sensitive
import name.levis.ichor.ui.overview.NodeAction
import name.levis.ichor.ui.resources.ResourceRef

internal object Routes {
    const val IMPORT = "import"
    const val DEMO = "demo"
    const val DEMO_KUBE = "demo-kube"
    const val OVERVIEW = "overview"
    const val NODE = "node?addr={addr}&host={host}&role={role}&tab={tab}&action={action}"
    const val LOGS = "logs?addr={addr}&host={host}&service={service}&container={container}&title={title}&subtitle={subtitle}"
    const val STORAGE = "storage?addr={addr}&host={host}"
    const val RESOURCES = "resources?addr={addr}&host={host}"
    const val RESOURCE_LIST = "resourcelist?addr={addr}&host={host}&ns={ns}&type={type}&sensitive={sensitive}"
    const val RESOURCE = "resource?addr={addr}&host={host}&ns={ns}&type={type}&id={id}&sensitive={sensitive}"
    const val SUPPORT_BUNDLE = "supportbundle"
    const val INTEGRATIONS = "integrations"
    const val CHANGELOG = "changelog"
    const val LICENSES = "licenses"
    const val SUPPORTED_INTEGRATIONS = "supported-integrations"
    const val FUNDING = "funding"
    const val INSIGHTS = "insights"
    const val APPS = "apps?attention={attention}"
    const val METRICS = "metrics?tab={tab}"

    /** The Metrics screen on its panels, or on [tab] (ui.metrics.METRICS_TAB_MONITORING). */
    fun metrics(tab: Int = 0) = "metrics?tab=$tab"
    const val ALERTS = "alerts?silence={silence}"

    /** [silence]: the fingerprint of an alert whose silence form opens (1 h, from its notification). */
    fun alerts(silence: String = "") = "alerts?silence=${Uri.encode(silence)}"

    /** [attention]: open on the "needs attention" chip. */
    fun apps(attention: Boolean = false) = "apps?attention=$attention"

    fun storage(addr: String, host: String) = "storage?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun resources(addr: String, host: String) = "resources?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun resourceList(addr: String, host: String, namespace: String, type: String, sensitive: Boolean) =
        "resourcelist?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&ns=${Uri.encode(namespace)}&type=${Uri.encode(type)}&sensitive=$sensitive"

    fun resource(addr: String, host: String, ref: ResourceRef, id: String) =
        "resource?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&ns=${Uri.encode(ref.namespace)}&type=${Uri.encode(ref.type)}" +
            "&id=${Uri.encode(id)}&sensitive=${ref.sensitive}"

    /** The log of a CRI container: [title] its name, [subtitle] its "namespace/pod". */
    fun containerLogs(addr: String, host: String, container: String, title: String, subtitle: String) =
        "logs?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&container=${Uri.encode(container)}" +
            "&title=${Uri.encode(title)}&subtitle=${Uri.encode(subtitle)}"
    const val ETCD = "etcd"

    /** The guided replacement of the failed etcd member [member] whose node is [addr] ([host]). */
    const val REPLACE_CONTROL_PLANE = "replacecp?member={member}&addr={addr}&host={host}"

    fun replaceControlPlane(member: String, addr: String, host: String) =
        "replacecp?member=${Uri.encode(member)}&addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"
    const val KUBESPAN = "kubespan"
    const val NODES = "nodes?filter={filter}"

    /** The Nodes screen of a large cluster; [filter] preselects one (null: all). */
    fun nodes(filter: NodeFilter?) = "nodes?filter=${filter?.name.orEmpty()}"
    const val KUBE_NODES = "kube-nodes?filter={filter}"

    /** The Kubernetes nodes screen of a large cluster added from a kubeconfig; [filter] preselects one (null: all). */
    fun kubeNodes(filter: NodeFilter?) = "kube-nodes?filter=${filter?.name.orEmpty()}"
    const val WORKLOADS = "workloads?tab={tab}&key={key}&ns={ns}&name={name}"

    /** The Kubernetes screen; [focus] opens a tab and shows one of its items (a share link). */
    fun workloads(focus: KubeFocus? = null) = if (focus == null) "workloads" else
        "workloads?tab=${focus.tab}&key=${Uri.encode(focus.key)}&ns=${Uri.encode(focus.namespace)}&name=${Uri.encode(focus.name)}"
    const val NETWORK_POLICIES = "netpol"

    /** The cluster's Kubernetes events, live. */
    const val KUBE_EVENTS = "kube-events"
    const val API_HEALTH = "apihealth"
    const val CHECKUP = "checkup"
    const val AUDIT = "audit"
    const val FLOWS = "flows?ns={ns}&pod={pod}"

    /** Empty [namespace] for all of them; [pod] narrows to one of [namespace]. */
    fun flows(namespace: String?, pod: String?) = "flows?ns=${Uri.encode(namespace.orEmpty())}&pod=${Uri.encode(pod.orEmpty())}"
    const val DATA_SERVICES = "data-services?kind={kind}"

    /** [kind] opens on that system's tab; null on the first. */
    fun dataServices(kind: DataServiceKind? = null) = "data-services?kind=${kind?.name.orEmpty()}"
    const val ARGO_CD = "argocd"
    const val ARGO_APP = "argocd-app?ns={ns}&name={name}&sync={sync}"
    const val ARGO_WINDOWS = "argocd-windows"

    /** [sync]: open its sync sheet (from an alert's button). */
    fun argoApp(namespace: String, name: String, sync: Boolean = false) =
        "argocd-app?ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}&sync=$sync"
    const val ARGO_DIFF = "argocd-diff?ns={ns}&name={name}"
    fun argoDiff(namespace: String, name: String) = "argocd-diff?ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}"
    const val FLUX = "flux"
    const val FLUX_APP = "flux-app?kind={kind}&ns={ns}&name={name}&reconcile={reconcile}"

    /** [reconcile]: open its reconcile confirmation (from an alert's button). */
    fun fluxApp(kind: String, namespace: String, name: String, reconcile: Boolean = false) =
        "flux-app?kind=${Uri.encode(kind)}&ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}&reconcile=$reconcile"
    const val FLUX_DIFF = "flux-diff?kind={kind}&ns={ns}&name={name}"
    fun fluxDiff(kind: String, namespace: String, name: String) =
        "flux-diff?kind=${Uri.encode(kind)}&ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}"
    const val DEBUG = "debug?addr={addr}&host={host}&ctx={ctx}"
    const val POD_SHELL = "pod-shell?ctx={ctx}&ns={ns}&pod={pod}&c={c}"
    const val KUBE_NODE_DEBUG = "kube-node-debug?ctx={ctx}&node={node}&ns={ns}"
    const val MACHINE_CONFIG = "machineconfig?addr={addr}&host={host}"
    const val NETWORK = "network?addr={addr}&host={host}"
    const val HARDWARE = "hardware?addr={addr}&host={host}"
    const val IMAGES = "images?addr={addr}&host={host}"
    const val ISSUE_CONFIG = "issueconfig"
    const val CAPTURE = "capture?addr={addr}&host={host}"
    const val CAPTURES = "captures"
    const val CAPTURE_FILE = "capturefile?name={name}"
    const val UPGRADE = "upgrade?addr={addr}&host={host}&version={version}"
    const val MAINTENANCE = "maintenance?addr={addr}&host={host}&drain={drain}"

    /** [drain]: a drain only, without the reboot or shutdown that a maintenance can add. */
    fun maintenance(addr: String, host: String, drain: Boolean = false) =
        "maintenance?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&drain=$drain"

    fun capture(addr: String, host: String) = "capture?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun captureFile(name: String) = "capturefile?name=${Uri.encode(name)}"

    /** [version]: target version to preselect (empty: none). */
    fun upgrade(addr: String, host: String, version: String = "") =
        "upgrade?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&version=${Uri.encode(version)}"

    fun network(addr: String, host: String) = "network?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun hardware(addr: String, host: String) = "hardware?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun images(addr: String, host: String) = "images?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    /** Empty addr: events of every node of the context. */
    const val EVENTS = "events?addr={addr}&host={host}"

    fun events(addr: String = "", host: String = "") = "events?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun machineConfig(addr: String, host: String) = "machineconfig?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    /** [context]: the cluster (talosconfig context) of the node; blank for the one on screen. */
    fun debug(addr: String, host: String, context: String = "") =
        "debug?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&ctx=${Uri.encode(context)}"

    /** A root shell on [node] of [context]'s cluster (no Talos) through a privileged pod in [namespace]. */
    fun kubeNodeDebug(context: String, node: String, namespace: String = "default") =
        "kube-node-debug?ctx=${Uri.encode(context)}&node=${Uri.encode(node)}&ns=${Uri.encode(namespace)}"

    /** `kubectl exec -it` in [container] ("" for the pod's only one) of a pod of [context]'s cluster. */
    fun podShell(context: String, namespace: String, pod: String, container: String) =
        "pod-shell?ctx=${Uri.encode(context)}&ns=${Uri.encode(namespace)}&pod=${Uri.encode(pod)}&c=${Uri.encode(container)}"
    const val HEALTH = "health"
    const val SETTINGS = "settings"

    /** The action audit log; empty [cluster]: every cluster. */
    const val ACTIVITY = "activity?cluster={cluster}"

    fun activity(cluster: String = "") = "activity?cluster=${Uri.encode(cluster)}"
    const val DIAGNOSIS = "diagnosis?note={note}"

    /** [note]: what to tell the model up front, e.g. a failed health check. */
    fun diagnosis(note: String = "") = "diagnosis?note=${Uri.encode(note)}"

    /** Empty [service] means the kernel log. */
    fun logs(addr: String, host: String, service: String?) =
        "logs?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&service=${Uri.encode(service.orEmpty())}"

    /** [tab]: 0 services, 1 resources, 2 live, 3 processes, 4 pods; [action]: "reboot"/"shutdown"/"cordon" opens its confirmation. */
    fun node(addr: String, host: String, role: String, tab: Int = 0, action: String = "") =
        "node?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&role=${Uri.encode(role)}&tab=$tab&action=$action"
}

/** What a node row's swipe or action sheet leads to, from home or the Nodes screen. */
internal fun NavHostController.openNodeAction(n: NodeOverview, action: NodeAction) {
    when (action) {
        NodeAction.LIVE -> navigate(Routes.node(n.node, n.hostname, n.role, tab = 2))
        NodeAction.SERVICES -> navigate(Routes.node(n.node, n.hostname, n.role))
        NodeAction.KERNEL_LOG -> navigate(Routes.logs(n.node, n.hostname, null))
        NodeAction.SHELL -> navigate(Routes.debug(n.node, n.hostname))
        NodeAction.DRAIN -> navigate(Routes.maintenance(n.node, n.hostname, drain = true))
        NodeAction.REBOOT -> navigate(Routes.node(n.node, n.hostname, n.role, action = "reboot"))
        NodeAction.SHUTDOWN -> navigate(Routes.node(n.node, n.hostname, n.role, action = "shutdown"))
        // The node screen holds the cordon confirmation and the running-maintenance guard.
        NodeAction.CORDON -> navigate(Routes.node(n.node, n.hostname, n.role, action = "cordon"))
    }
}

/** Navigates to [route] and drops everything else from the back stack. */
internal fun NavHostController.resetTo(route: String) {
    navigate(route) {
        popUpTo(graph.id) { inclusive = true }
        launchSingleTop = true
    }
}
