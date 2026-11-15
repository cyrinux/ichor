package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/relay_pair.go and relay_payload.go: the in-cluster ichor-relay found through
// the Kubernetes API service proxy, the device the phone pairs with it, and the alerts it pushes.

/**
 * The relays among the cluster's Services, the likeliest first; [canPair] false with [reason]
 * (the RBAC refusal, no relay, "demo") when this config cannot read the pairing token.
 */
@Serializable
data class RelayDiscovery(val sources: List<PromSource> = emptyList(), val canPair: Boolean = false, val reason: String = "")

/** What the phone needs before registering: the relay's VAPID public key for the distributor. */
@Serializable
data class RelayPairing(val vapidPublicKey: String = "", val relayVersion: String = "")

/** What the phone registers; Go adds the id, the cluster's fingerprint and the creation time. */
@Serializable
data class RelayDevice(
    val platform: String,
    val endpoint: String,
    val p256dh: String = "",
    val auth: String = "",
    /** The cluster's name as the user sees it; empty for none. */
    val label: String = "",
) {
    companion object {
        const val UNIFIED_PUSH = "unifiedpush"
    }
}

@Serializable
data class RelayRegistered(val id: String = "")

/** A pushed alert as Go checked it: [severity] folded (critical, warning, info, other), [status] firing or resolved. */
@Serializable
data class RelayAlert(
    val fingerprint: String = "",
    val alertname: String = "",
    val severity: String = "",
    val status: String = "",
    val namespace: String = "",
    val summary: String = "",
    val startsAt: Long = 0,
) {
    val firing: Boolean get() = status == "firing"
}

/** A decrypted push: [cluster] the fingerprint it was paired for; [omitted] alerts left out to fit ([truncated]). */
@Serializable
data class RelayPayload(
    val cluster: String = "",
    val alerts: List<RelayAlert> = emptyList(),
    val truncated: Boolean = false,
    val omitted: Int = 0,
    val sentAt: Long = 0,
)
