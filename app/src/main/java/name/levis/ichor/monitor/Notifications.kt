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
    // A certificate alert opens the renewal screen (which explains when the role cannot renew).
    val certificate = alert.kind == AlertKind.CERT_EXPIRING || alert.kind == AlertKind.CERT_EXPIRED
    if (certificate) intent.putExtra(MainActivity.EXTRA_OPEN, DeepLink.ISSUE_CONFIG.name)
    val open = PendingIntent.getActivity(
        context,
        if (certificate) 1 else 0, // distinct request codes: the extras differ
        intent,
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )
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

    try {
        NotificationManagerCompat.from(context).notify(alert.key.hashCode(), builder.build())
    } catch (_: SecurityException) {
        // Permission revoked between the check and the post; nothing to do.
    }
}

private fun alertTitle(context: Context, alert: Alert): String = when (alert.kind) {
    AlertKind.NODE_READY -> context.getString(R.string.monitor_node_ready_again, alert.subject)
    AlertKind.NODE_NOT_READY -> context.getString(R.string.monitor_node_not_ready, alert.subject)
    AlertKind.NODE_UNREACHABLE -> context.getString(R.string.monitor_node_unreachable, alert.subject)
    AlertKind.ETCD_ALARM -> context.getString(R.string.monitor_etcd_alarm_title)
    AlertKind.CERT_EXPIRING, AlertKind.CERT_EXPIRED -> context.getString(R.string.monitor_cert_title)
}

private fun alertText(context: Context, alert: Alert): String = when (alert.kind) {
    AlertKind.NODE_READY, AlertKind.NODE_NOT_READY, AlertKind.NODE_UNREACHABLE -> alert.detail
    AlertKind.ETCD_ALARM -> context.getString(R.string.monitor_etcd_alarm_text, alert.subject, alert.detail)
    AlertKind.CERT_EXPIRING -> context.resources.getQuantityString(R.plurals.monitor_cert_expires, alert.days, alert.days)
    AlertKind.CERT_EXPIRED -> context.resources.getQuantityString(R.plurals.monitor_cert_expired, alert.days, alert.days)
}
