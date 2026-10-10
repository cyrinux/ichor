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
        -> AlertChannel.NODES
        AlertKind.ETCD_ALARM, AlertKind.CHECKUP_PROBLEM, AlertKind.CHECKUP_OK, AlertKind.AM_FIRING, AlertKind.AM_RESOLVED,
        AlertKind.CLUSTER_UNREACHABLE, AlertKind.CLUSTER_REACHABLE,
        -> AlertChannel.CLUSTER
        AlertKind.DATA_PROBLEM, AlertKind.DATA_OK -> AlertChannel.DATA
        AlertKind.GITOPS_PROBLEM, AlertKind.GITOPS_OK -> AlertChannel.GITOPS
        AlertKind.CERT_EXPIRING, AlertKind.CERT_EXPIRED, AlertKind.KUBECONFIG_EXPIRING, AlertKind.KUBECONFIG_EXPIRED -> AlertChannel.CERTS
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
    AlertKind.KUBECONFIG_EXPIRING, AlertKind.KUBECONFIG_EXPIRED, AlertKind.CLUSTER_UNREACHABLE, AlertKind.CLUSTER_REACHABLE ->
        ShareTarget.screen(ShareTarget.CLUSTER)
    AlertKind.DATA_PROBLEM, AlertKind.DATA_OK -> {
        val system = detail.substringBefore('|')
        ShareTarget.dataServices(DataServiceKind.entries.firstOrNull { it.alertSystem == system }?.catalogId.orEmpty())
    }
    AlertKind.GITOPS_PROBLEM, AlertKind.GITOPS_OK -> gitopsTarget()
    AlertKind.CHECKUP_PROBLEM, AlertKind.CHECKUP_OK -> ShareTarget.screen(ShareTarget.CHECKUP)
    AlertKind.AM_FIRING, AlertKind.AM_RESOLVED -> ShareTarget.screen(ShareTarget.ALERTS)
    // "storage:node|volume" or "storage:node|smart|device": that node's Storage screen.
    AlertKind.STORAGE_PROBLEM, AlertKind.STORAGE_OK ->
        ShareTarget.storage(key.substringAfter(':').substringBefore('|'), subject)
}

/** Argo CD: "namespace/name"; Flux: "Kind namespace/name" (see [Alert.subject]). */
private fun Alert.gitopsTarget(): ShareTarget {
    if (GitOpsDetail.parse(detail).tool == GITOPS_FLUX) {
        val where = subject.substringAfter(' ')
        return ShareTarget.fluxApp(subject.substringBefore(' '), where.substringBefore('/'), where.substringAfter('/'))
    }
    return ShareTarget.argoApp(subject.substringBefore('/'), subject.substringAfter('/'))
}
