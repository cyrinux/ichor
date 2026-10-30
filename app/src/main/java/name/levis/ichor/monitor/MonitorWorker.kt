package name.levis.ichor.monitor

import android.content.Context
import android.util.Log
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ProcessLifecycleOwner
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withPermit
import kotlinx.coroutines.withContext
import name.levis.ichor.TalosApp
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.HistoryForecast
import name.levis.ichor.model.HistoryRecord
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.AlertRules
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.heldBackForVpn
import name.levis.ichor.model.needsEndpoint
import name.levis.ichor.model.wakeTargets
import name.levis.ichor.model.wolKey
import name.levis.ichor.util.StartupTrace
import name.levis.ichor.widget.refreshFleetGlance
import name.levis.ichor.widget.updateAllWidgets

/**
 * Periodic check of every watched cluster (see [monitoredContexts]), each within
 * [CLUSTER_CHECK_TIMEOUT_MS]: its fresh snapshot is diffed against its own previous one. A cluster
 * unreachable as a whole (e.g. off VPN) keeps its previous snapshot (see [evaluate]), so you are
 * not spammed; several such checks in a row may alert once (opt-in, see [reachStep]).
 *
 * [MONITOR_CLUSTER_PARALLELISM] (4) clusters are read at a time, in [monitoredOrder]: the ones
 * the last run could not read first, the one on screen last while the app is in the foreground,
 * so a run with many clusters neither floods the Go core and the VPN nor races the screen.
 * [MONITOR_RUN_NOW_DELAY_MS] (5 s): how long a check asked for while the app is in the foreground
 * and the overview has not loaded yet waits, so an import's first check follows the home screen
 * (see [syncMonitoring]). No user setting for either.
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
            updateAllWidgets(applicationContext)
            app.syncWear()
            return Result.success()
        }

        val now = System.currentTimeMillis()
        val before = store.state.value
        // Screenshot mode fakes every name the checks read: no history from those.
        val maskedBefore = app.uiPreferences.privacyMask.value.enabled
        // Renewed first, so the reads use fresh tokens and an idle-timeout session is kept alive.
        val labels = ClusterLabels(app.clusterNames.names.value, maskedBefore)
        val watched = monitoredContexts(stored.summary, stored.activeContext, app.unwatchedClusters.fingerprints.value)
        val signIns = renewSignIns(app, watched) { labels.of(it) }
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

        // The pull requests opened from the phone, on the clusters this run could read: their new
        // states are kept even with alerts off, so turning them on later tells nothing stale.
        val proposals = proposalAlerts(app, reads.filter { !it.skipped && it.snapshot != null }.map { it.context }, now)

        if (store.alertsEnabled.value) {
            // Snoozed from their notification: nothing, problem or resolved, until it is over.
            app.alertSnoozes.prune(now)
            app.alertMutes.prune(now)
            // A recovered alert's acknowledgement ends: firing again, it repeats again.
            firingKeys(reads, run.state).forEach { (cluster, keys) -> app.alertAcks.retainFiring(cluster, keys) }
            val ongoing = ongoingAlerts(reads, run.state, run.alerts)
            val notified = postAll(app, run.alerts + proposals + signIns.alerts, ongoing, run.state, now)
            if (notified != run.state) store.saveState(notified)
            store.setSignInNotified(signIns.notified)
        }
        app.refreshFleetGlance(now, stored.summary)
        updateAllWidgets(applicationContext)
        app.syncWear()
        return Result.success()
    }
}

/**
 * Reads every watched cluster, [MONITOR_CLUSTER_PARALLELISM] at a time in [monitoredOrder]. A
 * Talos cluster without an endpoint yet cannot be checked (nor shown); a VPN-only one while no
 * VPN is up is skipped, its state kept as it was.
 */
private suspend fun readAll(app: TalosApp, stored: StoredConfig, before: MonitorState, now: Long): List<ClusterRead> = coroutineScope {
    val vpnUp = app.vpn.isUp()
    val contexts = monitoredContexts(stored.summary, stored.activeContext, app.unwatchedClusters.fingerprints.value)
        .filterNot { it.needsEndpoint }
    val ordered = monitoredOrder(contexts, stored.activeContext, isForeground(), lastFailedKeys(before))
    StartupTrace.markOnce("monitor: ${ordered.size} clusters, $MONITOR_CLUSTER_PARALLELISM at a time")
    val permits = Semaphore(MONITOR_CLUSTER_PARALLELISM)
    ordered
        .map { context ->
            async {
                permits.withPermit {
                    if (heldBackForVpn(app.vpnOnly.fingerprints.value, context.fingerprint, vpnUp)) {
                        ClusterRead(context, snapshot = null, skipped = true)
                    } else {
                        readWithin { readClusterOrSkip(app, context, before.clusters[monitorKeyOf(context)], now) }
                            ?: ClusterRead(context, snapshot = null)
                    }
                }
            }
        }
        .awaitAll()
}

/** Whether an activity of the app is on screen (started), from the process lifecycle. */
internal fun isForeground(): Boolean =
    ProcessLifecycleOwner.get().lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)

/** The volume forecast of each cluster whose storage this run read, by monitor key (see [wantsStorageForecast]). */
private suspend fun storageForecasts(app: TalosApp, reads: List<ClusterRead>, now: Long): Map<String, HistoryForecast> {
    val critical = app.monitorStore.storageCriticalPercent.value
    return reads.filter(::wantsStorageForecast).mapNotNull { read ->
        app.historyRepository.forecast(read.context.fingerprint, now, critical)?.let { monitorKeyOf(read.context) to it }
    }.toMap()
}

