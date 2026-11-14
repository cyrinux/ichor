package name.levis.ichor.monitor

import android.content.Context
import android.util.Log
import kotlinx.coroutines.CancellationException
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.GoCall
import name.levis.ichor.data.RelayRepository
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.RelayDevice
import name.levis.ichor.model.RelayPayload
import name.levis.ichor.i18n.AppLocale
import org.unifiedpush.android.connector.UnifiedPush

// Push from ichor-relay on Android: the cluster's fingerprint is the UnifiedPush instance, the
// distributor hands RelayPushService an endpoint to register with the relay and the decrypted
// messages, which go through the same postAlert as the poller's.

private const val TAG = "IchorRelay"

/** The relay calls of the cluster [context], whichever cluster is on screen (like a background check). */
fun relayRepository(app: TalosApp, context: String): RelayRepository = RelayRepository(
    GoCall(app.configRepository, app.kubeServers, offline = null, pinned = context, idle = app.maskSwitcher::awaitIdle, googleRenewer = app.googleTokenRenewer),
)

/** The stored context of the cluster [fingerprint] (the one on screen first); null when gone or locked away. */
fun relayContext(app: TalosApp, fingerprint: String): ContextSummary? {
    val stored = app.configRepository.config.value ?: return null
    val same = stored.summary.contexts.filter { it.fingerprint == fingerprint && fingerprint.isNotBlank() }
    return same.firstOrNull { it.name == stored.activeContext } ?: same.firstOrNull()
}

/** The cluster's name as the app shows it, in the notification header and the relay's device label. */
fun relayLabel(app: TalosApp, context: ContextSummary): String =
    ClusterLabels(app.clusterNames.names.value, app.uiPreferences.privacyMask.value.enabled).of(context)

/**
 * Registers the endpoint the distributor gave [fingerprint] with its relay; on failure the
 * endpoint is kept and [retryRelayRegistrations] tries again at the next background check.
 */
suspend fun registerRelayEndpoint(app: TalosApp, fingerprint: String) {
    val pair = app.relayStore.read(fingerprint)
    val source = pair.source ?: return
    if (!pair.pending) return
    val context = relayContext(app, fingerprint) ?: return
    val device = RelayDevice(RelayDevice.UNIFIED_PUSH, pair.endpoint, pair.p256dh, pair.auth, label = pair.label)
    try {
        val id = relayRepository(app, context.name).register(source, device)
        val distributor = UnifiedPush.getAckDistributor(app).orEmpty()
        app.relayStore.update(fingerprint) {
            it.copy(deviceId = id, distributor = distributor, pairedAt = System.currentTimeMillis(), problem = "", detail = "")
        }
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        app.relayStore.update(fingerprint) { it.copy(problem = RelayProblem.REGISTER, detail = e.message.orEmpty()) }
    }
}

/** The endpoints the relay has not taken yet, registered again (called by each background check). */
suspend fun retryRelayRegistrations(app: TalosApp) {
    app.relayStore.pairs.value.filterValues { it.pending }.keys.forEach { registerRelayEndpoint(app, it) }
}

/** Stops the push of the clusters no longer on the phone, and forgets them. */
fun retainRelayPairs(app: TalosApp, fingerprints: List<String>) {
    app.relayStore.pairs.value.keys.filter { it !in fingerprints }.forEach { runCatching { UnifiedPush.unregister(app, it) } }
    app.relayStore.retain(fingerprints)
}

/**
 * Posts what the relay pushed for [fingerprint] like the poller would: only while the user wants
 * background and Alertmanager alerts and this cluster's push, the snoozed alerts left out.
 */
fun postRelayPush(app: TalosApp, fingerprint: String, payload: RelayPayload, now: Long = System.currentTimeMillis()) {
    val monitor = app.monitorStore
    val pair = app.relayStore.read(fingerprint)
    if (!pair.enabled || payload.cluster != fingerprint) return
    app.relayStore.update(fingerprint) { it.copy(lastPushAt = now) }
    if (!monitor.alertsEnabled.value || !monitor.alertmanagerWatched.value) return
    val context = relayContext(app, fingerprint)
    val label = context?.let { relayLabel(app, it) }
    val hide = app.appLock.enabled.value
    val canPage = app.onCallStore.read(fingerprint).isSet
    relayAlerts(payload).filterNot { app.alertSnoozes.isSnoozed(fingerprint, it.key, now) }.forEach { alert ->
        val actions = alert.actions(canWake = false, canReboot = false, canAcknowledgePage = alert.kind == AlertKind.AM_FIRING && canPage)
        postAlert(app, alert, hideOnLockScreen = hide, clusterId = context?.clusterId, actions = actions, fingerprint = fingerprint, cluster = label)
    }
    val omitted = relayOmitted(payload)
    if (omitted > 0) postRelayOmitted(app, fingerprint, label, omitted)
}

/** "N more alerts in Alertmanager" once a push left some out; a tap opens the app. */
private fun postRelayOmitted(context: Context, fingerprint: String, label: String?, omitted: Int) {
    val res = AppLocale.wrap(context)
    val title = label ?: res.getString(R.string.monitor_alertmanager)
    postAlertNotice(context, fingerprint, "am:relay-omitted", title, res.getString(R.string.relay_truncated_n, omitted), AlertChannel.CLUSTER)
}

/** Logs a count without any cluster name (the instance is a fingerprint). */
fun logRelay(message: String) {
    Log.i(TAG, message)
}
