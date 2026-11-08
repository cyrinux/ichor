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
import name.levis.ichor.model.CheckupKind
import name.levis.ichor.model.dataServiceKindOf
import name.levis.ichor.model.pluginAlertObject
import name.levis.ichor.model.formatSlope
import name.levis.ichor.model.title
import name.levis.ichor.ui.DeepLink
import name.levis.ichor.ui.checkup.sectionLook
import name.levis.ichor.ui.share.shareLinkFor
import name.levis.ichor.util.DurationFormat
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.formatDuration

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

/**
 * Cancels the alerts on screen: they name nodes and namespaces, so they go once screenshot mode
 * is on; the next check raises what still holds, masked. Only the app's alert channels.
 */
fun cancelPostedAlerts(context: Context) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
    val manager = context.getSystemService(NotificationManager::class.java) ?: return
    val channels = AlertChannel.entries.map { it.id }.toSet()
    runCatching { manager.activeNotifications }.getOrDefault(emptyArray())
        .filter { it.notification.channelId in channels }
        .forEach { manager.cancel(it.tag, it.id) }
}

fun canPostNotifications(context: Context): Boolean =
    Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
        ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

/**
 * With [hideOnLockScreen] (app lock on), the lock screen only shows a generic text. [clusterId]:
 * the cluster the alert is about (see [name.levis.ichor.model.ContextSummary.clusterId]), whose
 * screen of the alert's subject a tap opens like a share link; null opens the app only.
 * [actions]: its buttons (see [Alert.actions]), for the cluster of fingerprint [fingerprint].
 * [cluster]: the cluster's name as the app shows it, in the notification's header.
 * [stillFiringMinutes]: a repeat of an alert firing for that long ("Still firing · 25 min").
 */
fun postAlert(
    context: Context,
    alert: Alert,
    hideOnLockScreen: Boolean,
    clusterId: String?,
    actions: List<AlertAction> = emptyList(),
    fingerprint: String = "",
    cluster: String? = null,
    stillFiringMinutes: Long? = null,
) {
    if (!canPostNotifications(context)) return
    ensureAlertChannels(context)
    val res = AppLocale.wrap(context)
    val title = alertTitle(res, alert)
    val text = alertText(res, alert).let { text ->
        stillFiringMinutes?.let { res.getString(R.string.alert_repeat_still_firing, it.toInt(), text) } ?: text
    }

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
        alertNotificationId(fingerprint, alert.key), // distinct request codes: the links and extras differ
        intent,
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )
    val builder = alertNotification(context, alert.channel, title, text, open, hideOnLockScreen)
    // Every watched cluster alerts: which one this is about, next to the app's name.
    cluster?.takeIf { it.isNotBlank() }?.let(builder::setSubText)
    actions.mapNotNull { alertActionButton(context, alert, it, fingerprint, link) }.forEach(builder::addAction)
    // A repeat replaces the alert's notification and sounds again.
    if (stillFiringMinutes != null) builder.setOnlyAlertOnce(false)

    try {
        NotificationManagerCompat.from(context).notify(alertNotificationId(fingerprint, alert.key), builder.build())
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
    AlertKind.CLUSTER_UNREACHABLE -> context.getString(R.string.monitor_cluster_unreachable)
    AlertKind.CLUSTER_REACHABLE -> context.getString(R.string.monitor_cluster_reachable)
    AlertKind.STORAGE_PROBLEM, AlertKind.STORAGE_OK -> storageAlertTitle(context, alert)
    AlertKind.STORAGE_TREND, AlertKind.STORAGE_TREND_OK -> trendAlertTitle(context, alert)
    AlertKind.SENSOR_PROBLEM, AlertKind.SENSOR_OK -> sensorAlertTitle(context, alert)
    AlertKind.KUBESPAN_DOWN -> context.getString(R.string.monitor_kubespan_down, alert.subject)
    AlertKind.KUBESPAN_UP -> context.getString(R.string.monitor_kubespan_up, alert.subject)
    AlertKind.PLUGIN_PROBLEM, AlertKind.PLUGIN_OK -> pluginAlertTitle(context, alert)
    AlertKind.PROPOSAL_MERGED -> context.getString(R.string.monitor_proposal_merged, ProposalDetail.parse(alert.detail).number)
    AlertKind.PROPOSAL_CLOSED -> context.getString(R.string.monitor_proposal_closed, ProposalDetail.parse(alert.detail).number)
    AlertKind.SIGN_IN_REQUIRED -> context.getString(R.string.kube_signin_renew_notification_title)
}

