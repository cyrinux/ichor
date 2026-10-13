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
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch
import name.levis.ichor.MainActivity
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.ui.operations.OperationAction
import name.levis.ichor.ui.operations.OperationActionReceiver
import name.levis.ichor.ui.operations.OperationKind
import name.levis.ichor.ui.operations.OperationState
import name.levis.ichor.ui.operations.followedOperations
import name.levis.ichor.ui.operations.resolve

private const val CHANNEL_ID = "operations"

/** The channels the runs had before this service: gone once it posts. */
private val OLD_CHANNEL_IDS = listOf("talos-upgrades", "node-maintenance", "image-pull", "node-join", "image-scan")

/** The notification shown while the service starts and nothing runs (it goes away at once). */
private const val PLACEHOLDER_ID = 0x0b6d

private const val EXTRA_KIND = "name.levis.ichor.OPERATION_KIND"

/**
 * Keeps the app running while it follows a run (an upgrade, a maintenance, a config try or
 * apply, an image pull or scan, a node join), so suspending it does not break the connection.
 * Each running operation has its notification in the one "Operations" channel: its step, a bar
 * where the core counts, its buttons, and a tap opens its screen. Once a run ends its result
 * replaces it; the service stops when none runs.
 */
class OperationService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val operations by lazy { (application as TalosApp).followedOperations() }
    private var collecting = false
    private var lastStartId = 0
    /** The operation whose notification is the service's own. */
    private var foreground: OperationKind? = null
    /** Shown running: their end posts their result. */
    private var shownRunning = emptySet<OperationKind>()
    /** Just started: their result is posted even if they ended before being shown running. */
    private var started = emptySet<OperationKind>()

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        lastStartId = startId
        OperationKind.named(intent?.getStringExtra(EXTRA_KIND))?.let { started = started + it }
        // Started with startForegroundService: it must go foreground, even to stop right away.
        val states = operations.mapNotNull { it.state() }
        val first = states.firstOrNull { it.running && it.kind == foreground } ?: states.firstOrNull { it.running }
        if (first != null) {
            goForeground(first.kind.progressId, progressNotification(first))
            foreground = first.kind
        } else {
            goForeground(PLACEHOLDER_ID, placeholderNotification())
        }
        if (collecting) {
            show(states)
        } else {
            collecting = true
            scope.launch { combine(operations.map { it.states }) { it.filterNotNull() }.collect(::show) }
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private fun show(states: List<OperationState>) {
        val running = states.filter { it.running }
        val runningKinds = running.map { it.kind }.toSet()
        val lead = running.firstOrNull { it.kind == foreground } ?: running.firstOrNull()
        if (lead == null) {
            ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
            foreground = null
        } else {
            goForeground(lead.kind.progressId, progressNotification(lead))
            foreground = lead.kind
            running.filter { it.kind != lead.kind }.forEach { notify(it.kind.progressId, progressNotification(it)) }
        }
        // Ended since shown, or ended before it could be: the progress goes, the result comes.
        val manager = NotificationManagerCompat.from(this)
        (shownRunning - runningKinds).forEach { manager.cancel(it.progressId) }
        states.filter { !it.running && (it.kind in shownRunning || it.kind in started) }
            .forEach { state -> state.result?.let { notify(state.kind.resultId, resultNotification(state)) } }
        shownRunning = runningKinds
        started = emptySet()
        if (lead == null) stopSelf(lastStartId) // a run started since then keeps the service
    }

    private fun goForeground(id: Int, notification: Notification) {
        ensureChannel()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(id, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(id, notification)
        }
    }

    private fun notify(id: Int, notification: Notification) {
        try {
            NotificationManagerCompat.from(this).notify(id, notification)
        } catch (_: SecurityException) {
            // Notifications not allowed: the run is still on its screen.
        }
    }

    private fun progressNotification(state: OperationState): Notification {
        val res = AppLocale.wrap(this)
        val shown = requireNotNull(state.progress)
        val text = shown.text.resolve(res)
        val builder = baseNotification(shown.title.resolve(res), state.kind)
            .setContentText(text)
            .setStyle(NotificationCompat.BigTextStyle().bigText(text))
            .setOngoing(true)
            .setSilent(true)
            .setOnlyAlertOnce(true)
            .setCategory(NotificationCompat.CATEGORY_PROGRESS)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
        shown.progress?.let { builder.setProgress(it.total, it.done, it.total == 0) }
        // The countdown ticks by itself.
        if (shown.countdownTo > 0) {
            builder.setWhen(shown.countdownTo).setShowWhen(true).setUsesChronometer(true).setChronometerCountDown(true)
        }
        shown.actions.forEach { builder.addAction(action(res, state.kind, it)) }
        return lockScreenSafe(builder, res.getString(state.kind.publicTitle), state.kind)
    }

    private fun resultNotification(state: OperationState): Notification {
        val res = AppLocale.wrap(this)
        val result = requireNotNull(state.result)
        val builder = baseNotification(result.title.resolve(res), state.kind)
            .setContentText(result.text.resolve(res))
            .setStyle(NotificationCompat.BigTextStyle())
            .setAutoCancel(true)
            .setCategory(NotificationCompat.CATEGORY_STATUS)
        return lockScreenSafe(builder, res.getString(state.kind.resultPublicTitle), state.kind)
    }

    // With the app lock on, the lock screen does not name the node.
    private fun lockScreenSafe(builder: NotificationCompat.Builder, publicTitle: String, kind: OperationKind): Notification {
        if (appLocked()) {
            builder.setVisibility(NotificationCompat.VISIBILITY_PRIVATE)
                .setPublicVersion(baseNotification(publicTitle, kind).build())
        }
        return builder.build()
    }

    private fun placeholderNotification(): Notification =
        NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_stat_ichor)
            .setContentTitle(AppLocale.wrap(this).getString(R.string.operations_channel_name))
            .setSilent(true)
            .build()

    private fun baseNotification(title: String, kind: OperationKind): NotificationCompat.Builder =
        NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_stat_ichor)
            .setContentTitle(title)
            .setContentIntent(openRun(kind, kind.progressId))

    /** The app on [kind]'s screen (unlocked first when the app lock is on); the app as it was when it has none. */
    private fun openRun(kind: OperationKind, requestCode: Int): PendingIntent {
        val open = Intent(this, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        kind.deepLink?.let { open.putExtra(MainActivity.EXTRA_OPEN, it.name) }
            ?: open.setAction(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        return PendingIntent.getActivity(this, requestCode, open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    }

    // With the app lock on, Stop, Abort and Keep open the run's screen instead of acting from the
    // shade: the screen's buttons are behind the lock too.
    private fun action(res: Context, kind: OperationKind, action: OperationAction): NotificationCompat.Action {
        val requestCode = (kind.progressId shl 4) or action.ordinal
        val opensApp = action.opensAppWhenLocked && appLocked()
        val intent = if (opensApp) {
            openRun(kind, requestCode)
        } else {
            PendingIntent.getBroadcast(
                this,
                requestCode,
                OperationActionReceiver.intent(this, kind, action),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
            )
        }
        return NotificationCompat.Action.Builder(0, res.getString(action.label), intent)
            .setShowsUserInterface(opensApp)
            .setAuthenticationRequired(true)
            .build()
    }

    private fun appLocked(): Boolean = (application as TalosApp).appLock.enabled.value

    /** (Re)creating the channel also updates its name and description to the current language. */
    private fun ensureChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val res = AppLocale.wrap(this)
        val manager = getSystemService(NotificationManager::class.java)
        val channel = NotificationChannel(CHANNEL_ID, res.getString(R.string.operations_channel_name), NotificationManager.IMPORTANCE_LOW).apply {
            description = res.getString(R.string.operations_channel_desc)
        }
        manager.createNotificationChannel(channel)
        OLD_CHANNEL_IDS.forEach { id -> if (manager.getNotificationChannel(id) != null) manager.deleteNotificationChannel(id) }
    }

    companion object {
        /** Follows the operations running now; [kind] just started (its result shows even if it ended at once). */
        fun start(context: Context, kind: OperationKind) {
            ContextCompat.startForegroundService(
                context,
                Intent(context, OperationService::class.java).putExtra(EXTRA_KIND, kind.name),
            )
        }
    }
}
