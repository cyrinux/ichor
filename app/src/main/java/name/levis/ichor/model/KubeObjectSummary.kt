package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_summary.go: `kubectl describe` for any kind, CRDs included.

/** One object summed up (KubeObjectSummary). Times are Unix milliseconds. */
@Serializable
data class KubeObjectSummary(
    val kind: String = "",
    val apiVersion: String = "",
    val namespace: String = "",
    val name: String = "",
    /** "ok", "warn", "bad" or "none": the worst condition's tone, or the phase's. */
    val health: String = "none",
    /** What the worst condition says ("Ready: ContainersNotReady"). */
    val healthReason: String = "",
    val phase: String = "",
    val conditions: List<SummaryCondition> = emptyList(),
    val owners: List<ObjectOwner> = emptyList(),
    val labels: Map<String, String> = emptyMap(),
    val annotations: Map<String, String> = emptyMap(),
    val created: Long = 0,
    /** When its deletion was asked, 0 when none is pending. */
    val deleting: Long = 0,
    val finalizers: List<String> = emptyList(),
    val highlights: List<SpecHighlight> = emptyList(),
    val events: List<KubeEvent> = emptyList(),
    /** Why the events could not be read, "" when they were. */
    val eventsError: String = "",
) {
    val healthTone: CellTone get() = toneOf(health)
}

/** An entry of status.conditions, with the tone it reads as. */
@Serializable
data class SummaryCondition(
    val type: String = "",
    val status: String = "",
    val reason: String = "",
    val message: String = "",
    val lastTransition: Long = 0,
    val tone: String = "none",
) {
    val toneValue: CellTone get() = toneOf(tone)
}

/**
 * An object up the chain: an ownerReference ([via] "owner"), or the Flux object, Argo CD
 * Application or Helm release that manages it ("flux", "argocd", "helm").
 */
@Serializable
data class ObjectOwner(
    val via: String = "",
    val group: String = "",
    val version: String = "",
    val resource: String = "",
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val namespaced: Boolean = false,
    val verbs: List<String> = emptyList(),
    val controller: Boolean = false,
) {
    val isHelmRelease: Boolean get() = via == VIA_HELM

    /** Discovery knew its kind: its own summary can open. */
    val openable: Boolean get() = resource.isNotEmpty() && version.isNotEmpty() && name.isNotEmpty()

    /** The browser's reference to it, null when it cannot open as an object. */
    fun toRef(): KubeObjectRef? =
        if (openable) KubeObjectRef(group, version, resource, kind, namespace, name, editable = "update" in verbs && "get" in verbs) else null

    companion object {
        const val VIA_OWNER = "owner"
        const val VIA_FLUX = "flux"
        const val VIA_ARGO = "argocd"
        const val VIA_HELM = "helm"
    }
}

/** A spec field shown first; [key] names it (replicas, selector, image, node, suspended, schedule). */
@Serializable
data class SpecHighlight(val key: String = "", val value: String = "")

/** The tone Go names ("ok", "warn", "bad"), NONE for anything else. */
fun toneOf(tone: String): CellTone = when (tone) {
    "ok" -> CellTone.OK
    "warn" -> CellTone.WARN
    "bad" -> CellTone.BAD
    else -> CellTone.NONE
}
