package name.levis.ichor.monitor

import name.levis.ichor.i18n.AppLocale
import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import name.levis.ichor.MainActivity
import name.levis.ichor.R
import name.levis.ichor.model.dataServiceKindOf
import name.levis.ichor.model.title
import name.levis.ichor.ui.DeepLink
import name.levis.ichor.ui.checkup.sectionLook
import name.levis.ichor.ui.share.shareLinkFor

/** The single channel every alert used before [AlertChannel]: its settings carry over once. */
private const val LEGACY_CHANNEL_ID = "cluster-alerts"

/**
 * (Re)creating the channels also updates their names and descriptions to the current language;
 * the system keeps the importance the user chose. The first time, a legacy channel the user
 * turned down or off passes that on to every new one, then goes.
 */
fun ensureAlertChannels(context: Context) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
    val res = AppLocale.wrap(context)
    val manager = context.getSystemService(NotificationManager::class.java)
    val legacy = manager.getNotificationChannel(LEGACY_CHANNEL_ID)?.importance
    val channels = AlertChannel.entries.map { kind ->
        val importance = legacy?.let { minOf(it, kind.importance) } ?: kind.importance
        NotificationChannel(kind.id, res.getString(kind.title), importance).apply {
            description = res.getString(kind.description)
        }
    }
    manager.createNotificationChannels(channels)
    if (legacy != null) manager.deleteNotificationChannel(LEGACY_CHANNEL_ID)
}

fun canPostNotifications(context: Context): Boolean =
    Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
        ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

/**
 * With [hideOnLockScreen] (app lock on), the lock screen only shows a generic text. [clusterId]:
 * the cluster the alert is about (see [name.levis.ichor.model.ContextSummary.clusterId]), whose
 * screen of the alert's subject a tap opens like a share link; null opens the app only.
 * [actions]: its buttons (see [Alert.actions]), for the cluster of fingerprint [fingerprint].
 */
fun postAlert(
    context: Context,
    alert: Alert,
    hideOnLockScreen: Boolean,
    clusterId: String?,
    actions: List<AlertAction> = emptyList(),
    fingerprint: String = "",
) {
    if (!canPostNotifications(context)) return
    ensureAlertChannels(context)
    val res = AppLocale.wrap(context)
    val title = alertTitle(res, alert)
    val text = alertText(res, alert)

    val intent = Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
    val link = clusterId?.let { id -> alert.shareTarget()?.let { shareLinkFor(it, id) } }
    if (link != null) {
        // Opened like a share link: on that cluster, once unlocked.
        intent.setAction(Intent.ACTION_VIEW).setData(Uri.parse(link))
    } else {
        // The certificate alert opens the renewal screen (which explains when the role cannot renew).
        alertDestination(alert)?.let { intent.putExtra(MainActivity.EXTRA_OPEN, it.name) }
    }
    val open = PendingIntent.getActivity(
        context,
        alertNotificationId(alert.key), // distinct request codes: the links and extras differ
        intent,
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )
    val builder = alertNotification(context, alert.channel, title, text, open, hideOnLockScreen)
    actions.mapNotNull { alertActionButton(context, alert, it, fingerprint, link) }.forEach(builder::addAction)

    try {
        NotificationManagerCompat.from(context).notify(alertNotificationId(alert.key), builder.build())
    } catch (_: SecurityException) {
        // Permission revoked between the check and the post; nothing to do.
    }
}

private fun alertDestination(alert: Alert): DeepLink? = when (alert.kind) {
    AlertKind.CERT_EXPIRING, AlertKind.CERT_EXPIRED -> DeepLink.ISSUE_CONFIG
    AlertKind.GITOPS_PROBLEM, AlertKind.GITOPS_OK ->
        if (alert.detail.substringBefore('|') == GITOPS_FLUX) DeepLink.FLUX else DeepLink.ARGO_CD
    AlertKind.CHECKUP_PROBLEM, AlertKind.CHECKUP_OK -> DeepLink.CHECKUP
    else -> null
}

