package name.levis.ichor.monitor

import android.app.NotificationManager
import name.levis.ichor.R
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.alertSystem

/**
 * The notification channels of the alerts, so each kind can be muted on its own. [importance]
 * is the default; the user's choice in the system settings wins once the channel exists.
 */
enum class AlertChannel(val id: String, val importance: Int, val title: Int, val description: Int) {
    NODES("alerts-nodes", NotificationManager.IMPORTANCE_HIGH, R.string.monitor_channel_nodes, R.string.monitor_channel_nodes_desc),
    CLUSTER("alerts-cluster", NotificationManager.IMPORTANCE_DEFAULT, R.string.monitor_channel_cluster, R.string.monitor_channel_cluster_desc),
    DATA("alerts-data", NotificationManager.IMPORTANCE_DEFAULT, R.string.monitor_channel_data, R.string.monitor_channel_data_desc),
    GITOPS("alerts-gitops", NotificationManager.IMPORTANCE_DEFAULT, R.string.monitor_channel_gitops, R.string.monitor_channel_gitops_desc),
    CERTS("alerts-certs", NotificationManager.IMPORTANCE_DEFAULT, R.string.monitor_channel_certs, R.string.monitor_channel_certs_desc),
}

val Alert.channel: AlertChannel
    get() = when (kind) {
        AlertKind.NODE_READY, AlertKind.NODE_NOT_READY, AlertKind.NODE_UNREACHABLE, AlertKind.STORAGE_PROBLEM, AlertKind.STORAGE_OK,
        AlertKind.STORAGE_TREND, AlertKind.STORAGE_TREND_OK, AlertKind.SENSOR_PROBLEM, AlertKind.SENSOR_OK,
        -> AlertChannel.NODES
        AlertKind.ETCD_ALARM, AlertKind.CHECKUP_PROBLEM, AlertKind.CHECKUP_OK, AlertKind.AM_FIRING, AlertKind.AM_RESOLVED,
        AlertKind.CLUSTER_UNREACHABLE, AlertKind.CLUSTER_REACHABLE,
        -> AlertChannel.CLUSTER
        AlertKind.DATA_PROBLEM, AlertKind.DATA_OK, AlertKind.PLUGIN_PROBLEM, AlertKind.PLUGIN_OK -> AlertChannel.DATA
        AlertKind.GITOPS_PROBLEM, AlertKind.GITOPS_OK, AlertKind.PROPOSAL_MERGED, AlertKind.PROPOSAL_CLOSED -> AlertChannel.GITOPS
        AlertKind.CERT_EXPIRING, AlertKind.CERT_EXPIRED, AlertKind.KUBECONFIG_EXPIRING, AlertKind.KUBECONFIG_EXPIRED,
        AlertKind.SIGN_IN_REQUIRED,
        -> AlertChannel.CERTS
    }

/**
 * The screen a tapped alert opens, as a share link names it (the same keys on Android and
 * iOS); null for the talosconfig certificate, whose renewal screen is no share target.
 */
fun Alert.shareTarget(): ShareTarget? = when (kind) {
    AlertKind.NODE_READY, AlertKind.NODE_NOT_READY, AlertKind.NODE_UNREACHABLE ->
        ShareTarget(target = ShareTarget.NODE, addr = key.substringAfter(':'), host = subject)
    AlertKind.ETCD_ALARM -> ShareTarget.screen(ShareTarget.ETCD)
    AlertKind.CERT_EXPIRING, AlertKind.CERT_EXPIRED -> null
    // The cluster's home, where its sign-in banner is.
    AlertKind.KUBECONFIG_EXPIRING, AlertKind.KUBECONFIG_EXPIRED, AlertKind.CLUSTER_UNREACHABLE, AlertKind.CLUSTER_REACHABLE,
    AlertKind.SIGN_IN_REQUIRED,
    -> ShareTarget.screen(ShareTarget.CLUSTER)
    AlertKind.DATA_PROBLEM, AlertKind.DATA_OK -> {
        val system = detail.substringBefore('|')
        ShareTarget.dataServices(DataServiceKind.entries.firstOrNull { it.alertSystem == system }?.catalogId.orEmpty())
    }
    AlertKind.GITOPS_PROBLEM, AlertKind.GITOPS_OK -> gitopsTarget()
    // The app the request changed; the cluster for the repository's own.
    AlertKind.PROPOSAL_MERGED, AlertKind.PROPOSAL_CLOSED -> {
        val d = ProposalDetail.parse(detail)
        when {
            !d.isApp() -> ShareTarget.screen(ShareTarget.CLUSTER)
            d.tool == GITOPS_FLUX -> ShareTarget.fluxApp(d.kind, d.namespace, d.name)
            else -> ShareTarget.argoApp(d.namespace, d.name)
        }
    }
    // A data services link naming the plugin ("plugin-<id>"): the app opens where its tile leads (its
    // Data services tab or its own screen); an app that does not know it, the first tab.
    AlertKind.PLUGIN_PROBLEM, AlertKind.PLUGIN_OK -> ShareTarget.plugin(PluginIssue.parse(detail).plugin.ifEmpty { pluginOfKey(key) })
    AlertKind.CHECKUP_PROBLEM, AlertKind.CHECKUP_OK -> ShareTarget.screen(ShareTarget.CHECKUP)
    AlertKind.AM_FIRING, AlertKind.AM_RESOLVED -> ShareTarget.screen(ShareTarget.ALERTS)
    // "storage:node|volume", "storage:node|smart|device" or "storage:node|volume:trend": that node's Storage screen;
    // a Kubernetes node's disk ("storage:node|nodefs"), which has no Storage screen: the node.
    AlertKind.STORAGE_PROBLEM, AlertKind.STORAGE_OK, AlertKind.STORAGE_TREND, AlertKind.STORAGE_TREND_OK -> {
        val node = key.substringAfter(':').substringBefore('|')
        if (key.removeSuffix(":trend") == "storage:${kubeStorageKey(node)}") {
            ShareTarget(target = ShareTarget.NODE, addr = node, host = subject)
        } else {
            ShareTarget.storage(node, subject)
        }
    }
    // "sensor:node|hot" or "sensor:node|throttled": that node's Hardware screen.
    AlertKind.SENSOR_PROBLEM, AlertKind.SENSOR_OK -> ShareTarget.hardware(key.substringAfter(':').substringBefore('|'), subject)
}

/** Argo CD: "namespace/name"; Flux: "Kind namespace/name" (see [Alert.subject]). */
private fun Alert.gitopsTarget(): ShareTarget {
    if (GitOpsDetail.parse(detail).tool == GITOPS_FLUX) {
        val where = subject.substringAfter(' ')
        return ShareTarget.fluxApp(subject.substringBefore(' '), where.substringBefore('/'), where.substringAfter('/'))
    }
    return ShareTarget.argoApp(subject.substringBefore('/'), subject.substringAfter('/'))
}
