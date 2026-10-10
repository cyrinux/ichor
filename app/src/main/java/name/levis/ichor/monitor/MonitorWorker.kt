package name.levis.ichor.monitor

import android.content.Context
import androidx.glance.appwidget.updateAll
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.TalosApp
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.HistoryForecast
import name.levis.ichor.model.HistoryRecord
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.heldBackForVpn
import name.levis.ichor.model.needsEndpoint
import name.levis.ichor.model.wakeTargets
import name.levis.ichor.model.wolKey
import name.levis.ichor.widget.ClusterWidget

/**
 * Periodic check of every watched cluster (see [monitoredContexts]), concurrently, each within
 * [CLUSTER_CHECK_TIMEOUT_MS]: its fresh snapshot is diffed against its own previous one. A cluster
 * unreachable as a whole (e.g. off VPN) keeps its previous snapshot (see [evaluate]), so you are
 * not spammed; several such checks in a row may alert once (opt-in, see [reachStep]).
 */
class MonitorWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {

    override suspend fun doWork(): Result {
        val app = applicationContext as TalosApp
        val store = app.monitorStore
        // Unreadable for now is not deleted: keep the snapshots and check again later.
        val stored = app.configRepository.config.value
            ?: runCatching { app.configRepository.load() }.getOrElse { return Result.retry() }
        if (stored == null) {
            // Config deleted: forget what we saw so the widget stops showing it.
            store.clearSnapshots()
            ClusterWidget().updateAll(applicationContext)
            return Result.success()
        }

        val now = System.currentTimeMillis()
        val before = store.state.value
        // Screenshot mode fakes every name the checks read: no history from those.
        val maskedBefore = app.uiPreferences.privacyMask.value.enabled
        val reads = readAll(app, stored, before, now)
        val evaluated = monitorRun(
            before,
            reads,
            now,
            unreachableAlerts = store.unreachableAlerts.value,
            unreachableRuns = store.unreachableRuns.value,
            active = stored.activeSummary?.let(::monitorKeyOf).orEmpty(),
        )
        val firstPass = evaluated.state
        store.saveState(firstPass)
        val masked = maskedBefore || app.uiPreferences.privacyMask.value.enabled
        withContext(Dispatchers.IO) {
            historyRecords(before, reads, firstPass, now, masked).forEach { (fingerprint, record) ->
                app.historyStore.append(fingerprint, TalosJson.encodeToString(HistoryRecord.serializer(), record), now)
            }
        }
        // The volume trends read the ring with this run's record in it; none from faked names
        // (screenshot mode), so the open ones are kept as they are.
        val trendsOn = store.storageTrendAlerts.value
        val forecasts = if (trendsOn && !masked) storageForecasts(app, reads, now) else emptyMap()
        val run = withStorageTrends(evaluated, before, reads, forecasts, enabled = trendsOn)
        if (run.state != firstPass) store.saveState(run.state)
        scheduleWidgetStaleRefresh(applicationContext, run.state.clusters.values, now)

        if (store.alertsEnabled.value) {
            // Snoozed from their notification: nothing, problem or resolved, until it is over.
            app.alertSnoozes.prune(now)
            run.alerts.forEach { post(app, it, now) }
        }
        ClusterWidget().updateAll(applicationContext)
        return Result.success()
    }
}

/**
 * Reads every watched cluster at once. A Talos cluster without an endpoint yet cannot be checked
 * (nor shown); a VPN-only one while no VPN is up is skipped, its state kept as it was.
 */
private suspend fun readAll(app: TalosApp, stored: StoredConfig, before: MonitorState, now: Long): List<ClusterRead> = coroutineScope {
    val vpnUp = app.vpn.isUp()
    monitoredContexts(stored.summary, stored.activeContext, app.unwatchedClusters.fingerprints.value)
        .filterNot { it.needsEndpoint }
        .map { context ->
            async {
                if (heldBackForVpn(app.vpnOnly.fingerprints.value, context.fingerprint, vpnUp)) {
                    ClusterRead(context, snapshot = null, skipped = true)
                } else {
                    val reading = readWithin { readCluster(app, context, before.clusters[monitorKeyOf(context)], now) }
                    ClusterRead(context, reading?.snapshot, detail = reading?.detail)
                }
            }
        }
        .awaitAll()
}

