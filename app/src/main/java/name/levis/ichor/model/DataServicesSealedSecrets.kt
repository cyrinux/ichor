package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_sealedsecrets.go (see DataServices.kt). Names and status only: the
// reader never decodes an encrypted value or a sealing key's data.

@Serializable
data class SealedSecretsStatus(
    val version: String = "",
    val error: String = "",
    /** The worst of the items, the controller and the sealing keys. */
    val health: String = "",
    /** Section-level wire values of [SealedSecretsReason]: the controller and the keys. */
    val reasons: List<String> = emptyList(),
    /** null when no Deployment runs the controller's image. */
    val controller: SealedSecretsController? = null,
    /** Problems first. */
    val items: List<SealedSecretItem> = emptyList(),
    val truncated: Boolean = false,
    /** All of them; [items] is capped at 500. */
    val secretCount: Int = 0,
    /** Newest first. */
    val keys: List<SealingKey> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val reasonList: List<SealedSecretsReason> get() = reasons.mapNotNull(SealedSecretsReason::from)
}

@Serializable
data class SealedSecretsController(
    val namespace: String = "",
    val name: String = "",
    val ready: Boolean = false,
    val replicas: Int = 0,
    val readyReplicas: Int = 0,
    val health: String = "",
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
}

@Serializable
data class SealedSecretItem(
    val namespace: String = "",
    val name: String = "",
    /** The data keys of the Secret it unseals into, never their values. */
    val keys: List<String> = emptyList(),
    val keyCount: Int = 0,
    val type: String = "",
    val immutable: Boolean = false,
    /** strict, namespace-wide or cluster-wide. */
    val scope: String = "",
    val synced: Boolean = false,
    /** The Synced condition's reason and message. */
    val reason: String = "",
    val message: String = "",
    val lastTransitionTime: Long = 0,
    val observedGeneration: Long = 0,
    val stale: Boolean = false,
    /** Creation time, unix ms (the Go field is named "age"). */
    @SerialName("age") val createdAt: Long = 0,
    val health: String = "",
    /** Wire values of [SealedSecretsReason]. */
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<SealedSecretsReason> get() = reasons.mapNotNull(SealedSecretsReason::from)
    val objectRef: KubeObjectRef get() = KubeObjectRef("bitnami.com", "v1alpha1", "sealedsecrets", "SealedSecret", namespace, name, editable = false)
}

@Serializable
data class SealingKey(
    val namespace: String = "",
    val name: String = "",
    /** active or compromised, as the key's label says. */
    val state: String = "",
    /** Unix ms, 0 when unknown. */
    val createdAt: Long = 0,
    /** Elapsed ms. */
    val age: Long = 0,
) {
    val active: Boolean get() = state == "active"
    val compromised: Boolean get() = state == "compromised"
}

/** Why a SealedSecret, the controller or the sealing keys are not ok, as the Go core names it. */
enum class SealedSecretsReason(val wire: String) {
    NOT_SYNCED("notSynced"),
    UNMANAGED("unmanaged"),
    STALE("stale"),
    CONTROLLER_DOWN("controllerDown"),
    NO_KEYS("noKeys"),
    KEY_COMPROMISED("keyCompromised"),
    KEY_STALE("keyStale"),
    ;

    companion object {
        fun from(wire: String): SealedSecretsReason? = entries.firstOrNull { it.wire == wire }
    }
}