/**
 * A notification on the alerts [channel] opening [open]; with [hideOnLockScreen] (app lock on),
 * the lock screen only shows a generic text.
 */
fun alertNotification(
    context: Context,
    channel: AlertChannel,
    title: String,
    text: String,
    open: PendingIntent,
    hideOnLockScreen: Boolean,
): NotificationCompat.Builder {
    val res = AppLocale.wrap(context)
    val builder = NotificationCompat.Builder(context, channel.id)
        .setSmallIcon(R.drawable.ic_stat_ichor)
        .setContentTitle(title)
        .setContentText(text)
        .setStyle(NotificationCompat.BigTextStyle().bigText(text))
        .setContentIntent(open)
        .setAutoCancel(true)
        .setCategory(NotificationCompat.CATEGORY_STATUS)

    if (hideOnLockScreen) {
        builder.setVisibility(NotificationCompat.VISIBILITY_PRIVATE).setPublicVersion(
            NotificationCompat.Builder(context, channel.id)
                .setSmallIcon(R.drawable.ic_stat_ichor)
                .setContentTitle(res.getString(R.string.monitor_public_title))
                .build(),
        )
    }
    return builder
}

private fun alertTitle(context: Context, alert: Alert): String = when (alert.kind) {
    AlertKind.NODE_READY -> context.getString(R.string.monitor_node_ready_again, alert.subject)
    AlertKind.NODE_NOT_READY -> context.getString(R.string.monitor_node_not_ready, alert.subject)
    AlertKind.NODE_UNREACHABLE -> context.getString(R.string.monitor_node_unreachable, alert.subject)
    AlertKind.ETCD_ALARM -> context.getString(R.string.monitor_etcd_alarm_title)
    AlertKind.CERT_EXPIRING, AlertKind.CERT_EXPIRED -> context.getString(R.string.monitor_cert_title)
    AlertKind.KUBECONFIG_EXPIRING, AlertKind.KUBECONFIG_EXPIRED -> context.getString(R.string.monitor_kubeconfig_title)
    AlertKind.DATA_PROBLEM -> context.getString(R.string.monitor_data_problem, alert.subject)
    AlertKind.DATA_OK -> context.getString(R.string.monitor_data_ok, alert.subject)
    AlertKind.GITOPS_PROBLEM, AlertKind.GITOPS_OK -> gitopsAlertTitle(context, alert)
    AlertKind.CHECKUP_PROBLEM -> checkupAlertTitle(context, alert)
    AlertKind.CHECKUP_OK -> context.getString(R.string.monitor_checkup_ok, alert.subject)
    AlertKind.AM_FIRING -> context.getString(R.string.monitor_am_firing, alert.subject)
    AlertKind.AM_RESOLVED -> context.getString(R.string.monitor_am_resolved, alert.subject)
}

/** "demo/worker-6f4b8 · critical": what the alert is about, and how bad while it fires. */
private fun amAlertText(context: Context, alert: Alert): String {
    val detail = AmDetail.parse(alert.detail)
    val where = detail.where.ifEmpty { context.getString(R.string.alerts_title) }
    if (!alert.problem) return where
    val severity = when (detail.severity) {
        DATA_CRITICAL -> context.getString(R.string.data_services_health_critical)
        else -> context.getString(R.string.data_services_health_warning)
    }
    return context.getString(R.string.monitor_data_text, where, severity)
}

/** "Volumes: shop/data-postgres-0", from a checkup alert's "section|kind|severity" detail and its subject. */
private fun checkupAlertTitle(context: Context, alert: Alert): String {
    val section = sectionLook(alert.detail.substringBefore('|'))?.let { context.getString(it.title) }
    return listOfNotNull(section, alert.subject).joinToString(": ")
}

/** "Cluster checkup · critical". */
private fun checkupAlertText(context: Context, alert: Alert): String {
    val checkup = context.getString(R.string.checkup_title)
    if (!alert.problem) return checkup
    val severity = when (alert.detail.substringAfterLast('|')) {
        DATA_CRITICAL -> context.getString(R.string.data_services_health_critical)
        else -> context.getString(R.string.data_services_health_warning)
    }
    return context.getString(R.string.monitor_data_text, checkup, severity)
}