/** The volume forecast of each cluster whose storage this run read, by monitor key (see [wantsStorageForecast]). */
private suspend fun storageForecasts(app: TalosApp, reads: List<ClusterRead>, now: Long): Map<String, HistoryForecast> {
    val critical = app.monitorStore.storageCriticalPercent.value
    return reads.filter(::wantsStorageForecast).mapNotNull { read ->
        app.historyRepository.forecast(read.context.fingerprint, now, critical)?.let { monitorKeyOf(read.context) to it }
    }.toMap()
}

/** Posts the alerts of one cluster that are not snoozed, named after it as the app shows it. */
private fun post(app: TalosApp, cluster: ClusterAlerts, now: Long) {
    val context = cluster.context
    val fingerprint = context.fingerprint
    val hide = app.appLock.enabled.value
    val label = ClusterLabels(app.clusterNames.names.value, app.uiPreferences.privacyMask.value.enabled).of(context)
    val canReboot = context.allows(Feature.POWER)
    app.alertSnoozes.unsnoozed(cluster.alerts, fingerprint, now).forEach { alert ->
        val actions = alert.actions(canWake = canWake(app, fingerprint, alert), canReboot = canReboot)
        postAlert(
            app,
            alert,
            hideOnLockScreen = hide,
            clusterId = context.clusterId,
            actions = actions,
            fingerprint = fingerprint,
            cluster = label,
        )
    }
}

/**
 * What one run read of a cluster: [snapshot] null when it could not be read; [skipped] when it was
 * not tried; [detail] what only its history record keeps.
 */
data class ClusterRead(
    val context: ContextSummary,
    val snapshot: ClusterSnapshot?,
    val skipped: Boolean = false,
    val detail: HistoryDetail? = null,
)

/** The alerts of one cluster in a run. */
data class ClusterAlerts(val context: ContextSummary, val alerts: List<Alert>)

data class MonitorRun(val state: MonitorState, val alerts: List<ClusterAlerts>)

/**
 * One run over [reads]: each cluster's snapshot is evaluated against its own previous one in
 * [before], and its reachability counted (see [reachStep]). A cluster that could not be read keeps
 * its snapshot; a skipped one keeps its count too. Clusters not read (removed, no longer watched)
 * are forgotten. [active]: the key of the cluster on screen.
 */
fun monitorRun(
    before: MonitorState,
    reads: List<ClusterRead>,
    now: Long,
    unreachableAlerts: Boolean,
    unreachableRuns: Int,
    active: String,
): MonitorRun {
    val clusters = mutableMapOf<String, ClusterSnapshot>()
    val reach = mutableMapOf<String, Reach>()
    val alerts = mutableListOf<ClusterAlerts>()
    reads.forEach { read ->
        val key = monitorKeyOf(read.context)
        val previous = before.clusters[key]
        val snapshot = read.snapshot
        if (read.skipped) {
            previous?.let { clusters[key] = it }
            before.reach[key]?.let { reach[key] = it }
            return@forEach
        }
        val evaluation = snapshot?.let { evaluate(previous, it, now) }
        (evaluation?.next ?: previous)?.let { clusters[key] = it }
        val reachable = snapshot != null && !snapshot.unreachableAsAWhole
        val step = reachStep(before.reach[key], reachable, unreachableRuns, unreachableAlerts)
        // An answer resets the count: nothing to keep then.
        if (step.next != Reach()) reach[key] = step.next
        val found = evaluation?.alerts.orEmpty() + listOfNotNull(step.alert)
        if (found.isNotEmpty()) alerts += ClusterAlerts(read.context, found)
    }
    return MonitorRun(MonitorState(clusters, reach, active), alerts)
}

/** Whether Wake-on-LAN knows where to wake the node of a node [alert]: a saved target or a MAC it was seen with. */
private fun canWake(app: TalosApp, fingerprint: String, alert: Alert): Boolean {
    if (fingerprint.isBlank() || !alert.key.startsWith("node:")) return false
    val key = wolKey(fingerprint, alert.key.substringAfter(':'))
    return wakeTargets(app.wakeOnLan.targets.value[key], app.wakeOnLan.seen.value[key].orEmpty()).isNotEmpty()
}

/** Redraws the widget from the stored snapshots, without any network call. */
class WidgetRefreshWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        ClusterWidget().updateAll(applicationContext)
        return Result.success()
    }
}
