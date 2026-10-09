package name.levis.ichor.ui.debug

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
import name.levis.ichor.i18n.AppLocale

private const val CHANNEL_ID = "debug-shells"

// Shown only for the instant a service start finds every shell already gone.
private const val PLACEHOLDER_ID = 0x5e11

/**
 * Keeps the app running while a debug shell is open, so the shell survives leaving its screen
 * or the app. Each running shell has its notification: tap to go back to it, Exit to end it.
 * The first one is the service's own; the service stops once no shell runs.
 */
class DebugShellService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var collecting = false
    private var shown: Set<Int> = emptySet()
    private var lastStartId = 0

    private val shells get() = (application as TalosApp).debugShells

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        lastStartId = startId
        if (intent?.action == ACTION_EXIT) {
            intent.shellKey()?.let(shells::close)
            if (!collecting) stopSelf(startId) // a stale notification: nothing runs
            return START_NOT_STICKY
        }
        // Started with startForegroundService: it must go foreground, even to stop right away.
        val live = shells.live.value
        if (live.isEmpty()) goForeground(PLACEHOLDER_ID, placeholderNotification())
        show(live)
        if (!collecting) {
            collecting = true
            scope.launch { shells.live.collect(::show) }
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private fun show(live: List<LiveShell>) {
        val manager = NotificationManagerCompat.from(this)
        if (live.isEmpty()) {
            ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
            shown.forEach(manager::cancel)
            shown = emptySet()
            stopSelf(lastStartId) // a shell started since then keeps the service
            return
        }
        ensureChannel(this)
        val notifications = live.map { it.key.notificationId to shellNotification(it) }
        val (firstId, first) = notifications.first()
        goForeground(firstId, first)
        notifications.drop(1).forEach { (id, notification) ->
            try {
                manager.notify(id, notification)
            } catch (_: SecurityException) {
                // Notifications not allowed: the shells still run, reachable from the node.
            }
        }
        val ids = notifications.map { it.first }.toSet()
        (shown - ids).forEach(manager::cancel)
        shown = ids
    }

    private fun goForeground(id: Int, notification: Notification) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(id, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(id, notification)
        }
    }

    private fun shellNotification(shell: LiveShell): Notification {
        val res = AppLocale.wrap(this)
        val id = shell.key.notificationId
        val open = Intent(this, MainActivity::class.java)
            .setAction(ACTION_OPEN) // apart from the alerts' intents, whose extras differ
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
            .putShell(shell.key)
            .putExtra(MainActivity.EXTRA_SHELL_HOST, shell.hostname)
        val exit = Intent(this, DebugShellService::class.java).setAction(ACTION_EXIT).putShell(shell.key)
        val flags = PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        val title = when {
            shell.key.kubeNode -> R.string.node_debug_auth
            shell.key.isPod -> R.string.pod_shell_on
            else -> R.string.debug_notification_title
        }
        val builder = baseNotification(res.getString(title, shell.hostname))
            .setContentText(res.getString(R.string.debug_notification_text))
            .setContentIntent(PendingIntent.getActivity(this, id, open, flags))
            .addAction(0, res.getString(R.string.debug_notification_exit), PendingIntent.getService(this, id, exit, flags))
        // With the app lock on, the lock screen does not name the node.
        if ((application as TalosApp).appLock.enabled.value) {
            builder.setVisibility(NotificationCompat.VISIBILITY_PRIVATE)
                .setPublicVersion(baseNotification(res.getString(R.string.debug_notification_public)).build())
        }
        return builder.build()
    }

    private fun placeholderNotification(): Notification =
        baseNotification(AppLocale.wrap(this).getString(R.string.debug_notification_public)).also { ensureChannel(this) }.build()

    private fun baseNotification(title: String) = NotificationCompat.Builder(this, CHANNEL_ID)
        .setSmallIcon(R.drawable.ic_stat_ichor)
        .setContentTitle(title)
        .setOngoing(true)
        .setSilent(true)
        .setCategory(NotificationCompat.CATEGORY_SERVICE)
        .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)

    companion object {
        private const val ACTION_EXIT = "name.levis.ichor.debug.EXIT"
        const val ACTION_OPEN = "name.levis.ichor.debug.OPEN"

        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, DebugShellService::class.java))
        }
    }
}

private fun Intent.putShell(key: ShellKey): Intent =
    putExtra(MainActivity.EXTRA_SHELL_CONTEXT, key.context).putExtra(MainActivity.EXTRA_SHELL_NODE, key.node)
        .putExtra(MainActivity.EXTRA_SHELL_NAMESPACE, key.namespace).putExtra(MainActivity.EXTRA_SHELL_POD, key.pod)
        .putExtra(MainActivity.EXTRA_SHELL_CONTAINER, key.container).putExtra(MainActivity.EXTRA_SHELL_KUBE_NODE, key.kubeNode)

/** The shell a notification intent names, if it names one: a node's, or a pod's. */
fun Intent.shellKey(): ShellKey? {
    val context = getStringExtra(MainActivity.EXTRA_SHELL_CONTEXT) ?: return null
    val node = getStringExtra(MainActivity.EXTRA_SHELL_NODE).orEmpty()
    val namespace = getStringExtra(MainActivity.EXTRA_SHELL_NAMESPACE).orEmpty()
    val pod = getStringExtra(MainActivity.EXTRA_SHELL_POD).orEmpty()
    if (node.isBlank() && (pod.isBlank() || namespace.isBlank())) return null
    return ShellKey(
        context, node, namespace, pod, getStringExtra(MainActivity.EXTRA_SHELL_CONTAINER).orEmpty(),
        kubeNode = getBooleanExtra(MainActivity.EXTRA_SHELL_KUBE_NODE, false),
    )
}

/** (Re)creating the channel also updates its name and description to the current language. */
private fun ensureChannel(context: Context) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
    val res = AppLocale.wrap(context)
    val channel = NotificationChannel(CHANNEL_ID, res.getString(R.string.debug_channel_name), NotificationManager.IMPORTANCE_LOW).apply {
        description = res.getString(R.string.debug_channel_desc)
    }
    context.getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
}
