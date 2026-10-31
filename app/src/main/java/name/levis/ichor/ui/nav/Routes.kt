package name.levis.ichor.ui.nav

import android.net.Uri
import androidx.navigation.NavHostController
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.DriftFix
import name.levis.ichor.model.KubeFocus
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.PatchPrefill
import name.levis.ichor.model.SnippetPrefill
import name.levis.ichor.model.sensitive
import name.levis.ichor.ui.overview.NodeAction
import name.levis.ichor.ui.resources.ResourceRef
import name.levis.ichor.ui.settings.SettingsPage

internal object Routes {
    const val IMPORT = "import"
    const val DEMO = "demo"
    const val DEMO_KUBE = "demo-kube"
    const val OVERVIEW = "overview"
    const val NODE = "node?addr={addr}&host={host}&role={role}&action={action}"
    const val NODE_TABS = "node/tabs?addr={addr}&host={host}&tab={tab}"
    const val LOGS = "logs?addr={addr}&host={host}&service={service}&container={container}&title={title}&subtitle={subtitle}"
    const val STORAGE = "storage?addr={addr}&host={host}"
    const val RESOURCES = "resources?addr={addr}&host={host}"
    const val RESOURCE_LIST = "resourcelist?addr={addr}&host={host}&ns={ns}&type={type}&sensitive={sensitive}"
    const val RESOURCE = "resource?addr={addr}&host={host}&ns={ns}&type={type}&id={id}&sensitive={sensitive}"
    const val SUPPORT_BUNDLE = "supportbundle"
    const val DIAGNOSTICS = "diagnostics"
    const val INTEGRATIONS = "integrations"
    const val CHANGELOG = "changelog"
    const val LICENSES = "licenses"
    const val SUPPORTED_INTEGRATIONS = "supported-integrations"
    const val FUNDING = "funding"
    const val INSIGHTS = "insights"
    const val SEARCH = "search"
    const val FLEET = "fleet"
    const val APPS = "apps?attention={attention}"
    const val METRICS = "metrics?tab={tab}"

    /** The Metrics screen on its panels, or on [tab] (ui.metrics.METRICS_TAB_MONITORING). */
    fun metrics(tab: Int = 0) = "metrics?tab=$tab"
    const val ALERTS = "alerts?silence={silence}"

    /** [silence]: the fingerprint of an alert whose silence form opens (1 h, from its notification). */
    fun alerts(silence: String = "") = "alerts?silence=${Uri.encode(silence)}"
    const val LOG_SEARCH = "log-search?query={query}"

    /** Log search, searching [query] at once when set. */
    fun logSearch(query: String = "") = "log-search?query=${Uri.encode(query)}"

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
    const val KUBE_NODES = "kube-nodes?filter={filter}&query={query}"

    /**
     * The Kubernetes nodes screen of a cluster added from a kubeconfig; [filter] preselects one
     * (null: all), [query] searches for a node (a node or disk alert).
     */
    fun kubeNodes(filter: NodeFilter?, query: String = "") =
        "kube-nodes?filter=${filter?.name.orEmpty()}&query=${Uri.encode(query)}"
    const val WORKLOADS = "workloads?tab={tab}&key={key}&ns={ns}&name={name}"

    /** The Kubernetes screen; [focus] opens a tab and shows one of its items (a share link). */
    fun workloads(focus: KubeFocus? = null) = if (focus == null) "workloads" else
        "workloads?tab=${focus.tab}&key=${Uri.encode(focus.key)}&ns=${Uri.encode(focus.namespace)}&name=${Uri.encode(focus.name)}"
    const val NETWORK_POLICIES = "netpol"

    /** The admission policies (Kyverno), their findings and the engine's health. */
    const val POLICIES = "policies"
    /** What scales the cluster: HPAs, KEDA, VPA and Karpenter. */
    const val AUTOSCALING = "autoscaling"
    /** KubeVirt (and Harvester) virtual machines and their actions. */
    const val VIRTUAL_MACHINES = "vms"
    /** Argo Rollouts (canary and blue-green) and their promote/abort/retry/pause actions. */
    const val ARGO_ROLLOUTS = "argo-rollouts"

    /** The cluster's Kubernetes events, live. */
    const val KUBE_EVENTS = "kube-events"
    const val API_HEALTH = "apihealth"
    const val CHECKUP = "checkup"

    /** Free room per node, workloads by priority, and what losing a node costs. */
    const val KUBE_CAPACITY = "kube-capacity"
    const val AUDIT = "audit"
    const val FLOWS = "flows?ns={ns}&pod={pod}"

