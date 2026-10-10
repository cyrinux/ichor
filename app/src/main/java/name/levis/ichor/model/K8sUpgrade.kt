package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/upgradek8s.go, upgradek8s_run.go and upgradek8s_versions.go.

/** The versions offered: newer than [from], at most one minor up, inside the Talos range. */
@Serializable
data class K8sVersionChoice(
    val from: String = "",
    val supportedRange: String = "",
    val lo: Int = 0,
    val hi: Int = 0,
    val versions: List<String> = emptyList(),
    /** Why [versions] may miss some: the release list could not be read. */
    val warning: String? = null,
)

/** One component of one node, in the order the run follows. */
@Serializable
data class K8sPlanStep(
    /** controlplane | kubelet */
    val kind: String = "",
    val node: String = "",
    val hostname: String = "",
    /** apiserver | controller-manager | scheduler | proxy | kubelet */
    val component: String = "",
    val image: String = "",
    val current: String = "",
    val changed: Boolean = false,
) {
    val name: String get() = hostname.ifEmpty { node }
    val controlPlane: Boolean get() = kind == "controlplane"

    /** The tag the component runs now ("v1.34.0"), from its current image. */
    val currentTag: String get() = current.substringAfterLast(':', "").substringBefore('@')
}

/** A deprecated API still requested; severity "critical" when the target removes it. */
@Serializable
data class K8sDeprecatedApi(val api: String = "", val removedIn: String = "", val severity: String = "")

@Serializable
data class K8sUpgradePlan(
    val from: String = "",
    val to: String = "",
    val supported: Boolean = false,
    val supportedRange: String = "",
    val talosVersion: String = "",
    val steps: List<K8sPlanStep> = emptyList(),
    val deprecatedApis: List<K8sDeprecatedApi> = emptyList(),
    val blockers: List<String> = emptyList(),
    val warnings: List<String> = emptyList(),
) {
    val controlPlaneSteps: List<K8sPlanStep> get() = steps.filter { it.controlPlane }
    val kubeletSteps: List<K8sPlanStep> get() = steps.filter { !it.controlPlane }
    val changes: Int get() = steps.count { it.changed }
    val canStart: Boolean get() = blockers.isEmpty() && changes > 0
}

/** A step of the run: the node at [index] among the [total] to change, its phase and components. */
@Serializable
data class K8sUpgradeProgress(
    /** controlplane | kubelet | proxy | done */
    val phase: String = "",
    val index: Int = 0,
    val total: Int = 0,
    val node: String = "",
    val hostname: String = "",
    val component: String = "",
    val message: String = "",
    val dryRun: Boolean = false,
    val at: Long = 0,
) {
    val name: String get() = hostname.ifEmpty { node }

    companion object {
        const val CONTROL_PLANE = "controlplane"
        const val KUBELET = "kubelet"
        const val PROXY = "proxy"
        const val DONE = "done"
    }
}

/** [version] (1.X.Y) is one [choice] allows: after its current version, at most one minor up, in range. */
fun k8sVersionAllowed(choice: K8sVersionChoice, version: String): Boolean {
    val v = k8sParts(version) ?: return false
    val from = k8sParts(choice.from) ?: return false
    if (v[0] != 1 || from[0] != 1) return false
    if (compareValues(v[1], from[1]).let { it < 0 } || v[1] > from[1] + 1) return false
    if (v[1] == from[1] && v[2] <= from[2]) return false
    if (choice.lo > 0 && v[1] < choice.lo) return false
    if (choice.hi > 0 && v[1] > choice.hi) return false
    return true
}

private fun k8sParts(version: String): List<Int>? {
    val parts = version.trim().removePrefix("v").split('.')
    if (parts.size != 3) return null
    return parts.map { it.toIntOrNull() ?: return null }
}