/**
 * "Argo CD: grafana sync failed", "Flux: HelmRelease ingress-nginx is not ready", or the cleared
 * form, from a GitOps alert's "tool|severity|reason" detail and its subject.
 */
private fun gitopsAlertTitle(context: Context, alert: Alert): String {
    val (tool, _, reason) = GitOpsDetail.parse(alert.detail)
    if (tool == GITOPS_FLUX) {
        // "Kind namespace/name".
        val kind = alert.subject.substringBefore(' ')
        val name = alert.subject.substringAfter(' ').substringAfter('/')
        val res = if (alert.problem) R.string.monitor_gitops_flux_not_ready else R.string.monitor_gitops_flux_ok
        return context.getString(res, kind, name)
    }
    val name = alert.subject.substringAfter('/')
    val res = when {
        !alert.problem -> R.string.monitor_gitops_argo_ok
        reason == GITOPS_ARGO_FAILED -> R.string.monitor_gitops_argo_failed
        reason == GITOPS_ARGO_DEGRADED -> R.string.monitor_gitops_argo_degraded
        reason == GITOPS_ARGO_MISSING -> R.string.monitor_gitops_argo_missing
        reason == GITOPS_ARGO_ERROR -> R.string.monitor_gitops_argo_error
        else -> R.string.monitor_gitops_argo_out_of_sync
    }
    return context.getString(res, name)
}

/** "monitoring/grafana · critical": where the app lives (Flux: its namespace) and how bad. */
private fun gitopsAlertText(context: Context, alert: Alert): String {
    val where = alert.subject.substringAfter(' ')
    if (!alert.problem) return where
    val severity = when (GitOpsDetail.parse(alert.detail).severity) {
        DATA_CRITICAL -> context.getString(R.string.data_services_health_critical)
        else -> context.getString(R.string.data_services_health_warning)
    }
    return context.getString(R.string.monitor_data_text, where, severity)
}

/** "Longhorn · critical" from a data alert's "system|severity" detail. */
private fun dataAlertText(context: Context, alert: Alert): String {
    val system = dataServiceKindOf(alert.detail.substringBefore('|')).title
    if (!alert.problem) return system
    val severity = when (alert.detail.substringAfter('|')) {
        DATA_CRITICAL -> context.getString(R.string.data_services_health_critical)
        else -> context.getString(R.string.data_services_health_warning)
    }
    return context.getString(R.string.monitor_data_text, system, severity)
}

private fun alertText(context: Context, alert: Alert): String = when (alert.kind) {
    AlertKind.NODE_READY, AlertKind.NODE_NOT_READY, AlertKind.NODE_UNREACHABLE -> alert.detail
    AlertKind.ETCD_ALARM -> context.getString(R.string.monitor_etcd_alarm_text, alert.subject, alert.detail)
    AlertKind.CERT_EXPIRING -> context.resources.getQuantityString(R.plurals.monitor_cert_expires, alert.days, alert.days)
    AlertKind.CERT_EXPIRED -> context.resources.getQuantityString(R.plurals.monitor_cert_expired, alert.days, alert.days)
    AlertKind.KUBECONFIG_EXPIRING -> context.resources.getQuantityString(R.plurals.monitor_kubeconfig_expires, alert.days, alert.days)
    AlertKind.KUBECONFIG_EXPIRED -> context.resources.getQuantityString(R.plurals.monitor_kubeconfig_expired, alert.days, alert.days)
    AlertKind.DATA_PROBLEM, AlertKind.DATA_OK -> dataAlertText(context, alert)
    AlertKind.GITOPS_PROBLEM, AlertKind.GITOPS_OK -> gitopsAlertText(context, alert)
    AlertKind.CHECKUP_PROBLEM, AlertKind.CHECKUP_OK -> checkupAlertText(context, alert)
    AlertKind.AM_FIRING, AlertKind.AM_RESOLVED -> amAlertText(context, alert)
}
