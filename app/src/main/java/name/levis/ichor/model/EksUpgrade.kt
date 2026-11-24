package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_eks_upgrade.go: upgrading an EKS control plane one Kubernetes minor
// (UpdateClusterVersion), the plan that says what it will do, and the update AWS runs for it.
// installed false: not an EKS sign-in, the Autoscaling screen shows no control plane row.

/**
 * What upgrading the control plane one minor would do. [next] is empty when AWS offers no newer
 * version; [nextKnown] false with a [next] means availability was not checked (the role may not
 * call DescribeClusterVersions). [supportEnds] is the end of standard support of [version], unix
 * ms, 0 when unknown. [updating] is the update in progress, found again when the app reopens.
 * [groups] are every managed node group: each is behind once the control plane moves.
 */
@Serializable
data class EksUpgradePlan(
    val installed: Boolean = false,
    val cluster: String = "",
    val region: String = "",
    val version: String = "",
    val status: String = "",
    val platformVersion: String = "",
    val next: String = "",
    val nextKnown: Boolean = false,
    val supportEnds: Long = 0,
    val updating: NodeGroupUpdate? = null,
    val groupsBehind: List<String> = emptyList(),
    val groups: List<String> = emptyList(),
    val blockers: List<String> = emptyList(),
    val error: String = "",
    /** The AWS action the role may not call ("eks:DescribeCluster"), when that is the error. */
    val forbidden: String = "",
) {
    /** The upgrade may start now: a next minor, no blocker and nothing running. */
    val canUpgrade: Boolean get() = next.isNotEmpty() && blockers.isEmpty() && updating?.inProgress != true && error.isEmpty()
}

/** What the control plane row says: an update running, a next minor, up to date, or nothing known. */
enum class EksUpgradeState { RUNNING, AVAILABLE, UNCHECKED, UP_TO_DATE, UNKNOWN }

/** The row's state, [update] (the one the app follows, if any) first. */
fun EksUpgradePlan.state(update: NodeGroupUpdate?): EksUpgradeState = when {
    (update ?: updating)?.inProgress == true -> EksUpgradeState.RUNNING
    version.isEmpty() -> EksUpgradeState.UNKNOWN
    next.isEmpty() -> EksUpgradeState.UP_TO_DATE
    nextKnown -> EksUpgradeState.AVAILABLE
    else -> EksUpgradeState.UNCHECKED
}
