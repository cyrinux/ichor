package name.levis.ichor.monitor

import androidx.lifecycle.ProcessLifecycleOwner
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.launch
import name.levis.ichor.TalosApp
import name.levis.ichor.data.RelayRepository
import org.unifiedpush.android.connector.FailedReason
import org.unifiedpush.android.connector.PushService
import org.unifiedpush.android.connector.data.PushEndpoint
import org.unifiedpush.android.connector.data.PushMessage

/**
 * What the UnifiedPush distributor delivers for each cluster ([instance]: its fingerprint): the
 * endpoint to register with the cluster's ichor-relay, and the alerts it pushed, decrypted by
 * the connector (RFC 8291) and posted like the poller's. Started by the distributor: no UI,
 * nothing long on the main thread, no cluster credentials kept here.
 */
class RelayPushService : PushService() {
    private val app get() = application as TalosApp

    override fun onNewEndpoint(endpoint: PushEndpoint, instance: String) {
        val keys = endpoint.pubKeySet
        if (keys == null) {
            // Without Web Push keys the relay cannot encrypt for this phone: it would refuse the device.
            app.relayStore.update(instance) { it.copy(problem = RelayProblem.UNDECRYPTED, detail = "") }
            return
        }
        app.relayStore.update(instance) {
            if (!it.enabled) return@update it
            it.copy(endpoint = endpoint.url, p256dh = keys.pubKey, auth = keys.auth, deviceId = "", problem = "", detail = "")
        }
        ProcessLifecycleOwner.get().lifecycleScope.launch { registerRelayEndpoint(app, instance) }
    }

    override fun onMessage(message: PushMessage, instance: String) {
        if (!message.decrypted) {
            // A distributor too old for Web Push: its message cannot be read, the settings row says to update it.
            app.relayStore.update(instance) { it.copy(problem = RelayProblem.UNDECRYPTED, detail = "") }
            logRelay("dropped an undecrypted message")
            return
        }
        val content = String(message.content, Charsets.UTF_8)
        ProcessLifecycleOwner.get().lifecycleScope.launch {
            val payload = runCatching { RelayRepository.decode(content) }.getOrElse {
                logRelay("dropped a payload the core refused")
                return@launch
            }
            postRelayPush(app, instance, payload)
        }
    }

    override fun onRegistrationFailed(reason: FailedReason, instance: String) {
        app.relayStore.update(instance) { it.copy(problem = RelayProblem.DISTRIBUTOR, detail = reason.name) }
    }

    override fun onUnregistered(instance: String) {
        app.relayStore.update(instance) { it.copy(endpoint = "", p256dh = "", auth = "", deviceId = "") }
    }
}
