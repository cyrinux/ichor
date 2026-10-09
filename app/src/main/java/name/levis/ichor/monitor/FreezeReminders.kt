package name.levis.ichor.monitor

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.work.CoroutineWorker
import androidx.work.Data
import androidx.work.ExistingWorkPolicy
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import name.levis.ichor.MainActivity
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.realFingerprint
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.model.ArgoFreezeAction
import name.levis.ichor.model.ArgoFreezeOptions
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.FREEZE_EXTEND_MINUTES
import name.levis.ichor.model.ProjectWindow
import name.levis.ichor.model.runningIchorFreezes
import name.levis.ichor.ui.DeepLink
import name.levis.ichor.ui.uiText
import java.text.DateFormat
import java.util.Date
import java.util.concurrent.TimeUnit

// A notification 5 minutes before an Ichor freeze ends, with "+1 h". Scheduled from the freezes
// the app last read (WorkManager, no polling): Argo CD ends the freeze on its own, the reminder
// only says so. See the Linear plan document "D9. Argo CD freeze: hotfix live without being reverted".

private const val TAG_PREFIX = "argo-freeze-reminder|"
private const val LEAD_MILLIS = 5 * 60_000L
private const val KEY_CLUSTER = "cluster"
private const val KEY_PROJECT_NS = "projectNamespace"
private const val KEY_PROJECT = "project"
private const val KEY_WINDOW = "window"
private const val KEY_SCOPE = "scope"
private const val KEY_END = "end"
private const val ACTION_EXTEND = "name.levis.ichor.ARGO_FREEZE_EXTEND"
private val STRING_KEYS = listOf(KEY_CLUSTER, KEY_PROJECT_NS, KEY_PROJECT, KEY_WINDOW, KEY_SCOPE)

/** The freezes last scheduled per cluster, so the 2 s polling during a sync does not reschedule. */
private val scheduled = mutableMapOf<String, Set<String>>()

/** The active cluster's fingerprint, null for none or the demo: read when a load starts. */
fun freezeReminderCluster(app: TalosApp): String? =
    app.configRepository.config.value?.realFingerprint

/** For [name.levis.ichor.ui.argocd.ArgoViewModel]: binds each load to the cluster active when it starts. */
fun freezeReminderHook(app: TalosApp): () -> (ArgoStatus) -> Unit = {
    val cluster = freezeReminderCluster(app)
    val sync: (ArgoStatus) -> Unit = { status -> syncFreezeReminders(app, cluster, status) }
    sync
}

/**
 * Schedules a reminder for each Ichor freeze of [status], read from [cluster], still running:
 * one unique work per freeze, replaced when its end moves, cancelled when the freeze is gone.
 * A freeze already inside the lead time keeps its pending reminder. WorkManager is not exact:
 * under Doze the reminder may come a few minutes late.
 */
fun syncFreezeReminders(app: TalosApp, cluster: String?, status: ArgoStatus, now: Long = System.currentTimeMillis()) {
    if (cluster == null) return
    val running = status.runningIchorFreezes.filter { it.window.endsAt > now }
    val plan = synchronized(scheduled) {
        planFreezeReminders(scheduled[cluster], running.associate { it.key to it.window.endsAt }, now, LEAD_MILLIS)
            ?.also { scheduled[cluster] = it.signature }
    } ?: return
    val work = WorkManager.getInstance(app)
    val tag = TAG_PREFIX + cluster
    // Gone since the last load (ended early, removed); after a restart the worker checks itself.
    plan.cancel.forEach { work.cancelUniqueWork(tag + it) }
    running.forEach { pw ->
        val delay = plan.schedule[pw.key] ?: return@forEach
        val request = OneTimeWorkRequestBuilder<FreezeReminderWorker>()
            .setInitialDelay(delay, TimeUnit.MILLISECONDS)
            .addTag(tag)
            .setInputData(reminderData(cluster, pw))
            .build()
        work.enqueueUniqueWork(tag + pw.key, ExistingWorkPolicy.REPLACE, request)
    }
}

private fun reminderData(cluster: String, pw: ProjectWindow): Data = workDataOf(
    KEY_CLUSTER to cluster,
    KEY_PROJECT_NS to pw.project.namespace,
    KEY_PROJECT to pw.project.name,
    KEY_WINDOW to pw.window.id,
    KEY_SCOPE to (pw.window.namespaces + pw.window.applications).joinToString(", "),
    KEY_END to pw.window.endsAt,
)

