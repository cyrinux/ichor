package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_nodegroups_eks.go: a cluster's cloud node pools (EKS managed node
// groups), read with the cluster's own AWS sign-in. installed false: not an EKS sign-in, the
// Autoscaling screen hides the group.

const val NODE_GROUPS_EKS = "eks"

/** The group key the "Needs attention" rows of a managed node group carry. */
const val AUTOSCALING_GROUP_NODE_GROUPS = "nodegroups"

/** EKS' status of a group that is up and running; anything else needs attention. */
const val NODE_GROUP_ACTIVE = "ACTIVE"

@Serializable
data class NodeGroupsReport(
    val provider: String = "",
    val installed: Boolean = false,
    val region: String = "",
    val cluster: String = "",
    val error: String = "",
    /** The AWS action the role may not call ("eks:ListNodegroups"), when that is the error. */
    val forbidden: String = "",
    val groups: List<ManagedNodeGroupRow> = emptyList(),
)

/** One managed node group: its size against its bounds, version, capacity and AWS' health issues. */
@Serializable
data class ManagedNodeGroupRow(
    val name: String = "",
    val status: String = "",
    val desired: Int = 0,
    val min: Int = 0,
    val max: Int = 0,
    val version: String = "",
    val releaseVersion: String = "",
    val amiType: String = "",
    val capacityType: String = "",
    val instanceTypes: List<String> = emptyList(),
    val diskSize: Int = 0,
    /** The cluster's nodes carrying the group's label; differs from [desired] while it scales. */
    val nodes: Int = 0,
    val atMax: Boolean = false,
    val issues: List<NodeGroupIssue> = emptyList(),
    val updating: Boolean = false,
    /** Unix ms, 0 when unknown. */
    val created: Long = 0,
) {
    val key: String get() = name

    /** At its maximum, reporting a health issue, or not ACTIVE (creating, updating, degraded). */
    val needsAttention: Boolean get() = atMax || issues.isNotEmpty() || status != NODE_GROUP_ACTIVE
}

@Serializable
data class NodeGroupIssue(val code: String = "", val message: String = "", val resourceIds: List<String> = emptyList())

/** The groups that need attention, at their maximum or with a health issue first. */
fun NodeGroupsReport.problemGroups(): List<ManagedNodeGroupRow> =
    groups.filter { it.needsAttention }.sortedByDescending { it.atMax || it.issues.isNotEmpty() }

/** The groups a search [query] keeps (name, version, capacity type, instance types). */
fun NodeGroupsReport.filtered(query: String): NodeGroupsReport {
    val q = query.trim()
    if (q.isEmpty()) return this
    return copy(
        groups = groups.filter { g ->
            (listOf(g.name, g.version, g.capacityType, g.status) + g.instanceTypes).any { it.contains(q, ignoreCase = true) }
        },
    )
}
