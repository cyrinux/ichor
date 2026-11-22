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

// Mirrors go/ichorgo/kube_nodegroups_eks_scale.go: a group's size changed from the phone
// (UpdateNodegroupConfig) and the update AWS runs for it (DescribeUpdate).

/** AWS' statuses of an update; anything but in progress is the end of it. */
const val NODE_GROUP_UPDATE_IN_PROGRESS = "InProgress"
const val NODE_GROUP_UPDATE_SUCCESSFUL = "Successful"

/** The app's own status of an update it could not read any more (the error says why). */
const val NODE_GROUP_UPDATE_UNREADABLE = "Unreadable"

/** The largest size the dialog offers (EKS' own limit per group is set by quota). */
const val NODE_GROUP_MAX_SIZE = 450

/** EKS' status of a group whose instances do not all launch: its size may still change. */
const val NODE_GROUP_DEGRADED = "DEGRADED"

/** The update AWS runs on a group: InProgress, Failed, Cancelled or Successful, and why it failed. */
@Serializable
data class NodeGroupUpdate(
    val id: String = "",
    val status: String = "",
    val type: String = "",
    val errors: List<NodeGroupIssue> = emptyList(),
) {
    val inProgress: Boolean get() = status == NODE_GROUP_UPDATE_IN_PROGRESS
    val successful: Boolean get() = status == NODE_GROUP_UPDATE_SUCCESSFUL

    /** Why it failed, was cancelled or could not be read: the first error's message, else the status. */
    val failure: String?
        get() = if (inProgress || successful) null else errors.firstOrNull()?.message.orEmpty().ifEmpty { status }
}

/** Why a desired / minimum / maximum size is refused before any call (Go: validateNodeGroupSize). */
enum class NodeGroupSizeProblem { MIN_NEGATIVE, MAX_TOO_LOW, MIN_ABOVE_MAX, DESIRED_OUTSIDE }

fun nodeGroupSizeProblem(desired: Int, min: Int, max: Int): NodeGroupSizeProblem? = when {
    min < 0 -> NodeGroupSizeProblem.MIN_NEGATIVE
    max < 1 -> NodeGroupSizeProblem.MAX_TOO_LOW
    min > max -> NodeGroupSizeProblem.MIN_ABOVE_MAX
    desired !in min..max -> NodeGroupSizeProblem.DESIRED_OUTSIDE
    else -> null
}

/** Active or degraded: a group already updating (or being created or deleted) may not change its size. */
val ManagedNodeGroupRow.canChangeSize: Boolean
    get() = !updating && (status == NODE_GROUP_ACTIVE || status == NODE_GROUP_DEGRADED)