/** Posts the reminder: "the freeze on apps · demo ends at 15:42", "+1 h" and a tap to the windows. */
class FreezeReminderWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val end = inputData.getLong(KEY_END, 0)
        if (end <= System.currentTimeMillis() || !stillRunning()) return Result.success()
        val res = AppLocale.wrap(applicationContext)
        val project = inputData.getString(KEY_PROJECT).orEmpty()
        val scope = inputData.getString(KEY_SCOPE).orEmpty()
        val text = res.getString(R.string.argo_freeze_reminder_text, listOf(project, scope).filter { it.isNotEmpty() }.joinToString(" · "), clock(end))
        val extend = PendingIntent.getBroadcast(
            applicationContext,
            notificationId(inputData).hashCode(),
            Intent(applicationContext, FreezeExtendReceiver::class.java).setAction(ACTION_EXTEND).apply {
                STRING_KEYS.forEach { putExtra(it, inputData.getString(it)) }
                putExtra(KEY_END, end)
            },
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        postFreezeNotice(applicationContext, notificationId(inputData), res.getString(R.string.argo_freeze_reminder_title), text) {
            addAction(0, res.getString(R.string.argo_freeze_extend_hour), extend)
        }
        return Result.success()
    }

    /** False when the freeze was ended or changed meanwhile; true when that cannot be told (another cluster, offline). */
    private suspend fun stillRunning(): Boolean {
        val app = applicationContext as TalosApp
        val stored = app.configRepository.config.value ?: runCatching { app.configRepository.load() }.getOrElse { return true }
        if (stored?.activeSummary?.fingerprint != inputData.getString(KEY_CLUSTER)) return true
        val status = runCatching { app.gitOpsRepository.argoCD() }.getOrNull() ?: return true
        val window = inputData.getString(KEY_WINDOW)
        return status.runningIchorFreezes.any { it.window.id == window && it.project.name == inputData.getString(KEY_PROJECT) }
    }
}

/** "+1 h" from the reminder: the extension runs as work, the receiver must return at once. */
class FreezeExtendReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != ACTION_EXTEND) return
        val extras = intent.extras ?: return
        val data = Data.Builder().apply {
            STRING_KEYS.forEach { putString(it, extras.getString(it)) }
            putLong(KEY_END, extras.getLong(KEY_END))
        }.build()
        // Once per freeze: a second tap would find the window already changed by the first.
        NotificationManagerCompat.from(context).cancel(notificationId(data).hashCode())
        WorkManager.getInstance(context).enqueueUniqueWork(
            "argo-freeze-extend|" + notificationId(data),
            ExistingWorkPolicy.KEEP,
            OneTimeWorkRequestBuilder<FreezeExtendWorker>().setInputData(data).build(),
        )
    }
}

/** Extends the freeze by an hour on the cluster it was made on, if that is still the active one. */
class FreezeExtendWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val app = applicationContext as TalosApp
        val res = AppLocale.wrap(applicationContext)
        val id = notificationId(inputData)
        val stored = app.configRepository.config.value
            ?: runCatching { app.configRepository.load() }.getOrElse { return Result.retry() }
        val title = res.getString(R.string.argo_freeze_reminder_title)
        if (stored?.activeSummary?.fingerprint != inputData.getString(KEY_CLUSTER)) {
            postFreezeNotice(applicationContext, id, title, res.getString(R.string.argo_freeze_extend_other_cluster))
            return Result.success()
        }
        val options = ArgoFreezeOptions(window = inputData.getString(KEY_WINDOW).orEmpty(), minutes = FREEZE_EXTEND_MINUTES)
        val error = runCatching {
            app.gitOpsRepository.argoFreeze(inputData.getString(KEY_PROJECT_NS).orEmpty(), inputData.getString(KEY_PROJECT).orEmpty(), ArgoFreezeAction.EXTEND, options)
        }.exceptionOrNull()
        val end = inputData.getLong(KEY_END, 0) + FREEZE_EXTEND_MINUTES * 60_000L
        val text = if (error == null) res.getString(R.string.argo_freeze_extended_until, clock(end))
        else res.getString(R.string.argo_freeze_extend_failed, error.uiText().resolve(res))
        postFreezeNotice(applicationContext, id, title, text)
        // The extended window has a new id: read the freezes again to schedule its reminder.
        if (error == null) runCatching { syncFreezeReminders(app, inputData.getString(KEY_CLUSTER), app.gitOpsRepository.argoCD()) }
        return Result.success()
    }
}

/** One per freeze: two freezes of a project ending together each get their reminder. */
private fun notificationId(data: Data): String =
    "argo-freeze|${data.getString(KEY_CLUSTER)}|${data.getString(KEY_PROJECT_NS)}/${data.getString(KEY_PROJECT)}/${data.getString(KEY_WINDOW)}"

private fun clock(millis: Long): String = DateFormat.getTimeInstance(DateFormat.SHORT).format(Date(millis))

/**
 * A freeze notice on the GitOps alerts channel; a tap opens the sync windows (of the active cluster:
 * switching clusters from a notification would race the deep link).
 */
private fun postFreezeNotice(context: Context, key: String, title: String, text: String, extra: NotificationCompat.Builder.() -> Unit = {}) {
    if (!canPostNotifications(context)) return
    ensureAlertChannels(context)
    val app = context.applicationContext as TalosApp
    val intent = Intent(context, MainActivity::class.java)
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        .putExtra(MainActivity.EXTRA_OPEN, DeepLink.ARGO_WINDOWS.name)
    val open = PendingIntent.getActivity(context, key.hashCode(), intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    val builder = alertNotification(context, AlertChannel.GITOPS, title, text, open, hideOnLockScreen = app.appLock.enabled.value).apply(extra)
    try {
        NotificationManagerCompat.from(context).notify(key.hashCode(), builder.build())
    } catch (_: SecurityException) {
        // Permission revoked between the check and the post; nothing to do.
    }
}