/**
 * Posts a run's alerts under the alert rules (quiet hours, minimum severity, digest, repeat),
 * decided once over every cluster's: each one Go says to notify on its own, one digest per
 * digest cluster; the ones quiet hours or the minimum severity muted are recorded for the
 * history, the snoozed ones dropped. The [ongoing] ones (see [ongoingAlerts]) are posted again
 * when Go says their repeat is due. Without rules, or when Go refuses them, every alert not
 * snoozed is posted as before (see [alertPlan]) and nothing repeats: a rules error never drops
 * an alert. Returns [state] with the posted alerts' times (see [withNotified]).
 */
private suspend fun postAll(app: TalosApp, clusters: List<ClusterAlerts>, ongoing: List<ClusterAlerts>, state: MonitorState, now: Long): MonitorState {
    if (clusters.isEmpty() && ongoing.isEmpty()) return state
    val rules = app.monitorStore.alertRules.value
    val snoozed = { cluster: String, key: String -> app.alertSnoozes.isSnoozed(cluster, key, now) }
    val inputs = alertInputs(clusters, snoozed)
    val ongoingInputs = if (rules == AlertRules.NONE) emptyList() else repeatInputs(ongoing, state, snoozed, app.alertAcks::isAcknowledged)
    val decisions = if (rules == AlertRules.NONE || inputs.size + ongoingInputs.size == 0) {
        null
    } else {
        try {
            app.alertRulesRepository.decide(rules, inputs + ongoingInputs, now)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Log.w(TAG, "alert rules not applied, posting every alert: ${e.message}")
            null
        }
    }
    val plan = alertPlan(clusters, inputs, decisions?.let { it.copy(decisions = it.decisions.take(inputs.size)) })
    val repeats = repeatPlan(ongoing, ongoingInputs, decisions?.decisions?.drop(inputs.size), now)
    // Posted alerts that will repeat get Acknowledge.
    val repeating = decisions?.decisions.orEmpty().filter { it.repeats }.map { it.cluster to it.key }.toSet()
    plan.notify.forEach { post(app, it, repeating) }
    repeats.forEach { post(app, ClusterAlerts(it.context, listOf(it.alert)), repeating, stillFiringMinutes = it.minutes) }
    plan.muted.forEach { app.alertMutes.record(it.context.fingerprint, it.alert.key, AlertMute(it.reason, it.until, now)) }
    val hide = app.appLock.enabled.value
    plan.digests.forEach { postDigest(app, it, clusterLabel(app, it.context), it.context.clusterId, hide) }
    return withNotified(state, plan.notify + repeats.map { ClusterAlerts(it.context, listOf(it.alert)) }, now)
}

private const val TAG = "MonitorWorker"

private fun clusterLabel(app: TalosApp, context: ContextSummary): String =
    ClusterLabels(app.clusterNames.names.value, app.uiPreferences.privacyMask.value.enabled).of(context)

/**
 * Posts the alerts of one cluster, named after it as the app shows it; the ones in [repeating]
 * (fingerprint, key) get Acknowledge. [stillFiringMinutes]: a repeat, since how long it fires.
 */
private fun post(app: TalosApp, cluster: ClusterAlerts, repeating: Set<Pair<String, String>>, stillFiringMinutes: Long? = null) {
    val context = cluster.context
    val fingerprint = context.fingerprint
    val hide = app.appLock.enabled.value
    val label = clusterLabel(app, context)
    val canReboot = context.allows(Feature.POWER)
    cluster.alerts.forEach { alert ->
        val actions = alert.actions(
            canWake = canWake(app, fingerprint, alert),
            canReboot = canReboot,
            canAcknowledge = (fingerprint to alert.key) in repeating,
        )
        postAlert(
            app,
            alert,
            hideOnLockScreen = hide,
            clusterId = context.clusterId,
            actions = actions,
            fingerprint = fingerprint,
            cluster = label,
            stillFiringMinutes = stillFiringMinutes,
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
        (evaluation?.next?.let { withAlertTimes(previous, it, now) } ?: previous)?.let { clusters[key] = it }
        val reachable = snapshot != null && !snapshot.unreachableAsAWhole
        val step = reachStep(before.reach[key], reachable, unreachableRuns, unreachableAlerts)
        // An answer resets the count: nothing to keep then.
        if (step.next != Reach()) reach[key] = step.next
        val found = evaluation?.alerts.orEmpty() + listOfNotNull(step.alert)
        if (found.isNotEmpty()) alerts += ClusterAlerts(read.context, found)
    }
    // The fleet is ranked again once the run is over (refreshFleetGlance): the widget keeps the last one till then.
    return MonitorRun(MonitorState(clusters, reach, active, before.fleet), alerts)
}

/** Whether Wake-on-LAN knows where to wake the node of a node [alert]: a saved target or a MAC it was seen with. */
private fun canWake(app: TalosApp, fingerprint: String, alert: Alert): Boolean {
    if (fingerprint.isBlank() || !alert.key.startsWith("node:")) return false
    val key = wolKey(fingerprint, alert.key.substringAfter(':'))
    return wakeTargets(app.wakeOnLan.targets.value[key], app.wakeOnLan.seen.value[key].orEmpty()).isNotEmpty()
}

/** Redraws the widgets from the stored snapshots, without any network call. */
class WidgetRefreshWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        updateAllWidgets(applicationContext)
        return Result.success()
    }
}