/** "kube-green: demo-lab/weekend", or "… is healthy again": the plugin's name and its finding's own title. */
private fun pluginAlertTitle(context: Context, alert: Alert): String {
    val issue = PluginIssue.parse(alert.detail)
    val what = listOf(issue.pluginName, alert.subject).filter { it.isNotEmpty() }.joinToString(": ")
    return if (alert.problem) what else context.getString(R.string.monitor_data_ok, what)
}

/** "no wake-up time · warning": the finding's own detail (Go translated it, else the plugin's name) and how bad; the plugin's name once cleared. */
private fun pluginAlertText(context: Context, alert: Alert): String {
    val issue = PluginIssue.parse(alert.detail)
    val plugin = issue.pluginName.ifEmpty { issue.plugin }
    if (!alert.problem) return plugin
    val severity = when (issue.severity) {
        DATA_CRITICAL -> context.getString(R.string.data_services_health_critical)
        else -> context.getString(R.string.data_services_health_warning)
    }
    return context.getString(R.string.monitor_data_text, issue.detail.ifEmpty { plugin }, severity)
}

/** "worker-1 runs hot (88.0 °C)", "worker-1 is throttled (91.0 °C)", or the cleared forms, from a [SensorDetail]. */
private fun sensorAlertTitle(context: Context, alert: Alert): String {
    val d = SensorDetail.parse(alert.detail)
    return when {
        // Without a reading (a node without sensors, or an alert stored before it was kept): no value.
        d.throttled && alert.problem && d.celsius > 0 -> context.getString(R.string.monitor_sensor_throttled_temp, d.hostname, celsius(d.celsius))
        d.throttled && alert.problem -> context.getString(R.string.monitor_sensor_throttled, d.hostname)
        d.throttled -> context.getString(R.string.monitor_sensor_throttled_ok, d.hostname)
        alert.problem -> context.getString(R.string.monitor_sensor_hot, d.hostname, celsius(d.celsius))
        else -> context.getString(R.string.monitor_sensor_hot_ok, d.hostname)
    }
}

/** "Package id 0, alert at 80.0 °C", the throttling hint; "Node temperatures" once cleared. */
private fun sensorAlertText(context: Context, alert: Alert): String {
    val d = SensorDetail.parse(alert.detail)
    return when {
        !alert.problem -> context.getString(R.string.monitor_sensors)
        d.throttled -> context.getString(R.string.monitor_sensor_throttled_text)
        else -> context.getString(R.string.monitor_sensor_hot_text, d.label, celsius(d.limit))
    }
}

/** "never handshaked · Pods on these nodes cannot reach each other over KubeSpan.", in the map banner's words. */
private fun kubespanAlertText(context: Context, alert: Alert): String {
    val d = KubeSpanDetail.parse(alert.detail)
    val reason = when (d.reason) {
        KUBESPAN_REASON_NO_ENDPOINT -> context.getString(R.string.kubespan_reason_no_endpoint)
        KUBESPAN_REASON_NEVER -> context.getString(R.string.kubespan_reason_never)
        KUBESPAN_REASON_STALE -> context.getString(
            R.string.kubespan_reason_stale,
            formatDuration(
                System.currentTimeMillis() / 1000 - d.since,
                DurationFormat(
                    daysHours = context.getString(R.string.common_duration_days_hours),
                    hoursMinutes = context.getString(R.string.common_duration_hours_minutes),
                    minutes = context.getString(R.string.common_duration_minutes),
                    lessThanMinute = context.getString(R.string.common_duration_less_than_minute),
                ),
            ),
        )
        KUBESPAN_REASON_ONE_SIDE -> context.getString(R.string.topology_link_degraded)
        else -> context.getString(R.string.kubespan_peer_down)
    }
    return "$reason · ${context.getString(R.string.kubespan_banner_meaning)}"
}

private fun celsius(value: Double): String = String.format(java.util.Locale.ROOT, "%.1f", value)

/**
 * "EPHEMERAL on worker-1 full in ~3 days" ("critical in" when that comes first, "in < 1 day"
 * below a day), or the cleared form, from a trend alert's [TrendDetail].
 */
private fun trendAlertTitle(context: Context, alert: Alert): String {
    val d = TrendDetail.parse(alert.detail)
    return when {
        !alert.problem -> context.getString(R.string.monitor_storage_trend_ok, d.name, d.hostname)
        d.days < 1 && d.critical -> context.getString(R.string.monitor_storage_trend_critical_soon, d.name, d.hostname)
        d.days < 1 -> context.getString(R.string.monitor_storage_trend_full_soon, d.name, d.hostname)
        d.critical -> context.resources.getQuantityString(R.plurals.monitor_storage_trend_critical, d.days, d.name, d.hostname, d.days)
        else -> context.resources.getQuantityString(R.plurals.monitor_storage_trend_full, d.days, d.name, d.hostname, d.days)
    }
}

