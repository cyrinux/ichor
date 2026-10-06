package name.levis.ichor.monitor

import name.levis.ichor.i18n.AppLocale
import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import name.levis.ichor.MainActivity
import name.levis.ichor.R
import name.levis.ichor.model.dataServiceKindOf
import name.levis.ichor.model.title
import name.levis.ichor.ui.DeepLink

private const val CHANNEL_ID = "cluster-alerts"

/** (Re)creating the channel also updates its name and description to the current language. */
fun ensureAlertChannel(context: Context) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
    val res = AppLocale.wrap(context)
    val name = res.getString(R.string.monitor_channel_name)
    val channel = NotificationChannel(CHANNEL_ID, name, NotificationManager.IMPORTANCE_DEFAULT).apply {
        description = res.getString(R.string.monitor_channel_desc)
    }
    context.getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
}

fun canPostNotifications(context: Context): Boolean =
    Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
        ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

/** With [hideOnLockScreen] (app lock on), the lock screen only shows a generic text. */
fun postAlert(context: Context, alert: Alert, hideOnLockScreen: Boolean) {
    if (!canPostNotifications(context)) return
    ensureAlertChannel(context)
    val res = AppLocale.wrap(context)
    val title = alertTitle(res, alert)
    val text = alertText(res, alert)

    val intent = Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
    // A certificate alert opens the renewal screen (which explains when the role cannot renew), a
    // GitOps one the Argo CD or Flux screen (of the active cluster, like the freeze notices).
    val destination = alertDestination(alert)
    destination?.let { intent.putExtra(MainActivity.EXTRA_OPEN, it.name) }
    val open = PendingIntent.getActivity(
        context,
        destination?.let { it.ordinal + 1 } ?: 0, // distinct request codes: the extras differ
        intent,
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )
    val builder = alertNotification(context, title, text, open, hideOnLockScreen)

    try {
        NotificationManagerCompat.from(context).notify(alert.key.hashCode(), builder.build())
    } catch (_: SecurityException) {
        // Permission revoked between the check and the post; nothing to do.
    }
}

private fun alertDestination(alert: Alert): DeepLink? = when (alert.kind) {
    AlertKind.CERT_EXPIRING, AlertKind.CERT_EXPIRED -> DeepLink.ISSUE_CONFIG
    AlertKind.GITOPS_PROBLEM, AlertKind.GITOPS_OK ->
        if (alert.detail.substringBefore('|') == GITOPS_FLUX) DeepLink.FLUX else DeepLink.ARGO_CD
    else -> null
}

/**
 * A notification on the alerts channel opening [open]; with [hideOnLockScreen] (app lock on),
 * the lock screen only shows a generic text.
 */
fun alertNotification(context: Context, title: String, text: String, open: PendingIntent, hideOnLockScreen: Boolean): NotificationCompat.Builder {
    val res = AppLocale.wrap(context)
    val builder = NotificationCompat.Builder(context, CHANNEL_ID)
        .setSmallIcon(R.drawable.ic_stat_ichor)
        .setContentTitle(title)
        .setContentText(text)
        .setStyle(NotificationCompat.BigTextStyle().bigText(text))
        .setContentIntent(open)
        .setAutoCancel(true)
        .setCategory(NotificationCompat.CATEGORY_STATUS)

    if (hideOnLockScreen) {
        builder.setVisibility(NotificationCompat.VISIBILITY_PRIVATE).setPublicVersion(
            NotificationCompat.Builder(context, CHANNEL_ID)
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
    AlertKind.DATA_PROBLEM -> context.getString(R.string.monitor_data_problem, alert.subject)
    AlertKind.DATA_OK -> context.getString(R.string.monitor_data_ok, alert.subject)
    AlertKind.GITOPS_PROBLEM, AlertKind.GITOPS_OK -> gitopsAlertTitle(context, alert)
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
    AlertKind.DATA_PROBLEM, AlertKind.DATA_OK -> dataAlertText(context, alert)
    AlertKind.GITOPS_PROBLEM, AlertKind.GITOPS_OK -> gitopsAlertText(context, alert)
}
