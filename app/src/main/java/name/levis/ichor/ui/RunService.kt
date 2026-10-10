package name.levis.ichor.ui

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
import androidx.annotation.StringRes
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.ServiceCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.MainActivity
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.i18n.AppLocale

/** A run the app follows app-wide (an upgrade, a node maintenance): its state while a [RunService] keeps the app alive. */
interface FollowedRun {
    /** The node's name, for the notification. */
    val hostname: String
    val finished: Boolean
    val running: Boolean get() = !finished
    /** Why the run failed; null when it succeeded or still runs. */
    val error: String?
}

/**
 * Keeps the app running while it follows a run, so suspending it does not break the
 * connection. Its notification shows the node and the latest step; once the run ends it is
 * replaced by the result, and the service stops. Subclasses name the channel and the texts.
 */
abstract class RunService<T : FollowedRun> : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var collecting = false
    private var lastStartId = 0

    protected abstract val channelId: String
    protected abstract val progressId: Int
    protected abstract val resultId: Int
    @get:StringRes protected abstract val channelName: Int
    @get:StringRes protected abstract val channelDescription: Int
    /** What the lock screen shows while running with the app lock on: never names the node. */
    @get:StringRes protected abstract val publicTitle: Int
    @get:StringRes protected abstract val resultPublicTitle: Int

    /** The followed run, null once none. */
    protected abstract val current: StateFlow<T?>

    /** Title and latest step of the progress notification. */
    protected abstract fun progressText(res: Context, run: T): Pair<String, String>

    /** Title and detail once the run succeeded. */
    protected abstract fun doneText(res: Context, run: T): Pair<String, String>

    /** The title once the run failed; [FollowedRun.error] is the detail. */
    protected abstract fun failedTitle(res: Context, run: T): String

    /** More on the progress notification (a countdown, action buttons); nothing by default. */
    protected open fun decorateProgress(res: Context, builder: NotificationCompat.Builder, run: T) {}

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        lastStartId = startId
        // Started with startForegroundService: it must go foreground, even to stop right away.
        val run = current.value
        goForeground(if (run?.running == true) progressNotification(run) else placeholderNotification())
        if (collecting) {
            show(run)
        } else {
            collecting = true
            scope.launch { current.collect(::show) } // shows the current run right away
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private fun show(run: T?) {
        if (run?.running == true) {
            goForeground(progressNotification(run))
            return
        }
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        if (run?.finished == true) notify(resultId, resultNotification(run))
        stopSelf(lastStartId) // a run started since then keeps the service
    }

    private fun goForeground(notification: Notification) {
        ensureChannel()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(progressId, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(progressId, notification)
        }
    }

    private fun notify(id: Int, notification: Notification) {
        try {
            NotificationManagerCompat.from(this).notify(id, notification)
        } catch (_: SecurityException) {
            // Notifications not allowed: the result is still on the run's screen.
        }
    }

    private fun progressNotification(run: T): Notification {
        val res = AppLocale.wrap(this)
        val (title, step) = progressText(res, run)
        val builder = baseNotification(title)
            .setContentText(step)
            .setOngoing(true)
            .setSilent(true)
            .setCategory(NotificationCompat.CATEGORY_PROGRESS)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
        decorateProgress(res, builder, run)
        return lockScreenSafe(builder, res.getString(publicTitle))
    }

    private fun resultNotification(run: T): Notification {
        val res = AppLocale.wrap(this)
        val error = run.error
        val builder = if (error != null) {
            baseNotification(failedTitle(res, run)).setContentText(error)
        } else {
            val (title, detail) = doneText(res, run)
            baseNotification(title).setContentText(detail)
        }
        builder.setStyle(NotificationCompat.BigTextStyle()).setAutoCancel(true).setCategory(NotificationCompat.CATEGORY_STATUS)
        return lockScreenSafe(builder, res.getString(resultPublicTitle))
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
        baseNotification(AppLocale.wrap(this).getString(publicTitle)).setSilent(true).build()

    private fun baseNotification(title: String): NotificationCompat.Builder {
        val open = Intent(this, MainActivity::class.java)
            .setAction(Intent.ACTION_MAIN)
            .addCategory(Intent.CATEGORY_LAUNCHER)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        val flags = PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        return NotificationCompat.Builder(this, channelId)
            .setSmallIcon(R.drawable.ic_stat_ichor)
            .setContentTitle(title)
            .setContentIntent(PendingIntent.getActivity(this, progressId, open, flags))
    }

    /** (Re)creating the channel also updates its name and description to the current language. */
    private fun ensureChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val res = AppLocale.wrap(this)
        val channel = NotificationChannel(channelId, res.getString(channelName), NotificationManager.IMPORTANCE_LOW).apply {
            description = res.getString(channelDescription)
        }
        getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
    }
}
