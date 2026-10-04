package name.levis.ichor.ui.upgrade

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import name.levis.ichor.MainActivity
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.UpgradeRunState
import name.levis.ichor.i18n.AppLocale

private const val CHANNEL_ID = "talos-upgrades"
private const val PROGRESS_ID = 0x0b6e
private const val RESULT_ID = 0x0b6f

/**
 * Keeps the app running while it follows a Talos upgrade, so suspending it does not break the
 * connection mid-install. Its notification shows the node and the latest step; once the upgrade
 * ends it is replaced by the result, and the service stops.
 */
class UpgradeService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var collecting = false
    private var lastStartId = 0

    private val upgrades get() = (application as TalosApp).upgradeManager

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        lastStartId = startId
        // Started with startForegroundService: it must go foreground, even to stop right away.
        val run = upgrades.current.value
        goForeground(if (run?.running == true) progressNotification(run) else placeholderNotification())
        if (collecting) {
            show(run)
        } else {
            collecting = true
            scope.launch { upgrades.current.collect(::show) } // shows the current run right away
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private fun show(run: UpgradeRunState?) {
        if (run?.running == true) {
            goForeground(progressNotification(run))
            return
        }
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        if (run?.finished == true) notify(RESULT_ID, resultNotification(run))
        stopSelf(lastStartId) // an upgrade started since then keeps the service
    }

    private fun goForeground(notification: Notification) {
        ensureChannel(this)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(PROGRESS_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(PROGRESS_ID, notification)
        }
    }

    private fun notify(id: Int, notification: Notification) {
        try {
            NotificationManagerCompat.from(this).notify(id, notification)
        } catch (_: SecurityException) {
            // Notifications not allowed: the result is still on the upgrade screen.
        }
    }

    private fun progressNotification(run: UpgradeRunState): Notification {
        val res = AppLocale.wrap(this)
        val step = run.events.lastOrNull()?.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.upgrade_phase_requested)
        return lockScreenSafe(
            baseNotification(res.getString(R.string.upgrade_notification_title, run.hostname))
                .setContentText(step)
                .setOngoing(true)
                .setSilent(true)
                .setCategory(NotificationCompat.CATEGORY_PROGRESS)
                .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE),
            res.getString(R.string.upgrade_notification_public),
        )
    }

    private fun resultNotification(run: UpgradeRunState): Notification {
        val res = AppLocale.wrap(this)
        val builder = if (run.error != null) {
            baseNotification(res.getString(R.string.upgrade_notification_failed, run.hostname)).setContentText(run.error)
        } else {
            baseNotification(res.getString(R.string.upgrade_notification_done, run.hostname))
                .setContentText(res.getString(R.string.upgrade_done_versions, run.fromVersion.ifEmpty { "?" }, run.newVersion.ifEmpty { "?" }))
        }
        builder.setStyle(NotificationCompat.BigTextStyle()).setAutoCancel(true).setCategory(NotificationCompat.CATEGORY_STATUS)
        return lockScreenSafe(builder, res.getString(R.string.upgrade_notification_result_public))
    }

    // With the app lock on, the lock screen does not name the node.
    private fun lockScreenSafe(builder: NotificationCompat.Builder, publicTitle: String): Notification {
        if ((application as TalosApp).appLock.enabled.value) {
            builder.setVisibility(NotificationCompat.VISIBILITY_PRIVATE)
                .setPublicVersion(baseNotification(publicTitle).build())
        }
        return builder.build()
    }

    private fun placeholderNotification(): Notification =
        baseNotification(AppLocale.wrap(this).getString(R.string.upgrade_notification_public)).setSilent(true).build()

    private fun baseNotification(title: String): NotificationCompat.Builder {
        val open = Intent(this, MainActivity::class.java)
            .setAction(Intent.ACTION_MAIN)
            .addCategory(Intent.CATEGORY_LAUNCHER)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        val flags = PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_stat_ichor)
            .setContentTitle(title)
            .setContentIntent(PendingIntent.getActivity(this, PROGRESS_ID, open, flags))
    }

    companion object {
        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, UpgradeService::class.java))
        }
    }
}

/** (Re)creating the channel also updates its name and description to the current language. */
private fun ensureChannel(context: Context) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
    val res = AppLocale.wrap(context)
    val channel = NotificationChannel(CHANNEL_ID, res.getString(R.string.upgrade_channel_name), NotificationManager.IMPORTANCE_LOW).apply {
        description = res.getString(R.string.upgrade_channel_desc)
    }
    context.getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
}