    /** Empty [namespace] for all of them; [pod] narrows to one of [namespace]. */
    fun flows(namespace: String?, pod: String?) = "flows?ns=${Uri.encode(namespace.orEmpty())}&pod=${Uri.encode(pod.orEmpty())}"
    const val DATA_SERVICES = "data-services?kind={kind}&solo={solo}"

    /** [kind] opens on that system's tab; null on the first. [solo]: that system alone, no tab row. */
    fun dataServices(kind: DataServiceKind? = null, solo: Boolean = false) = "data-services?kind=${kind?.name.orEmpty()}&solo=$solo"

    /** The `kind` of a plugin's tab: "plugin:<id>". */
    const val DATA_SERVICES_PLUGIN_PREFIX = "plugin:"

    /** Data services on the tab of the CRD plugin [pluginId]. */
    fun dataServicesPlugin(pluginId: String) = "data-services?kind=${Uri.encode(DATA_SERVICES_PLUGIN_PREFIX + pluginId)}"
    const val ARGO_CD = "argocd"
    const val ARGO_APP = "argocd-app?ns={ns}&name={name}&sync={sync}"
    const val ARGO_WINDOWS = "argocd-windows"

    /** [sync]: open its sync sheet (from an alert's button). */
    fun argoApp(namespace: String, name: String, sync: Boolean = false) =
        "argocd-app?ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}&sync=$sync"
    /** [focus]: the object ("kind/namespace/name") the diff scrolls to and unfolds, "" for none. */
    const val ARGO_DIFF = "argocd-diff?ns={ns}&name={name}&focus={focus}"
    fun argoDiff(namespace: String, name: String, focus: String = "") =
        "argocd-diff?ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}&focus=${Uri.encode(focus)}"
    const val FLUX = "flux"
    const val FLUX_APP = "flux-app?kind={kind}&ns={ns}&name={name}&reconcile={reconcile}"

    /** [reconcile]: open its reconcile confirmation (from an alert's button). */
    fun fluxApp(kind: String, namespace: String, name: String, reconcile: Boolean = false) =
        "flux-app?kind=${Uri.encode(kind)}&ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}&reconcile=$reconcile"
    const val FLUX_DIFF = "flux-diff?kind={kind}&ns={ns}&name={name}&focus={focus}"
    fun fluxDiff(kind: String, namespace: String, name: String, focus: String = "") =
        "flux-diff?kind=${Uri.encode(kind)}&ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}&focus=${Uri.encode(focus)}"
    /** The files of source [idx] of an Argo CD or Flux app ([tool] "argocd" or "flux"), at [dir] ("" for the source's path). */
    const val GITOPS_SOURCE = "gitops-source?tool={tool}&kind={kind}&ns={ns}&name={name}&idx={idx}&dir={dir}"
    fun gitOpsSource(tool: String, kind: String, namespace: String, name: String, idx: Int, dir: String = "") =
        "gitops-source?tool=${Uri.encode(tool)}&kind=${Uri.encode(kind)}&ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}&idx=$idx&dir=${Uri.encode(dir)}"

    /**
     * One file of source [idx] of an Argo CD or Flux app; [path] from the repository root, [line]
     * (1-based) scrolled to and highlighted, 0 for none; [quick] (a quick edit's JSON, "" for
     * none) opens its sheet prefilled (an update from the Updates screen).
     */
    const val GITOPS_FILE = "gitops-file?tool={tool}&kind={kind}&ns={ns}&name={name}&idx={idx}&path={path}&line={line}&quick={quick}"
    fun gitOpsFile(tool: String, kind: String, namespace: String, name: String, idx: Int, path: String, line: Int = 0, quick: String = "") =
        "gitops-file?tool=${Uri.encode(tool)}&kind=${Uri.encode(kind)}&ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}&idx=$idx&path=${Uri.encode(path)}&line=$line" +
            "&quick=${Uri.encode(quick)}"

    /** The Updates screen: the active cluster's images and charts with a newer version. */
    const val UPDATES = "updates"

    /**
     * The editor of the file [path] of source [idx]: the draft stays in its view model, never here.
     * [quick] (a quick edit's JSON, "" when none) is applied to the file as read, then reviewed.
     */
    const val GITOPS_EDIT = "gitops-edit?tool={tool}&kind={kind}&ns={ns}&name={name}&idx={idx}&path={path}&quick={quick}"
    fun gitOpsEdit(tool: String, kind: String, namespace: String, name: String, idx: Int, path: String, quick: String = "") =
        "gitops-edit?tool=${Uri.encode(tool)}&kind=${Uri.encode(kind)}&ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}&idx=$idx&path=${Uri.encode(path)}&quick=${Uri.encode(quick)}"

