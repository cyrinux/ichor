package name.levis.talosmobile.monitor

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
import name.levis.talosmobile.MainActivity
import name.levis.talosmobile.R

private const val CHANNEL_ID = "cluster-alerts"

fun ensureAlertChannel(context: Context) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
    val channel = NotificationChannel(CHANNEL_ID, "Cluster alerts", NotificationManager.IMPORTANCE_DEFAULT).apply {
        description = "Node status changes, etcd alarms and certificate expiry"
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

    val open = PendingIntent.getActivity(
        context,
        0,
        Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )
    val builder = NotificationCompat.Builder(context, CHANNEL_ID)
        .setSmallIcon(R.drawable.ic_stat_talos)
        .setContentTitle(alert.title)
        .setContentText(alert.text)
        .setStyle(NotificationCompat.BigTextStyle().bigText(alert.text))
        .setContentIntent(open)
        .setAutoCancel(true)
        .setCategory(NotificationCompat.CATEGORY_STATUS)

    if (hideOnLockScreen) {
        builder.setVisibility(NotificationCompat.VISIBILITY_PRIVATE).setPublicVersion(
            NotificationCompat.Builder(context, CHANNEL_ID)
                .setSmallIcon(R.drawable.ic_stat_talos)
                .setContentTitle("Talos cluster alert")
                .build(),
        )
    }

    try {
        NotificationManagerCompat.from(context).notify(alert.key.hashCode(), builder.build())
    } catch (_: SecurityException) {
        // Permission revoked between the check and the post; nothing to do.
    }
}