/** "growing ~2.4 % a day, now at 81 %"; "Node storage" once cleared. */
private fun trendAlertText(context: Context, alert: Alert): String {
    if (!alert.problem) return context.getString(R.string.monitor_storage)
    val d = TrendDetail.parse(alert.detail)
    return context.getString(R.string.monitor_storage_trend_text, formatSlope(d.slopePerDay), d.percent)
}

/**
 * "EPHEMERAL on worker-1 at 91 %" (", almost full" when critical), "nodefs on worker-1:
 * DiskPressure", "SMART failing on sda (worker-2)", or the cleared forms, from a storage
 * alert's [StorageDetail].
 */
private fun storageAlertTitle(context: Context, alert: Alert): String {
    val d = StorageDetail.parse(alert.detail)
    return when {
        d.smart && alert.problem -> context.getString(R.string.monitor_storage_smart, d.name, d.hostname)
        d.smart -> context.getString(R.string.monitor_storage_smart_ok, d.name, d.hostname)
        !alert.problem -> context.getString(R.string.monitor_storage_fill_ok, d.name, d.hostname, d.warn)
        d.reason == DISK_PRESSURE -> context.getString(R.string.monitor_storage_disk_pressure, d.name, d.hostname)
        d.severity == DATA_CRITICAL -> context.getString(R.string.monitor_storage_fill_critical, d.name, d.hostname, d.percent)
        else -> context.getString(R.string.monitor_storage_fill, d.name, d.hostname, d.percent)
    }
}

/**
 * "9.0 GiB free of 100.0 GiB", the SMART reason (or the disk's model); "Node storage" once
 * cleared, or for a node in DiskPressure whose kubelet gave no size.
 */
private fun storageAlertText(context: Context, alert: Alert): String {
    val d = StorageDetail.parse(alert.detail)
    val fallback = context.getString(R.string.monitor_storage)
    return when {
        !alert.problem -> fallback
        d.smart -> d.reason.ifBlank { fallback }
        d.sizeBytes <= 0 -> fallback
        else -> context.getString(R.string.monitor_storage_fill_text, formatBytes(d.freeBytes), formatBytes(d.sizeBytes))
    }
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
    // A plugin finding ("plugin:finding:object"): "kube-green: demo-lab/weekend".
    if (alert.detail.split('|').getOrNull(1) == CheckupKind.PLUGIN) {
        return listOf(alert.subject.substringBefore(':'), pluginAlertObject(alert.subject)).filter { it.isNotEmpty() }.joinToString(": ")
    }
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
    if (alert.problem && alert.detail.substringBefore('|') == "snapshots") return context.getString(R.string.snapshot_stale).replaceFirstChar { it.titlecase() }
    if (alert.problem && alert.detail.substringBefore('|') == "backupjobs") return context.resources.getQuantityString(R.plurals.backup_failures_in_row, 3, 3).replaceFirstChar { it.titlecase() }
    val key = alert.detail.substringBefore('|')
    val system = dataServiceKindOf(key)?.title ?: key
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
    AlertKind.CLUSTER_UNREACHABLE -> context.getString(R.string.monitor_cluster_unreachable_text, alert.detail)
    AlertKind.CLUSTER_REACHABLE -> context.getString(R.string.monitor_cluster_reachable_text)
    AlertKind.STORAGE_PROBLEM, AlertKind.STORAGE_OK -> storageAlertText(context, alert)
    AlertKind.STORAGE_TREND, AlertKind.STORAGE_TREND_OK -> trendAlertText(context, alert)
    AlertKind.SENSOR_PROBLEM, AlertKind.SENSOR_OK -> sensorAlertText(context, alert)
    AlertKind.KUBESPAN_DOWN -> kubespanAlertText(context, alert)
    AlertKind.KUBESPAN_UP -> context.getString(R.string.monitor_kubespan)
    AlertKind.PLUGIN_PROBLEM, AlertKind.PLUGIN_OK -> pluginAlertText(context, alert)
    // The request's own title, unmasked: left out in screenshot mode.
    AlertKind.PROPOSAL_MERGED, AlertKind.PROPOSAL_CLOSED ->
        if ((context.applicationContext as? name.levis.ichor.TalosApp)?.uiPreferences?.privacyMask?.value?.enabled == true) "" else alert.subject
    AlertKind.SIGN_IN_REQUIRED -> context.getString(R.string.kube_signin_renew_notification_text, alert.subject)
}