    /** The active cluster's repository (its Cluster repository setting), at [dir] ("" for the root). */
    const val REPO_SOURCE = "repo-source?dir={dir}"
    fun repoSource(dir: String = "") = "repo-source?dir=${Uri.encode(dir)}"

    /** One file of the active cluster's repository, at the branch head; [line] as in [GITOPS_FILE]. */
    const val REPO_FILE = "repo-file?path={path}&line={line}&noTry={noTry}"

    /** [noTry]: opened from a node's machine config (Compare with Git), so no Try on a node there. */
    fun repoFile(path: String, line: Int = 0, noTry: Boolean = false) = "repo-file?path=${Uri.encode(path)}&line=$line&noTry=$noTry"

    /** The editor of the file [path] of the active cluster's repository; [quick] as in [GITOPS_EDIT]. */
    const val REPO_EDIT = "repo-edit?path={path}&quick={quick}"
    fun repoEdit(path: String, quick: String = "") = "repo-edit?path=${Uri.encode(path)}&quick=${Uri.encode(quick)}"
    const val DEBUG = "debug?addr={addr}&host={host}&ctx={ctx}"
    const val POD_SHELL = "pod-shell?ctx={ctx}&ns={ns}&pod={pod}&c={c}"
    const val KUBE_NODE_DEBUG = "kube-node-debug?ctx={ctx}&node={node}&ns={ns}"
    const val VM_CONSOLE = "vm-console?ctx={ctx}&ns={ns}&vm={vm}"
    const val MACHINE_CONFIG = "machineconfig?addr={addr}&host={host}&fix={fix}&before={before}&after={after}&snippet={snippet}&snippetParams={snippetParams}&patch={patch}&patchFrom={patchFrom}"
    const val NETWORK = "network?addr={addr}&host={host}"
    const val HARDWARE = "hardware?addr={addr}&host={host}"
    const val IMAGES = "images?addr={addr}&host={host}"
    const val ISSUE_CONFIG = "issueconfig"
    const val ROTATE_CA = "rotateca"
    const val CAPTURE = "capture?addr={addr}&host={host}&peer={peer}&peerHost={peerHost}"
    const val NET_TOOLS = "nettools?addr={addr}&host={host}"
    const val CAPTURES = "captures"
    const val CAPTURE_FILE = "capturefile?name={name}"
    const val UPGRADE = "upgrade?addr={addr}&host={host}&version={version}"
    const val CLUSTER_UPGRADE = "clusterupgrade?version={version}"
    const val K8S_UPGRADE = "k8supgrade"
    const val JOIN_NODE = "joinnode"
    const val BOOT_MEDIA = "bootmedia"
    const val MAINTENANCE = "maintenance?addr={addr}&host={host}&drain={drain}"
    const val NODE_META = "nodemeta?addr={addr}&host={host}"

    /** [drain]: a drain only, without the reboot or shutdown that a maintenance can add. */
    fun maintenance(addr: String, host: String, drain: Boolean = false) =
        "maintenance?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&drain=$drain"

    /** The taints and labels of a node: [addr] is its Talos address, or its Kubernetes name on a kubeconfig cluster. */
    fun nodeMeta(addr: String, host: String) = "nodemeta?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    /** [peer]: a second node to capture on too, prefilled ("" for none). */
    fun capture(addr: String, host: String, peer: String = "", peerHost: String = "") =
        "capture?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}" +
            (if (peer.isNotEmpty()) "&peer=${Uri.encode(peer)}&peerHost=${Uri.encode(peerHost)}" else "")
    fun netTools(addr: String, host: String) = "nettools?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun captureFile(name: String) = "capturefile?name=${Uri.encode(name)}"

    /** [version]: target version to preselect (empty: none). */
    /** Every node to [version] ("": the newest stable release), one after the other. */
    fun clusterUpgrade(version: String = "") = "clusterupgrade?version=${Uri.encode(version)}"

    fun upgrade(addr: String, host: String, version: String = "") =
        "upgrade?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&version=${Uri.encode(version)}"

    fun network(addr: String, host: String) = "network?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun hardware(addr: String, host: String) = "hardware?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun images(addr: String, host: String) = "images?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    /** Empty addr: events of every node of the context. */
    const val EVENTS = "events?addr={addr}&host={host}"

