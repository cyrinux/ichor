package name.levis.ichor.monitor

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.work.CoroutineWorker
import androidx.work.ExistingWorkPolicy
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import name.levis.ichor.MainActivity
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.model.wakeTargets
import name.levis.ichor.model.wolKey
import name.levis.ichor.ui.overview.sendWake

// The buttons of an alert's notification (see [AlertAction]): Snooze and Wake run here, in the
// background; the others open the app on the alert's screen, whose confirmation they show.

private const val ACTION_SNOOZE = "name.levis.ichor.ALERT_SNOOZE"
private const val ACTION_WAKE = "name.levis.ichor.ALERT_WAKE"
private const val KEY_CLUSTER = "cluster"
private const val KEY_ALERT = "alert"
private const val KEY_HOST = "host"

/**
 * The notification an alert of [key] on the cluster [cluster] (fingerprint) is posted as: a newer
 * one replaces it, never another cluster's of the same key.
 */
fun alertNotificationId(cluster: String, key: String): Int = "$cluster|$key".hashCode()

/**
 * The button [action] of [alert], posted for the cluster [fingerprint]; [link] is the alert's
 * share link, which the in-app actions open. Null when it cannot be offered (no link to open).
 */
fun alertActionButton(context: Context, alert: Alert, action: AlertAction, fingerprint: String, link: String?): NotificationCompat.Action? {
    val res = AppLocale.wrap(context)
    val requestCode = "$fingerprint|${alert.key}|${action.name}".hashCode()
    val flags = PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
    val intent = if (action.inApp) {
        val request = alert.actionRequest(action) ?: return null
        // Opened like the share link, on its cluster once unlocked; the screen asks before anything is sent.
        val open = Intent(context, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
            .setAction(Intent.ACTION_VIEW)
            .setData(Uri.parse(link ?: return null))
            .putExtra(MainActivity.EXTRA_ALERT_ACTION, request.action.name)
            .putExtra(MainActivity.EXTRA_ALERT_TARGET, request.target)
            .putExtra(MainActivity.EXTRA_ALERT_TOKEN, (context.applicationContext as TalosApp).alertActionToken.value())
        PendingIntent.getActivity(context, requestCode, open, flags)
    } else {
        val broadcast = Intent(context, AlertActionReceiver::class.java)
            .setAction(if (action == AlertAction.WAKE) ACTION_WAKE else ACTION_SNOOZE)
            .putExtra(KEY_CLUSTER, fingerprint)
            .putExtra(KEY_ALERT, alert.key)
            .putExtra(KEY_HOST, alert.subject)
        PendingIntent.getBroadcast(context, requestCode, broadcast, flags)
    }
    val title = when (action) {
        AlertAction.WAKE -> R.string.alert_action_wake
        AlertAction.REBOOT -> R.string.power_reboot
        AlertAction.SYNC -> R.string.argo_sync
        AlertAction.RECONCILE -> R.string.flux_reconcile
        AlertAction.SILENCE -> R.string.alert_action_silence_hour
        AlertAction.SNOOZE -> R.string.alert_action_snooze
    }
    return NotificationCompat.Action.Builder(0, res.getString(title), intent)
        .setShowsUserInterface(action.inApp)
        // Never from the lock screen: the device is unlocked first (the app lock then asks too).
        .setAuthenticationRequired(action.inApp)
        .build()
}

/** Snooze and Wake from an alert's notification. Wake runs as work: the receiver must return at once. */
class AlertActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val cluster = intent.getStringExtra(KEY_CLUSTER).orEmpty()
        val key = intent.getStringExtra(KEY_ALERT) ?: return
        when (intent.action) {
            ACTION_SNOOZE -> {
                val snoozes = (context.applicationContext as TalosApp).alertSnoozes
                val now = System.currentTimeMillis()
                snoozes.prune(now)
                snoozes.snooze(cluster, key, now + SNOOZE_MILLIS)
                NotificationManagerCompat.from(context).cancel(alertNotificationId(cluster, key))
            }
            ACTION_WAKE -> {
                val data = workDataOf(KEY_CLUSTER to cluster, KEY_ALERT to key, KEY_HOST to intent.getStringExtra(KEY_HOST).orEmpty())
                WorkManager.getInstance(context).enqueueUniqueWork(
                    "alert-wake|$cluster|$key",
                    ExistingWorkPolicy.KEEP,
                    OneTimeWorkRequestBuilder<AlertWakeWorker>().setInputData(data).build(),
                )
            }
        }
    }
}

/**
 * Sends the magic packets like the in-app Wake-on-LAN, to the node's saved target or the MACs it
 * was seen with, then says how it went in place of the alert.
 */
class AlertWakeWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val app = applicationContext as TalosApp
        val res = AppLocale.wrap(applicationContext)
        val key = inputData.getString(KEY_ALERT).orEmpty()
        val host = inputData.getString(KEY_HOST).orEmpty()
        val cluster = inputData.getString(KEY_CLUSTER).orEmpty()
        val wol = wolKey(cluster, key.substringAfter(':'))
        val targets = wakeTargets(app.wakeOnLan.targets.value[wol], app.wakeOnLan.seen.value[wol].orEmpty())
        if (targets.isEmpty()) return Result.success()
        postWakeNotice(applicationContext, cluster, key, res.getString(R.string.wol_title, host), sendWake(res, host, targets))
        return Result.success()
    }
}

/** The outcome of a Wake, replacing the alert; a tap opens the app. */
private fun postWakeNotice(context: Context, cluster: String, key: String, title: String, text: String) {
    if (!canPostNotifications(context)) return
    ensureAlertChannels(context)
    val app = context.applicationContext as TalosApp
    val intent = Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
    val open = PendingIntent.getActivity(context, "$cluster|$key|wake".hashCode(), intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    val builder = alertNotification(context, AlertChannel.NODES, title, text, open, hideOnLockScreen = app.appLock.enabled.value)
    try {
        NotificationManagerCompat.from(context).notify(alertNotificationId(cluster, key), builder.build())
    } catch (_: SecurityException) {
        // Permission revoked between the check and the post; nothing to do.
    }
}