    fun events(addr: String = "", host: String = "") = "events?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    /**
     * [fix]: a drift difference the screen opens a draft for; [snippet]: a snippet it opens a
     * draft and the picker on; [patch]: a repository's Talos patch file it opens a draft with
     * (see MachineConfigScreen).
     */
    fun machineConfig(addr: String, host: String, fix: DriftFix? = null, snippet: SnippetPrefill? = null, patch: PatchPrefill? = null) =
        "machineconfig?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}" +
            fix?.let { "&fix=${Uri.encode(it.key)}&before=${Uri.encode(it.before)}&after=${Uri.encode(it.after)}" }.orEmpty() +
            snippet?.let { "&snippet=${Uri.encode(it.id)}&snippetParams=${Uri.encode(it.encodedParams())}" }.orEmpty() +
            patch?.let { "&patch=${Uri.encode(it.content)}&patchFrom=${Uri.encode(it.from)}" }.orEmpty()

    /** [context]: the cluster (talosconfig context) of the node; blank for the one on screen. */
    fun debug(addr: String, host: String, context: String = "") =
        "debug?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&ctx=${Uri.encode(context)}"

    /** A root shell on [node] of [context]'s cluster (no Talos) through a privileged pod in [namespace]. */
    fun kubeNodeDebug(context: String, node: String, namespace: String = "default") =
        "kube-node-debug?ctx=${Uri.encode(context)}&node=${Uri.encode(node)}&ns=${Uri.encode(namespace)}"

    /** `kubectl exec -it` in [container] ("" for the pod's only one) of a pod of [context]'s cluster. */
    fun podShell(context: String, namespace: String, pod: String, container: String) =
        "pod-shell?ctx=${Uri.encode(context)}&ns=${Uri.encode(namespace)}&pod=${Uri.encode(pod)}&c=${Uri.encode(container)}"

    /** `virtctl console`: the serial console of the KubeVirt VM [vm] in [namespace] of [context]'s cluster. */
    fun vmConsole(context: String, namespace: String, vm: String) =
        "vm-console?ctx=${Uri.encode(context)}&ns=${Uri.encode(namespace)}&vm=${Uri.encode(vm)}"
    const val HEALTH = "health"
    const val SETTINGS = "settings"

    /** One group of Settings on its own page ([SettingsPage.route]). */
    const val SETTINGS_PAGE = "settings/{page}"

    fun settingsPage(page: SettingsPage) = "settings/${page.route}"

    /** The action audit log; empty [cluster]: every cluster. */
    const val ACTIVITY = "activity?cluster={cluster}"

    fun activity(cluster: String = "") = "activity?cluster=${Uri.encode(cluster)}"

    /** The runbooks of the cluster on screen. */
    const val RUNBOOKS = "runbooks"

    /** One runbook's plan, by its id in the cluster's list. */
    const val RUNBOOK_PLAN = "runbookplan?id={id}"

    fun runbookPlan(id: String) = "runbookplan?id=${Uri.encode(id)}"

    /** The runbook running or last run. */
    const val RUNBOOK_RUN = "runbookrun"

    /** The vulnerability scan reports kept on the phone for [cluster]. */
    const val SCAN_REPORTS = "scan-reports?cluster={cluster}"

    fun scanReports(cluster: String) = "scan-reports?cluster=${Uri.encode(cluster)}"
    const val DIAGNOSIS = "diagnosis?note={note}"

    /** [note]: what to tell the model up front, e.g. a failed health check. */
    fun diagnosis(note: String = "") = "diagnosis?note=${Uri.encode(note)}"

    /** Empty [service] means the kernel log. */
    fun logs(addr: String, host: String, service: String?) =
        "logs?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&service=${Uri.encode(service.orEmpty())}"

    /** The node screen (its sections); [action]: "reboot"/"shutdown"/"cordon" opens its confirmation. */
    fun node(addr: String, host: String, role: String, action: String = "") =
        "node?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&role=${Uri.encode(role)}&action=$action"

    /** The node's tabs on [tab]: 0 services, 1 resources, 2 live, 3 processes, 4 pods, then cgroups and Kubernetes pods. */
    fun nodeTabs(addr: String, host: String, tab: Int = 0) =
        "node/tabs?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&tab=$tab"

    /** The node screen for [tab] 0 (the node itself), its tabs on [tab] otherwise (e.g. its pods). */
    fun nodeOrTab(addr: String, host: String, role: String, tab: Int) =
        if (tab == 0) node(addr, host, role) else nodeTabs(addr, host, tab)
}

/** What a node row's swipe or action sheet leads to, from home or the Nodes screen. */
internal fun NavHostController.openNodeAction(n: NodeOverview, action: NodeAction) {
    when (action) {
        NodeAction.LIVE -> navigate(Routes.nodeTabs(n.node, n.hostname, tab = 2))
        NodeAction.SERVICES -> navigate(Routes.nodeTabs(n.node, n.hostname))
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
